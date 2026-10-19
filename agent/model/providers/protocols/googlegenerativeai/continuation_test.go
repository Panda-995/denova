package googlegenerativeai

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alfredxw/denova/agent/model/providers"
	agentschema "github.com/alfredxw/denova/agent/schema"
	"google.golang.org/genai"
)

func TestStreamRetainsSignedPartsAndUsageOnlyTail(t *testing.T) {
	config := providers.ModelConfig{Provider: providers.ProviderGoogle, Protocol: providers.ProtocolGoogleGenerativeAI, Model: "gemini-test"}
	state := &streamState{}
	var messages []*agentschema.Message
	for _, response := range []*genai.GenerateContentResponse{
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "think", Thought: true}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{ThoughtSignature: []byte("thought-signature")}}}}}},
		{Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "lookup", Args: map[string]any{"q": "x"}}, ThoughtSignature: []byte("tool-signature")}}}, FinishReason: genai.FinishReasonStop}}},
		{UsageMetadata: &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 4, ThoughtsTokenCount: 2, TotalTokenCount: 16}},
	} {
		chunks, err := state.convert(response, config)
		if err != nil {
			t.Fatal(err)
		}
		messages = append(messages, chunks...)
	}
	final, err := state.finish(config)
	if err != nil {
		t.Fatal(err)
	}
	messages = append(messages, final)
	message, err := agentschema.ConcatMessages(messages)
	if err != nil {
		t.Fatal(err)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].ID == "" || message.ResponseMeta.Usage.CompletionTokens != 6 {
		t.Fatalf("message = %#v", message)
	}
	parts, err := assistantParts(message, config)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || string(parts[0].ThoughtSignature) != "thought-signature" || string(parts[1].ThoughtSignature) != "tool-signature" || parts[1].FunctionCall.ID != message.ToolCalls[0].ID {
		t.Fatalf("continuation = %#v", parts)
	}
	message.ToolCalls = nil
	parts, err = assistantParts(message, config)
	if err != nil || len(parts) != 1 {
		t.Fatalf("removed call was replayed: %#v, %v", parts, err)
	}
}

func TestStreamWithoutTerminalDoesNotCommitContinuation(t *testing.T) {
	state := &streamState{parts: []*genai.Part{{Text: "partial"}}}
	if _, err := state.convert(&genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonUnspecified}}}, providers.ModelConfig{}); err != nil {
		t.Fatal(err)
	}
	if message, err := state.finish(providers.ModelConfig{}); err == nil || message != nil {
		t.Fatalf("partial stream committed: %#v, %v", message, err)
	}
}

func TestUserAndToolImagesStayNativeAndParallelResultsStayTogether(t *testing.T) {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aY9sAAAAASUVORK5CYII=")
	path := filepath.Join(t.TempDir(), "image.png")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	attachments := []agentschema.Attachment{{Name: "image.png", MediaType: "image/png", Path: path, Size: int64(len(data)), SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}}
	tool := agentschema.ToolMessage(agentschema.TextToolResult("pixels"), "one", agentschema.WithToolName("lookup"))
	tool.Attachments = attachments
	_, contents, err := requestContents([]*agentschema.Message{
		agentschema.UserMessageWithAttachments("inspect", attachments),
		{Role: agentschema.Assistant, ToolCalls: []agentschema.ToolCall{{ID: "one", Function: agentschema.FunctionCall{Name: "lookup", Arguments: `{}`}}, {ID: "two", Function: agentschema.FunctionCall{Name: "lookup", Arguments: `{}`}}}},
		tool, agentschema.ToolMessage(agentschema.TextToolResult("second"), "two", agentschema.WithToolName("lookup")),
	}, providers.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if len(contents) != 3 || len(contents[2].Parts) != 2 {
		t.Fatalf("parallel results = %#v", contents)
	}
	if !bytes.Equal(contents[0].Parts[1].InlineData.Data, data) || !bytes.Equal(contents[2].Parts[0].FunctionResponse.Parts[0].InlineData.Data, data) {
		t.Fatal("native image pixels changed")
	}
}
