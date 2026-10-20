package decision

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"denova/config"
)

func TestEvaluateUsesSelectedModelConnectionAndCredential(t *testing.T) {
	type request struct{ Route, Key, Model string }
	var mu sync.Mutex
	var received []request
	server := func(route string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Model string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			mu.Lock()
			received = append(received, request{route, r.Header.Get("Authorization"), body.Model})
			mu.Unlock()
			_, _ = w.Write([]byte(`{"answers":{"test":{"type":"choice","choice":"yes","confidence":0.99,"probabilities":{"yes":1}}}}`))
		}))
	}
	first, second := server("first"), server("second")
	defer first.Close()
	defer second.Close()
	settings := &config.DecisionSettings{
		Enabled: true,
		Endpoints: []config.DecisionEndpointSettings{
			{ID: "first", Endpoint: first.URL, APIKey: "first-key"},
			{ID: "second", Endpoint: second.URL, APIKey: "second-key"},
		},
		Profiles: []config.DecisionProfileSettings{
			{ID: "a", EndpointID: "first", Model: "same-model"},
			{ID: "b", EndpointID: "second", Model: "same-model"},
			{ID: "c", EndpointID: "second", Model: "other-model"},
		},
	}
	question := Choice("Select yes", map[string]string{"yes": "Yes"})
	for _, id := range []string{"b", "a", "c"} {
		settings.DefaultProfileID = id
		answers, err := Evaluate(context.Background(), settings, "routing-test", "state", map[string]Question{"test": question})
		if err != nil || !answers["test"].Accepted(question, .8) {
			t.Fatalf("selected model %s failed: %v, %v", id, answers, err)
		}
	}
	disabled := false
	settings.Endpoints[1].Enabled = &disabled
	if _, err := Evaluate(context.Background(), settings, "disabled-test", nil, nil); err == nil {
		t.Fatal("disabled connection accepted a request")
	}
	mu.Lock()
	defer mu.Unlock()
	want := []request{{"second", "Bearer second-key", "same-model"}, {"first", "Bearer first-key", "same-model"}, {"second", "Bearer second-key", "other-model"}}
	if !reflect.DeepEqual(received, want) {
		t.Fatalf("requests = %#v, want %#v", received, want)
	}
}
