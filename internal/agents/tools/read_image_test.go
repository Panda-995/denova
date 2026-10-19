package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"denova/internal/agents/toolartifact"

	agentexecution "github.com/alfredxw/denova/agent/engine/execution"
	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agenttools "github.com/alfredxw/denova/agent/tool/builtin"
	toolresult "github.com/alfredxw/denova/agent/tool/result"
)

func TestReadImagesCaptureImmutableCopiesAcrossFormats(t *testing.T) {
	for _, format := range []string{"png", "jpeg", "gif", "webp"} {
		t.Run(format, func(t *testing.T) {
			var data bytes.Buffer
			picture := image.NewNRGBA(image.Rect(0, 0, 31, 47))
			var err error
			switch format {
			case "png":
				err = png.Encode(&data, picture)
			case "jpeg":
				err = jpeg.Encode(&data, picture, nil)
			case "gif":
				err = gif.Encode(&data, picture, nil)
			case "webp":
				decoded, decodeErr := base64.StdEncoding.DecodeString("UklGRiIAAABXRUJQVlA4TBEAAAAvAAAAAAfQ//73v/+BiOh/AAA=")
				err = decodeErr
				data.Write(decoded)
			}
			if err != nil {
				t.Fatal(err)
			}
			workspaceRoot, stateRoot := t.TempDir(), t.TempDir()
			// Detect bytes even when the source has no extension.
			source := filepath.Join(workspaceRoot, "reference")
			if err := os.WriteFile(source, data.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			definition, ctx := imageReadDefinition(t, workspaceRoot, stateRoot)
			ctx = agentexecution.ContextWithToolCall(ctx, "image-call", "read")
			result, err := definition.Tool.Run(ctx, `{"path":"reference"}`)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Attachments) != 1 || len(result.Artifacts) != 1 || !strings.Contains(result.ModelContent, `"kind":"local_image"`) {
				t.Fatalf("image was not returned natively: %+v", result)
			}
			attachment := result.Attachments[0]
			if !fs.ValidPath(attachment.Path) || filepath.IsAbs(attachment.Path) || attachment.Path == "reference" || attachment.MediaType != "image/"+format {
				t.Fatalf("snapshot identity is not portable: %+v", attachment)
			}
			// Same execution is idempotent; another read captures the then-current bytes.
			retry, err := definition.Tool.Run(ctx, `{"path":"reference"}`)
			if err != nil || !reflect.DeepEqual(retry.Attachments, result.Attachments) {
				t.Fatalf("retry changed image snapshot: %v", err)
			}
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			original, err := agentschema.ReadAttachmentImage(attachment)
			if err != nil || !bytes.Equal(original, data.Bytes()) {
				t.Fatalf("source deletion changed captured pixels: %v", err)
			}
			processed, err := toolresult.Standard(toolresult.Policy{MaxBytes: 512}).Process(ctx, toolresult.ToolResultProcessRequest{
				ToolName: "read", Arguments: `{"path":"reference"}`, ProviderCallID: "image-call", BatchSize: 1,
				Definition: agenttool.ToolDefinitionSnapshot{Descriptor: definition.Descriptor}, Result: result,
			})
			if err != nil {
				t.Fatal(err)
			}
			message := agentschema.ToolMessage(processed, "image-call", agentschema.WithToolName("read"))
			if !reflect.DeepEqual(message.Attachments, result.Attachments) || !reflect.DeepEqual(message.EffectiveToolResult().Attachments, result.Attachments) {
				t.Fatal("tool projection dropped the native image")
			}
			encoded, err := json.Marshal(message)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, []byte(stateRoot)) || bytes.Contains(encoded, []byte("base64")) {
				t.Fatal("host paths or binary data entered the journal")
			}
			var restored agentschema.Message
			if err := json.Unmarshal(encoded, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.Attachments[0].RuntimePath != "" || restored.Attachments[0].Path != attachment.Path {
				t.Fatal("image journal round trip changed portable identity")
			}
			estimator := agentmodel.InputEstimator{ImageTokens: func(int, int) int { return 123 }}
			size, err := estimator.Estimate([]*agentschema.Message{message, message.Clone()}, nil)
			if err != nil || size.Tokens != agentmodel.EstimateRequestTextTokens([]*agentschema.Message{message, message.Clone()}, nil)+246 || providers.NativeImageCount([]*agentschema.Message{message, message.Clone()}) != 2 {
				t.Fatalf("repeated tool images were not included in visual accounting: %+v %v", size, err)
			}
			if err := os.WriteFile(attachment.RuntimePath, []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := agentschema.ReadAttachmentImage(attachment); err == nil {
				t.Fatal("snapshot corruption was silently accepted")
			}
		})
	}
}

func TestReadImageFailuresDoNotPublishSnapshots(t *testing.T) {
	for _, scenario := range []string{"invalid", "oversized", "cancelled", "no_store"} {
		t.Run(scenario, func(t *testing.T) {
			workspaceRoot, stateRoot := t.TempDir(), t.TempDir()
			var data bytes.Buffer
			if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
				t.Fatal(err)
			}
			if scenario == "invalid" {
				data.Truncate(12)
			}
			source := filepath.Join(workspaceRoot, "reference.png")
			if err := os.WriteFile(source, data.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if scenario == "oversized" {
				if err := os.Truncate(source, 20<<20+1); err != nil {
					t.Fatal(err)
				}
			}
			definition, ctx := imageReadDefinition(t, workspaceRoot, stateRoot)
			if scenario == "no_store" {
				ctx = t.Context()
			}
			if scenario == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if _, err := definition.Tool.Run(ctx, `{"path":"reference.png"}`); err == nil {
				t.Fatal("invalid image read unexpectedly succeeded")
			}
			entries, err := os.ReadDir(stateRoot)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed image read published state: %v %v", entries, err)
			}
		})
	}
}

func imageReadDefinition(t *testing.T, workspaceRoot, stateRoot string, maxBytes ...int) (agenttool.ToolDefinition, context.Context) {
	t.Helper()
	workspace, err := agenttools.OpenWorkspaceWithOptions(agenttools.WorkspaceOptions{Root: workspaceRoot})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := agenttools.LocalFileAdapter(workspace)
	if err != nil {
		t.Fatal(err)
	}
	limit := 256
	if len(maxBytes) > 0 {
		limit = maxBytes[0]
	}
	definition, err := agenttools.Read([]agenttools.ReadAdapter{adapter}, agenttools.WithMaxResultBytes(limit))
	if err != nil {
		t.Fatal(err)
	}
	store, err := toolartifact.NewStateStore(stateRoot, "image-session")
	if err != nil {
		t.Fatal(err)
	}
	return definition, agenttool.ContextWithToolArtifactBackend(t.Context(), store)
}
