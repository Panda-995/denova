package conversationjournal

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

// RewritePayloads is an offline, backed-up format migration seam. It preserves
// identities, cursors, timestamps and domain records while verifying the old
// physical chain and rebuilding checksums after payload transformation. The
// owner must atomically replace the journal before opening it and drop its index.
func RewritePayloads(data []byte, transform func(json.RawMessage) (json.RawMessage, error)) ([]byte, error) {
	reader := bufio.NewReader(bytes.NewReader(data))
	var output bytes.Buffer
	oldPrevious, newPrevious := "", ""
	var cursor Cursor
	var identity Identity
	hasIdentity := false
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}
		if len(line) == 0 {
			break
		}
		original := trimRecord(line)
		if len(original) == 0 {
			return nil, fmt.Errorf("empty conversation journal record")
		}
		// Keep the same recoverable incomplete tail accepted by journal replay.
		// Its original bytes remain in the migration backup and normal append
		// recovery will archive it; migration must not invent a committed row.
		if !json.Valid(original) && err == io.EOF && !bytes.HasSuffix(line, []byte{'\n'}) {
			output.Write(line)
			break
		}
		body, transactionRecord, decodeErr := decodeTransaction(original)
		if decodeErr != nil {
			return nil, decodeErr
		}
		var next []byte
		if transactionRecord {
			if body.Cursor != cursor+1 {
				return nil, fmt.Errorf("conversation migration cursor gap")
			}
			if hasIdentity && identity != body.Identity {
				return nil, fmt.Errorf("conversation migration identity changed")
			}
			identity, hasIdentity = body.Identity, true
			if body.PreviousRecordSHA256 != oldPrevious {
				return nil, fmt.Errorf("conversation migration previous checksum mismatch")
			}
			changed := oldPrevious != newPrevious
			for i, raw := range body.Records {
				body.Records[i], decodeErr = transform(raw)
				changed = changed || !bytes.Equal(body.Records[i], raw)
				if decodeErr != nil {
					return nil, decodeErr
				}
			}
			if !changed {
				next = original
			} else {
				body.PreviousRecordSHA256 = newPrevious
				encoded, encodeErr := json.Marshal(body)
				if encodeErr != nil {
					return nil, encodeErr
				}
				digest := sha256.Sum256(encoded)
				next, decodeErr = json.Marshal(transaction{transactionBody: body, Checksum: hex.EncodeToString(digest[:])})
			}
		} else {
			next, decodeErr = transform(original)
		}
		if decodeErr != nil {
			return nil, decodeErr
		}
		cursor++
		output.Write(next)
		if bytes.HasSuffix(line, []byte{'\n'}) {
			output.WriteByte('\n')
		}
		oldPrevious, newPrevious = recordSHA256(original), recordSHA256(next)
		if err == io.EOF {
			break
		}
	}
	return output.Bytes(), nil
}
