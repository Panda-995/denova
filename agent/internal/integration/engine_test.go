package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/alfredxw/denova/agent/engine"
	"github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/stream"
	"github.com/alfredxw/denova/agent/schema"
	"github.com/alfredxw/denova/agent/session"
)

type checkpointModel struct {
	inputs [][]*schema.Message
}

func (m *checkpointModel) Generate(_ context.Context, input []*schema.Message, _ ...model.ModelOption) (*schema.Message, error) {
	m.inputs = append(m.inputs, schema.CloneMessages(input))
	return schema.AssistantMessage("saved answer", nil), nil
}

func (m *checkpointModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.ModelOption) (*stream.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return stream.StreamReaderFromArray([]*schema.Message{message}), nil
}

func TestPublicEngineRestoresCheckpointAndHonorsCommitFailure(t *testing.T) {
	provider := &checkpointModel{}
	config := engine.Config{
		Source:  engine.Definition{Model: provider},
		Session: session.Named("public-engine"),
	}
	var state json.RawMessage
	var final string
	accept := func(update engine.Event) error {
		switch update := update.(type) {
		case engine.TranscriptUpdated:
			state = append(json.RawMessage(nil), update.State...)
		case engine.AssistantFinal:
			state = append(json.RawMessage(nil), update.State...)
			final = update.Content
		}
		return nil
	}
	for _, text := range []string{"first question", "follow-up question"} {
		// The caller restores the opaque checkpoint into a new engine instance.
		runner, err := engine.New(config)
		if err != nil {
			t.Fatal(err)
		}
		_, input, err := engine.EncodeInput(schema.Text(text))
		if err != nil {
			t.Fatal(err)
		}
		result, err := runner.Run(t.Context(), engine.Request{Snapshot: engine.TurnSnapshot{
			CommandID: engine.CommandID(text), OperationID: engine.OperationID(text),
			Cycle: 1, Delivery: engine.DeliveryStart, Input: input, State: state,
		}}, accept)
		if err != nil || result.Status != engine.Completed || final != "saved answer" || len(state) == 0 {
			t.Fatalf("cycle %q: result=%+v final=%q state=%s err=%v", text, result, final, state, err)
		}
	}
	if len(provider.inputs) != 2 {
		t.Fatalf("expected two model calls, got %d", len(provider.inputs))
	}
	want := []string{"user:first question", "assistant:saved answer", "user:follow-up question"}
	var got []string
	for _, message := range provider.inputs[1] {
		got = append(got, string(message.Role)+":"+message.Content)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("cold engine lost or duplicated history: got=%v want=%v", got, want)
	}

	runner, err := engine.New(config)
	if err != nil {
		t.Fatal(err)
	}
	commitError := errors.New("journal rejected checkpoint")
	_, err = runner.Run(t.Context(), engine.Request{Snapshot: engine.TurnSnapshot{
		CommandID: "rejected", OperationID: "rejected", Cycle: 1,
		Delivery: engine.DeliveryStart, Input: engine.UserInput{Text: "must not reach provider"}, State: state,
	}}, func(engine.Event) error { return commitError })
	if !errors.Is(err, commitError) || len(provider.inputs) != 2 {
		t.Fatalf("commit failure did not stop execution: calls=%d err=%v", len(provider.inputs), err)
	}
}
