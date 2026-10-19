package agents

import (
	"context"
	"strings"
	"testing"

	"denova/config"
	agentinteractive "denova/internal/agents/interactive"
	"denova/internal/agents/prompts"
	producttools "denova/internal/agents/tools"

	agenttool "github.com/alfredxw/denova/agent/tool"
)

func TestExternalAssembliesExcludeNativeCoordination(t *testing.T) {
	for _, runtime := range []config.RuntimeSelection{
		{Kind: config.RuntimeCodex, Codex: &config.CodexRuntimeSettings{Model: "test-model"}},
		{Kind: config.RuntimeClaude, Claude: &config.ClaudeRuntimeSettings{Model: "test-model"}},
	} {
		for _, kind := range config.SubAgentParentKinds() {
			t.Run(string(runtime.Kind)+"/"+kind, func(t *testing.T) {
				cfg := &config.Config{
					Workspace: t.TempDir(), DenovaDir: t.TempDir(),
					ActiveAgentRuntime: &runtime,
					// Saved Native delegation must stay dormant for external engines.
					AgentTools: config.AgentToolSettings{
						Default:          config.AgentToolOverride{config.AgentToolDelegation: true},
						InteractiveStory: config.AgentToolOverride{config.AgentToolDelegation: true},
					},
				}
				called := false
				var bound *externalPluginToolset
				plugin, err := agenttool.InferTool("plugin_validation", "Read a plugin result.", func(context.Context, struct{}) (string, error) {
					if err := bound.owner.Err(); err != nil {
						return "", err
					}
					called = true
					return "plugin result", nil
				})
				if err != nil {
					t.Fatal(err)
				}
				descriptor := producttools.BoundedReadDescriptor(agenttool.ToolSourceOther, "")
				pluginTools, err := agenttool.StaticTools(agenttool.ToolDefinition{Tool: plugin, Descriptor: descriptor})
				if err != nil {
					t.Fatal(err)
				}
				bound = &externalPluginToolset{Toolset: pluginTools}
				host := AgentHostCapabilities{Interactive: true, PluginTools: bound}
				admission, cancelAdmission := context.WithCancel(t.Context())
				defer cancelAdmission()
				var assembly ExternalAssembly
				if kind == config.AgentKindInteractiveStory {
					assembly, err = BuildExternalGameAssembly(admission, cfg, nil, prompts.InteractiveStorySystemInstructionInput{}, host, agentinteractive.InteractiveStoryToolContext{})
				} else {
					assembly, err = BuildExternalConversationAssembly(admission, cfg, nil, prompts.IDEStoryTeller{}, kind, host)
				}
				if err != nil {
					t.Fatal(err)
				}
				cancelAdmission()
				if bound.owner != nil {
					t.Fatal("plugin tools must not bind to admission context")
				}
				definitions, err := assembly.PrepareTools(t.Context())
				if err != nil {
					t.Fatal(err)
				}
				instruction := assembly.Composition.Instruction()
				for _, marker := range []string{"send delegate", "list_agents", "await is a readiness synchronization point", "TASK_RESULT"} {
					if strings.Contains(instruction, marker) {
						t.Errorf("external instruction leaked Native coordination guidance %q", marker)
					}
				}
				for _, required := range []string{"Denova Runtime Contract", "## Output Protocol", "## Language Alignment"} {
					if !strings.Contains(instruction, required) {
						t.Errorf("external instruction lost shared product rules %q", required)
					}
				}
				if assembly.ToolSettings.Allows(config.AgentToolDelegation) || len(assembly.Tools) == 0 {
					t.Fatal("external assembly must expose product tools without Native delegation")
				}
				for _, definition := range definitions {
					info, err := definition.Tool.Info(t.Context())
					if err != nil {
						t.Fatal(err)
					}
					switch info.Name {
					case "plugin_validation":
						if definition.Descriptor != descriptor {
							t.Fatal("external assembly changed the plugin tool contract")
						}
						if _, err := definition.Tool.Run(t.Context(), "{}"); err != nil {
							t.Fatal(err)
						}
					case "send", "await", "list_agents", "task", "task_wait", "todo":
						t.Errorf("external assembly exposed Native coordination tool %q", info.Name)
					}
				}
				if !called {
					t.Fatal("external assembly must expose and dispatch host plugin tools")
				}
			})
		}
	}
}

type externalPluginToolset struct {
	agenttool.Toolset
	owner context.Context
}

func (tools *externalPluginToolset) PrepareTools(ctx context.Context, request agenttool.ToolRequest) ([]agenttool.ToolDefinition, error) {
	tools.owner = ctx
	return tools.Toolset.PrepareTools(ctx, request)
}
