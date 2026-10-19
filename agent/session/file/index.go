package file

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/alfredxw/denova/agent/session"
)

type checkpointBody struct {
	Version     int                                 `json:"version"`
	Revision    session.Revision                    `json:"revision"`
	Bytes       int64                               `json:"bytes"`
	FirstDigest string                              `json:"first_digest"`
	LastDigest  string                              `json:"last_digest"`
	Last        recordLocation                      `json:"last"`
	Locations   map[session.Revision]recordLocation `json:"locations"`
	Resilience  bool                                `json:"resilience"`
	Projection  json.RawMessage                     `json:"projection"`
}
type checkpointFile struct {
	checkpointBody
	Checksum string `json:"checksum"`
}

func digestBytes(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }
func (log *logFile) indexPath() string {
	path := strings.TrimSuffix(log.path, ".jsonl") + ".idx.json"
	if log.readOnly {
		path += ".current"
	}
	return path
}
func (log *logFile) rememberTransaction(line []byte, location recordLocation) {
	digest := digestBytes(line)
	if log.firstDigest == "" {
		log.firstDigest = digest
	}
	log.lastDigest, log.lastLocation = digest, location
}

// Recover verifies an immutable prefix, restores its disposable projection and
// reduces only the committed tail. Full Replay remains available to callers.
func (log *logFile) Recover(ctx context.Context, target session.CheckpointProjection) (result error) {
	if ctx == nil {
		ctx = context.Background()
	}
	log.mu.Lock()
	defer log.mu.Unlock()
	started := time.Now()
	cache := "hit"
	defer func() {
		if result != nil || time.Since(started) >= time.Second {
			slog.InfoContext(ctx, "[agent-session-file] recovery", "path", log.path, "read_only", log.readOnly, "cache", cache, "revision", log.revision, "duration_ms", time.Since(started).Milliseconds(), "error", result)
		}
	}()
	if log.closed {
		return session.ErrLogClosed
	}
	release, err := acquireLease(ctx, log.path+".domain.lock")
	if err != nil {
		return err
	}
	defer release()
	log.projection = nil
	projection := target.New()
	previous := session.Revision(0)
	if err := log.restoreCheckpointLocked(projection); err != nil {
		cache = "rebuilt"
		if err := projection.Reset(); err != nil {
			return err
		}
		if _, err := log.replayLocked(ctx, projection.Apply); err != nil {
			return err
		}
	} else {
		previous = log.revision
		if err := log.replayTailLocked(ctx, projection); err != nil {
			cache = "rebuilt"
			// A bounded observation checkpoint can lack an old command referenced
			// by a new control record. Rebuild from canonical facts in that case.
			if err := projection.Reset(); err != nil {
				return err
			}
			if _, err := log.replayLocked(ctx, projection.Apply); err != nil {
				return err
			}
			previous = 0
		}
	}
	log.projection = projection
	if previous != log.revision {
		if err := log.persistCheckpointLocked(); err != nil {
			slog.WarnContext(ctx, "[agent-session-file] recovery cache write failed", "path", log.path, "error", err)
			// Keep the older verified checkpoint: its canonical tail is enough
			// for the next recovery, even when this cache write is unavailable.
		}
	}
	data, err := projection.Checkpoint()
	if log.readOnly {
		if current, ok := projection.(interface {
			CurrentCheckpoint() (json.RawMessage, error)
		}); ok {
			data, err = current.CurrentCheckpoint()
		}
	}
	if err != nil {
		return err
	}
	return target.Restore(data)
}

func (log *logFile) restoreCheckpointLocked(projection session.CheckpointProjection) error {
	data, err := os.ReadFile(log.indexPath())
	if err != nil {
		return err
	}
	var checkpoint checkpointFile
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return err
	}
	body, err := json.Marshal(checkpoint.checkpointBody)
	if err != nil {
		return err
	}
	if checkpoint.Version != 1 || checkpoint.Checksum != digestBytes(body) {
		return errors.New("invalid Agent recovery cache")
	}
	file, err := os.Open(log.path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if checkpoint.Bytes <= 0 || info.Size() < checkpoint.Bytes || checkpoint.Last.Offset < 0 || checkpoint.Last.Length <= 0 || checkpoint.Last.Offset+int64(checkpoint.Last.Length) != checkpoint.Bytes {
		return errors.New("Agent recovery cache prefix changed")
	}
	first, err := bufio.NewReaderSize(file, replayBufferBytes).ReadBytes('\n')
	if err != nil {
		return err
	}
	last := make([]byte, checkpoint.Last.Length)
	if _, err := file.ReadAt(last, checkpoint.Last.Offset); err != nil {
		return err
	}
	if digestBytes(first) != checkpoint.FirstDigest || digestBytes(last) != checkpoint.LastDigest {
		return errors.New("Agent recovery cache incarnation changed")
	}
	if err := projection.Restore(checkpoint.Projection); err != nil {
		return err
	}
	log.revision, log.validBytes, log.locations = checkpoint.Revision, checkpoint.Bytes, checkpoint.Locations
	log.firstDigest, log.lastDigest, log.lastLocation = checkpoint.FirstDigest, checkpoint.LastDigest, checkpoint.Last
	log.resilienceFormat, log.initialized = checkpoint.Resilience, true
	log.checkpointRevision = checkpoint.Revision
	return nil
}

func (log *logFile) replayTailLocked(ctx context.Context, projection session.CheckpointProjection) error {
	file, err := os.Open(log.path)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := file.Seek(log.validBytes, io.SeekStart); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(file, replayBufferBytes)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 || line[len(line)-1] != '\n' {
			return nil
		}
		records, err := decodeTransaction(line[:len(line)-1], log.revision)
		if err != nil {
			return err
		}
		location := recordLocation{Offset: log.validBytes, Length: len(line), Previous: log.revision}
		if log.locations == nil {
			log.locations = make(map[session.Revision]recordLocation)
		}
		for _, record := range records {
			if err := projection.Apply(record); err != nil {
				return err
			}
			log.locations[record.Revision] = location
			log.resilienceFormat = log.resilienceFormat || isResilienceRecord(record.Kind)
		}
		log.rememberTransaction(line, location)
		log.revision, log.validBytes = records[len(records)-1].Revision, log.validBytes+int64(len(line))
	}
}

func (log *logFile) persistCheckpointLocked() error {
	if log.projection == nil || log.revision == 0 {
		return nil
	}
	data, err := log.projection.Checkpoint()
	if err != nil {
		return err
	}
	body := checkpointBody{Version: 1, Revision: log.revision, Bytes: log.validBytes, FirstDigest: log.firstDigest, LastDigest: log.lastDigest, Last: log.lastLocation, Locations: log.locations, Resilience: log.resilienceFormat, Projection: data}
	write := func(path string, body checkpointBody) error {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		encoded, err = json.Marshal(checkpointFile{checkpointBody: body, Checksum: digestBytes(encoded)})
		if err != nil {
			return err
		}
		return commitAtomicFile(path, append(encoded, '\n'))
	}
	if !log.readOnly {
		if err := write(log.indexPath(), body); err != nil {
			return err
		}
	}
	log.checkpointRevision = log.revision
	if current, ok := log.projection.(interface {
		CurrentCheckpoint() (json.RawMessage, error)
		CurrentReadRevisions() []session.Revision
	}); ok {
		body.Projection, err = current.CurrentCheckpoint()
		if err != nil {
			return err
		}
		body.Locations = make(map[session.Revision]recordLocation)
		for _, revision := range current.CurrentReadRevisions() {
			if location, found := log.locations[revision]; found {
				body.Locations[revision] = location
			}
		}
		return write(strings.TrimSuffix(log.path, ".jsonl")+".idx.json.current", body)
	}
	if log.readOnly {
		return write(log.indexPath(), body)
	}
	return nil
}

var _ session.IndexedLog = (*logFile)(nil)
