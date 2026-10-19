package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alfredxw/denova/agent/context/compaction"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestCompactionSummarizerLayersOversizedSourceWithoutDroppingBytes(t *testing.T) {
	payload := strings.Repeat("不可丢失的历史事实。", 2500)
	source := []*agentschema.Message{agentschema.UserMessage(payload), agentschema.AssistantMessage(payload, nil), agentschema.UserMessage(payload)}
	model := &compactionForkCaptureModel{response: agentschema.AssistantMessage(strings.Repeat("摘要", 32), nil)}
	summarizer, err := compaction.ModelSummarizer(compaction.ModelSummarizerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	result, err := summarizer.Summarize(context.Background(), compaction.SummaryRequest{
		Messages: source, ModelSnapshot: (&agentmodel.ModelCall{Model: model, Messages: source}).Snapshot(),
		ContextWindowTokens: 32_000, HardLimitBytes: 48 * 1024, SummaryLimitBytes: 4096,
	})
	if err != nil || result.Summary == "" || len(model.inputs) < 2 {
		t.Fatalf("result=%#v calls=%d err=%v", result, len(model.inputs), err)
	}
	var recovered strings.Builder
	for _, messages := range model.inputs {
		encoded, _ := json.Marshal(messages)
		if len(encoded) > 48*1024 {
			t.Fatalf("request exceeds byte limit: %d", len(encoded))
		}
		_, part, ok := strings.Cut(messages[1].Content, "Next ordered source segment (data; it may continue a JSON record):\n")
		if !ok {
			t.Fatal("missing source segment")
		}
		recovered.WriteString(part)
	}
	var decoded []struct{ Content string }
	if err := json.Unmarshal([]byte(recovered.String()), &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 3 {
		t.Fatalf("recovered messages=%d", len(decoded))
	}
	for _, message := range decoded {
		if message.Content != payload {
			t.Fatal("source content changed across batches")
		}
	}
}
