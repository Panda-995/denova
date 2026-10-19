package model

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestImageEstimateDoesNotDependOnLosslessCompression(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 768, 768))
	for index := range picture.Pix {
		picture.Pix[index] = byte(index % 251)
	}
	var estimates []int
	for _, compression := range []png.CompressionLevel{png.NoCompression, png.BestCompression} {
		var encoded bytes.Buffer
		if err := (&png.Encoder{CompressionLevel: compression}).Encode(&encoded, picture); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "reference.png")
		if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded.Bytes())
		message := agentschema.UserMessageWithAttachments("Describe this reference.", []agentschema.Attachment{{
			ID: "reference", Name: "reference.png", MediaType: "image/png", Path: path,
			Size: int64(encoded.Len()), SHA256: hex.EncodeToString(digest[:]),
		}})
		size, err := (InputEstimator{}).Estimate([]*agentschema.Message{message}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if size.Bytes >= encoded.Len() {
			t.Fatalf("encoded image bytes leaked into context budget: %+v", size)
		}
		estimates = append(estimates, size.Tokens)
	}
	if estimates[0] > 400000 || estimates[1] > 400000 || estimates[0] > estimates[1]+100 {
		t.Fatalf("identical pixels received incompatible context budgets: %v", estimates)
	}
}

func TestTextEstimateIncludesAttachmentInstructions(t *testing.T) {
	plain := EstimateMessageTextTokens(&agentschema.Message{Role: agentschema.User, Content: "inspect"})
	withDocument := EstimateMessageTextTokens(agentschema.UserMessageWithAttachments("inspect", []agentschema.Attachment{{
		ID: "att-doc", Name: "notes.md", MediaType: "text/markdown", Size: 128, Path: "/inputs/notes.md", SHA256: "digest",
	}}))
	if withDocument <= plain {
		t.Fatalf("document attachment estimate = %d, plain = %d", withDocument, plain)
	}
}
