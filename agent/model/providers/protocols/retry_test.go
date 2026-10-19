package protocols_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	"github.com/alfredxw/denova/agent/model/providers/protocols/anthropicmessages"
	"github.com/alfredxw/denova/agent/model/providers/protocols/googlegenerativeai"
	"github.com/alfredxw/denova/agent/model/providers/protocols/openaichatcompletions"
	"github.com/alfredxw/denova/agent/model/providers/protocols/openairesponses"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestAdaptersMakeOneHTTPAttemptAndPreserveRetryHints(t *testing.T) {
	for _, adapter := range []providers.ProtocolAdapter{anthropicmessages.NewAdapter(), openaichatcompletions.NewAdapter(), openairesponses.NewAdapter(), googlegenerativeai.NewAdapter()} {
		t.Run(string(adapter.ID()), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "4")
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":{"message":"temporarily overloaded","type":"overloaded_error","code":"server_error"}}`))
			}))
			defer server.Close()
			model, err := adapter.New(context.Background(), providers.ModelConfig{
				Provider: providers.ProviderOpenAICompatible, Protocol: adapter.ID(), APIKey: "test-key",
				Model: "test-model", BaseURL: server.URL, HTTPClient: server.Client(),
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = model.Generate(context.Background(), []*agentschema.Message{agentschema.UserMessage("go")})
			var failure *providers.APIError
			if !errors.As(err, &failure) || !failure.Retryable() || failure.RetryDelay() != 4*time.Second || calls.Load() != 1 {
				t.Fatalf("error=%#v calls=%d", err, calls.Load())
			}
			decision := agentmodel.TransientRetry(context.Background(), agentmodel.RetryContext{Attempt: 1, Err: err, OutputState: agentmodel.ModelOutputNone})
			if decision.Action != agentmodel.RetryAgain || decision.Delay < 4*time.Second {
				t.Fatalf("retry=%#v", decision)
			}
		})
	}
}

func TestPermanentProviderFailuresDoNotRetry(t *testing.T) {
	for _, failure := range []*providers.APIError{
		{StatusCode: 401}, {StatusCode: 403}, {StatusCode: 400},
		{StatusCode: 429, Code: "insufficient_quota"}, {StatusCode: 429, Kind: "billing_hard_limit_reached"},
	} {
		decision := agentmodel.TransientRetry(context.Background(), agentmodel.RetryContext{Attempt: 1, Err: failure})
		if decision.Action != agentmodel.RetryStop {
			t.Fatalf("failure=%#v decision=%#v", failure, decision)
		}
	}
}
