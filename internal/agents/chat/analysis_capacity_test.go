package chat

import (
	"testing"

	"denova/config"
	"denova/internal/agents/prompts"

	"github.com/alfredxw/denova/agent"
	agentmodel "github.com/alfredxw/denova/agent/model"
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func TestInspectedContextAnalysisUsesCapacityAwareProfileOutputReserve(t *testing.T) {
	maxOutput := 4000
	window := 10_000
	disableToolContext := false
	cfg := &config.Config{
		OpenAIContextWindowTokens: window,
		AgentContexts: config.AgentContextSettings{IDE: config.AgentContextOverride{
			ToolResultContextEnabled: &disableToolContext,
		}},
	}
	messages := []*agentschema.Message{agentschema.UserMessage("short request")}
	analysis, err := BuildInspectedContextAnalysis(cfg, config.AgentKindIDE, "ide", prompts.SystemPromptComposition{}, agent.Inspection{
		ModelRequest: agentmodel.ModelRequestInspection{
			Messages: messages,
			Options:  agentmodel.Options{MaxTokens: &maxOutput},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ReservedCompletionTokens != 2500 || analysis.ReservedToolResultTokens != 0 {
		t.Fatalf("analysis reserves = completion:%d tools:%d, want 2500/0",
			analysis.ReservedCompletionTokens, analysis.ReservedToolResultTokens)
	}
	if analysis.ProjectedTokenEstimate != analysis.TokenEstimate+2500 {
		t.Fatalf("projected tokens = %d, want estimate %d + reserve 2500",
			analysis.ProjectedTokenEstimate, analysis.TokenEstimate)
	}
}

func TestLegacyContextAnalysisUsesCapacityAwareProfileOutputReserve(t *testing.T) {
	maxOutput := 4000
	window := 10_000
	disableToolContext := false
	cfg := &config.Config{
		OpenAIContextWindowTokens: window,
		ModelProfiles:             []config.ModelProfileSettings{{ID: "default", MaxTokens: &maxOutput}},
		AgentContexts: config.AgentContextSettings{InteractiveStory: config.AgentContextOverride{
			ToolResultContextEnabled: &disableToolContext,
		}},
	}
	usage, err := analyzeContextUsage(cfg, config.AgentKindInteractiveStory, "", []*agentschema.Message{agentschema.UserMessage("short request")}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if usage.completionReserve != 2500 || usage.toolResultReserve != 0 {
		t.Fatalf("legacy analysis reserves = completion:%d tools:%d, want 2500/0",
			usage.completionReserve, usage.toolResultReserve)
	}
}
