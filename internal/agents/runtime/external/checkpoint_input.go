package external

import (
	"encoding/json"
	"fmt"

	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

// Adapters may report the resolved model's existing visual estimator. Unknown
// models use Agent's shared image fallback; no Native execution is involved.
type modelInputEstimator interface {
	InputEstimator(Input) agentmodel.InputEstimator
}

func estimatorFor(adapter Adapter, input Input) agentmodel.InputEstimator {
	if provider, ok := adapter.(modelInputEstimator); ok {
		return provider.InputEstimator(input)
	}
	return agentmodel.InputEstimator{}
}

func messageCost(estimator agentmodel.InputEstimator, message Message) (agentmodel.InputSize, error) {
	// Tool images can accompany any projected public role. The neutral User
	// envelope ensures all of them receive the same visual estimate.
	files := append(append([]agentschema.Attachment(nil), message.Attachments...), message.ToolImages...)
	size, err := estimator.Estimate([]*agentschema.Message{agentschema.UserMessageWithAttachments(message.Text, files)}, nil)
	size.Bytes = messageBytes(message)
	return size, err
}

func inputTokens(estimator agentmodel.InputEstimator, input Input) (int, error) {
	// Maintenance has no tools; this estimate includes its complete instructions,
	// rolling summary, ordered text records and native image parts.
	tokens := agentmodel.EstimateTextTokens(input.Instructions) + agentmodel.EstimateTextTokens(input.Text)
	for _, message := range input.History {
		cost, err := messageCost(estimator, message)
		if err != nil {
			return 0, err
		}
		tokens += cost.Tokens
	}
	return tokens, nil
}

func checkInputBytes(input Input, limit int) error {
	if limit <= 0 {
		return nil
	}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	if len(body) > limit {
		return fmt.Errorf("external provider input exceeds shared semantic byte budget: %d > %d", len(body), limit)
	}
	return nil
}

func prepareInput(input Input, limit int) (Input, error) {
	input.History = append([]Message(nil), input.History...)
	input.Text = agentschema.ModelUserContent(&agentschema.Message{Content: input.Text, Attachments: input.Attachments})
	for index := range input.History {
		message := &input.History[index]
		message.Text = agentschema.ModelUserContent(&agentschema.Message{Content: message.Text, Attachments: message.Attachments})
	}
	if err := checkInputBytes(input, limit); err != nil {
		return Input{}, err
	}
	return input, nil
}
