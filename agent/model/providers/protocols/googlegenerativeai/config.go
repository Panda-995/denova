package googlegenerativeai

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	agentmodel "github.com/alfredxw/denova/agent/model"
	"github.com/alfredxw/denova/agent/model/providers"
	"github.com/alfredxw/denova/agent/model/providers/protocols/internal/llmhttp"
	"google.golang.org/genai"
)

type Adapter struct{}

func NewAdapter() *Adapter { return &Adapter{} }

func (*Adapter) ID() providers.ProtocolID { return providers.ProtocolGoogleGenerativeAI }

func (model *ChatModel) InputEstimator() agentmodel.InputEstimator {
	return model.config.InputEstimator()
}

type ChatModel struct {
	client        *genai.Client
	config        providers.ModelConfig
	compatibility Compatibility
	options       *agentmodel.Options
}

var (
	_ providers.ProtocolAdapter       = (*Adapter)(nil)
	_ agentmodel.ToolCallingChatModel = (*ChatModel)(nil)
)

func (*Adapter) New(ctx context.Context, config providers.ModelConfig) (agentmodel.ToolCallingChatModel, error) {
	if config.Protocol != providers.ProtocolGoogleGenerativeAI {
		return nil, fmt.Errorf("google generative AI: protocol must be %q", providers.ProtocolGoogleGenerativeAI)
	}
	if strings.TrimSpace(string(config.Provider)) == "" {
		return nil, fmt.Errorf("google generative AI: provider is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, fmt.Errorf("google generative AI: model is required")
	}
	if strings.TrimSpace(config.BaseURL) == "" {
		return nil, fmt.Errorf("google generative AI: base URL is required")
	}
	cloned, err := config.Clone()
	if err != nil {
		return nil, fmt.Errorf("google generative AI config: %w", err)
	}
	compatibility, err := resolveCompatibility(cloned)
	if err != nil {
		return nil, err
	}
	client, err := googleClient(ctx, cloned, compatibility)
	if err != nil {
		return nil, fmt.Errorf("google generative AI client: %w", err)
	}
	return &ChatModel{
		client:        client,
		config:        cloned,
		compatibility: compatibility,
		options:       &agentmodel.Options{},
	}, nil
}

func googleHTTPClient(configured *http.Client) *http.Client {
	client := llmhttp.Client(configured)
	client.Transport = errorTransport{next: client.Transport}
	return client
}

func googleClient(ctx context.Context, config providers.ModelConfig, compatibility Compatibility) (*genai.Client, error) {
	headers := make(http.Header, len(config.Headers))
	for name, value := range config.Headers {
		headers.Set(name, value)
	}
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     config.APIKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: googleHTTPClient(config.HTTPClient),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:      config.BaseURL,
			APIVersion:   compatibility.APIVersion,
			Headers:      headers,
			RetryOptions: &genai.HTTPRetryOptions{Attempts: genai.Ptr(int32(1))},
		},
	})
}
