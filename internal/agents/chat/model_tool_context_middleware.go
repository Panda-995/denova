package chat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"denova/internal/agents/toolresult"

	agentmiddleware "github.com/alfredxw/denova/agent/engine/middleware"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

type modelHistoryProjectionMiddleware struct {
	*agentmiddleware.BaseMiddleware
	policy toolresult.ContextPolicy
}

// NewModelHistoryProjectionMiddleware applies Denova's product history policy
// only before the active raw user message. The current cycle's tool exchange
// remains visible. Provider adapters own reasoning replay because signed or
// encrypted reasoning state is protocol-specific and may be required on the
// next turn.
func NewModelHistoryProjectionMiddleware(policy toolresult.ContextPolicy) agentmiddleware.IdentifiedMiddleware {
	return &modelHistoryProjectionMiddleware{
		BaseMiddleware: &agentmiddleware.BaseMiddleware{}, policy: policy.Normalize(),
	}
}

func (middleware *modelHistoryProjectionMiddleware) Identity() agentschema.CapabilityIdentity {
	encoded, _ := json.Marshal(middleware.policy)
	digest := sha256.Sum256(encoded)
	return agentschema.CapabilityIdentity{
		Kind: "denova.model.history_projection", Version: 1,
		ConfigHash: hex.EncodeToString(digest[:]),
	}
}

func (middleware *modelHistoryProjectionMiddleware) BeforeModelCall(
	ctx context.Context,
	call *agentmodel.ModelCall,
	modelContext *agentmiddleware.ModelContext,
) (context.Context, *agentmodel.ModelCall, error) {
	if middleware == nil || call == nil {
		return ctx, call, nil
	}
	activeUser := lastModelUserIndex(call.Messages)
	if activeUser <= 0 {
		return ctx, call, nil
	}
	projected := toolresult.ApplyContextPolicy(call.Messages[:activeUser], middleware.policy)
	messages := make([]*agentschema.Message, 0, len(projected)+len(call.Messages)-activeUser)
	messages = append(messages, projected...)
	for _, message := range call.Messages[activeUser:] {
		messages = append(messages, agentschema.CloneMessage(message))
	}
	if reflect.DeepEqual(call.Messages, messages) {
		return ctx, call, nil
	}
	if modelContext != nil {
		modelContext.ReportContextNormalization(agentmiddleware.ContextNormalizationMetrics{
			RepairCount: 1, MessagesBefore: len(call.Messages), MessagesAfter: len(messages),
		})
	}
	next := *call
	next.Messages = messages
	return ctx, &next, nil
}

func lastModelUserIndex(messages []*agentschema.Message) int {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index] != nil && messages[index].Role == agentschema.User {
			return index
		}
	}
	return -1
}
