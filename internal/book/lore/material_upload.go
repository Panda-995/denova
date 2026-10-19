package lore

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"log/slog"
	"path"
	"strings"
	"time"

	"denova/internal/assetstore"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

const MaxMaterialUploadBytes = 64 * 1024 * 1024

var ErrMaterialInvalid = errors.New("material must contain a valid PNG, JPEG, WebP, GIF, MP3 or PCM WAV file")
var ErrMaterialTooLarge = errors.New("material exceeds 64 MiB")

// MaterialFile is a ready payload from an upload or generator. Source and Entry
// are committed with the file reference; callers never overwrite existing assets.
type MaterialFile struct {
	Filename       string
	Data           []byte
	Source         AssetSource
	Entry          MaterialEntry
	ReplaceAssetID string
	// Cover controls whether this image fills an empty cover on attachment.
	Cover CoverPolicy
}

func (s *Store) UploadMaterial(ctx context.Context, id, filename string, data []byte) (Item, error) {
	return s.SaveMaterial(ctx, id, MaterialFile{Filename: filename, Data: data, Source: AssetSource{Kind: "upload"}})
}

// SaveMaterial publishes an immutable file before committing its attributes
// and association together in items.json.
func (s *Store) SaveMaterial(ctx context.Context, id string, file MaterialFile) (Item, error) {
	if file.Cover != CoverPreserve && file.Cover != CoverIfMissing {
		return Item{}, errors.New("unknown lore cover policy")
	}
	data := file.Data
	if len(data) > MaxMaterialUploadBytes {
		return Item{}, ErrMaterialTooLarge
	}
	mime, ext, err := MaterialFormat(data)
	if err != nil {
		return Item{}, err
	}
	if file.Cover == CoverIfMissing && !strings.HasPrefix(mime, "image/") {
		return Item{}, errors.New("cover material must be an image")
	}
	if _, err := s.ReadAny(id); err != nil {
		return Item{}, err
	}
	a := Asset{ID: "asset_" + uuid.NewString(), Path: assetstore.NewPath(assetstore.Lore, ext), OriginalName: path.Base(strings.ReplaceAll(file.Filename, `\`, "/")),
		MIMEType: mime, SizeBytes: len(data), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Source: file.Source}
	if err := assetstore.Save(ctx, s.workspace, assetstore.File{Path: a.Path, Data: data}); err != nil {
		return Item{}, err
	}
	committed := false
	defer func() {
		if !committed {
			if err := s.DiscardUnlinkedMaterial(context.Background(), a.Path); err != nil {
				slog.WarnContext(ctx, "[lore-material] retain file after uncertain association", "path", a.Path, "error", err)
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return Item{}, err
	}
	item, err := s.attachAsset(id, a, file.Entry, materialAttachment{replaceID: file.ReplaceAssetID, cover: file.Cover})
	if err != nil {
		return Item{}, err
	}
	committed = true
	slog.InfoContext(ctx, "[lore-material] saved", "item_id", id, "asset_id", a.ID, "path", a.Path, "source", file.Source.Kind)
	return item, nil
}

// DiscardUnlinkedMaterial is only for a fresh file owned by the failed caller.
// A successful association read is required before removing its metadata/bytes.
func (s *Store) DiscardUnlinkedMaterial(ctx context.Context, name string) error {
	assets, err := s.Assets()
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if asset.Path == name {
			return nil
		}
	}
	return assetstore.Discard(ctx, s.workspace, name)
}

// MaterialFormat validates supported upload containers and returns their canonical MIME and extension.
func MaterialFormat(data []byte) (mime, ext string, err error) {
	if len(data) == 0 {
		return "", "", ErrMaterialInvalid
	}
	if _, format, e := image.DecodeConfig(bytes.NewReader(data)); e == nil {
		switch format {
		case "png", "jpeg", "webp", "gif":
			return "image/" + format, format, nil
		}
	}
	// WAV is a chunk container. Require a supported format and nonempty, bounded
	// audio data instead of trusting the extension or just the RIFF signature.
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE" {
		end := int64(binary.LittleEndian.Uint32(data[4:8])) + 8
		if end > int64(len(data)) {
			return "", "", ErrMaterialInvalid
		}
		formatOK, audioOK := false, false
		for offset := int64(12); offset+8 <= end; {
			n := int64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			start := offset + 8
			if start+n > end {
				return "", "", ErrMaterialInvalid
			}
			switch string(data[offset : offset+4]) {
			case "fmt ":
				if n >= 16 {
					f := binary.LittleEndian.Uint16(data[start : start+2])
					formatOK = (f == 1 || f == 3) && binary.LittleEndian.Uint16(data[start+2:start+4]) > 0 && binary.LittleEndian.Uint32(data[start+4:start+8]) > 0
				}
			case "data":
				audioOK = n > 0
			}
			offset = start + n + n%2
		}
		if formatOK && audioOK {
			return "audio/wav", "wav", nil
		}
	}
	// Check an MPEG Layer III frame, after the optional ID3v2 tag. This bounds
	// the first encoded frame without decoding large uploads on the request path.
	offset := 0
	if len(data) >= 10 && string(data[:3]) == "ID3" {
		for _, b := range data[6:10] {
			if b&0x80 != 0 {
				return "", "", ErrMaterialInvalid
			}
			offset = (offset << 7) | int(b)
		}
		offset += 10
		if data[5]&0x10 != 0 {
			offset += 10
		}
	}
	if offset+4 <= len(data) {
		h := binary.BigEndian.Uint32(data[offset : offset+4])
		version := (h >> 19) & 3
		bitrate := (h >> 12) & 15
		rate := (h >> 10) & 3
		if h&0xffe00000 == 0xffe00000 && version != 1 && (h>>17)&3 == 1 && bitrate > 0 && bitrate < 15 && rate < 3 {
			rates := [3]int{44100, 48000, 32000}
			bps := [15]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
			sampleRate := rates[rate]
			factor := 144
			if version != 3 {
				sampleRate /= 2
				factor = 72
				bps = [15]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
				if version == 0 {
					sampleRate /= 2
				}
			}
			size := factor*bps[bitrate]*1000/sampleRate + int((h>>9)&1)
			if size >= 4 && offset+size <= len(data) {
				return "audio/mpeg", "mp3", nil
			}
		}
	}
	return "", "", ErrMaterialInvalid
}
