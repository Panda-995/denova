package builtin

import (
	"testing"

	"github.com/alfredxw/denova/agent/model/providers"
)

func TestGoogleNativeProtocolIsSelectableWithoutChangingExistingDefaults(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	native, err := registry.Resolve(providers.ModelConfig{
		Provider: providers.ProviderGoogle, Protocol: "google-generative-ai", Model: "gemini-3.5-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if native.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Fatalf("native endpoint = %q", native.BaseURL)
	}
	existing, err := registry.Resolve(providers.ModelConfig{Provider: providers.ProviderGoogle, Model: "gemini-3.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if existing.Protocol != providers.ProtocolOpenAIChatCompletions || existing.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Fatalf("existing default changed: %#v", existing)
	}
}
