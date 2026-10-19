package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	agentscript "github.com/alfredxw/denova/agent/tool/script"
	"github.com/invopop/jsonschema"
)

const (
	ScriptToolName = "script"
)

// ScriptConfig controls the Tool wrapper. Engine owns source/output limits;
// Timeout is optional and zero deliberately means unlimited.
type ScriptConfig struct {
	Engine         *agentscript.Engine
	MaxResultBytes int
	Timeout        time.Duration
}

// Script constructs the immediate model-visible orchestration tool.
func Script(config ScriptConfig) (agenttool.ToolDefinition, error) {
	if err := validateScriptConfig(config); err != nil {
		return agenttool.ToolDefinition{}, err
	}
	schema := &jsonschema.Schema{}
	if err := json.Unmarshal([]byte(`{
  "type":"object",
  "additionalProperties":false,
  "required":["source"],
  "properties":{
    "source":{"type":"string","minLength":1,"description":"Synchronous JavaScript function body to execute."},
    "input":{"type":"object","description":"Optional JSON input exposed to the script as input."}
  }
}`), schema); err != nil {
		return agenttool.ToolDefinition{}, fmt.Errorf("build script schema: %w", err)
	}
	tool := &scriptTool{
		name: ScriptToolName,
		description: "Execute a synchronous JavaScript function body that orchestrates the tools available to this Agent. " +
			"Use ctx.tools.call(name, input) for one call and ctx.tools.parallel(calls) for an ordered batch. " +
			"Each call returns {tool, ok, status, output, truncated, artifacts, reason}. " +
			"Return one JSON-compatible value. Scripts cannot invoke the script tool recursively.",
		schema: schema, engine: config.Engine, timeout: config.Timeout,
	}
	return agenttool.ToolDefinition{
		Tool: tool, Descriptor: scriptDescriptor(ScriptToolName, config.MaxResultBytes),
		ImplementationIdentity: agentschema.CapabilityIdentity{
			Kind: "tools.script", Version: agentscript.ContractVersion,
		},
	}, nil
}

func validateScriptConfig(config ScriptConfig) error {
	if config.Engine == nil {
		return errors.New("Script Tool requires an Engine")
	}
	if config.MaxResultBytes <= 0 {
		return errors.New("Script Tool result limit must be positive")
	}
	if config.Timeout < 0 {
		return errors.New("Script Tool timeout cannot be negative")
	}
	return nil
}

func scriptDescriptor(capability string, maxResultBytes int) agenttool.ToolDescriptor {
	return agenttool.ToolDescriptor{
		Source: agenttool.ToolSourceOther, Capability: capability, Execution: agenttool.ToolExecutionChild,
		MutationScope: agenttool.ToolMutationNone, PostCheck: agenttool.ToolPostCheckNone,
		Recovery: agenttool.ToolRecoveryNonIdempotent, ResultProjection: agentschema.ToolResultBoundedModelContext,
		ResultRetention: agentschema.ToolResultProtected, Steering: agenttool.SteeringFinishCurrent,
		MaxResultBytes: maxResultBytes, Presentation: agenttool.UniformToolPresentation(agenttool.ToolPresentationScript),
	}
}

type scriptTool struct {
	name        string
	description string
	schema      *jsonschema.Schema
	engine      *agentscript.Engine
	timeout     time.Duration
}

func (tool *scriptTool) Info(context.Context) (*agentschema.ToolInfo, error) {
	if tool == nil || tool.engine == nil || tool.schema == nil {
		return nil, errors.New("Script Tool is not configured")
	}
	return &agentschema.ToolInfo{
		Name: tool.name, Desc: tool.description, ParamsOneOf: agentschema.NewParamsOneOfByJSONSchema(tool.schema),
	}, nil
}

func (tool *scriptTool) Run(ctx context.Context, arguments string, _ ...agenttool.ToolOption) (agentschema.ToolResult, error) {
	if tool == nil || tool.engine == nil {
		return agentschema.ToolResult{}, errors.New("Script Tool is not configured")
	}
	if containsScriptTool(ctx, tool.name) {
		return agenttool.SyntheticToolResult(
			agentschema.ToolResultBlocked, agentschema.ToolSyntheticPolicyBlocked,
			fmt.Sprintf("Script Tool %q cannot call itself directly or indirectly.", tool.name),
		), nil
	}
	ctx = contextWithScriptTool(ctx, tool.name)
	var request struct {
		Source string          `json:"source"`
		Input  json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal([]byte(arguments), &request); err != nil {
		return agentschema.ToolResult{}, fmt.Errorf("decode script arguments: %w", err)
	}
	if len(request.Input) == 0 {
		request.Input = json.RawMessage(`{}`)
	}
	program, diagnostics := tool.engine.Compile(ctx, agentscript.Source{Name: "script.js", Code: request.Source})
	if len(diagnostics) != 0 {
		return scriptDiagnosticsResult(diagnostics), nil
	}
	runContext := ctx
	stop := func() {}
	if tool.timeout > 0 {
		runContext, stop = context.WithTimeout(ctx, tool.timeout)
	}
	defer stop()
	host := &scriptHost{}
	run, err := tool.engine.Run(runContext, program, host, request.Input)
	if err != nil {
		return agentschema.ToolResult{}, err
	}
	if host.effectUnknown {
		return agenttool.SyntheticToolResult(
			agentschema.ToolResultBlocked, agentschema.ToolSyntheticEffectUnknown,
			"A nested tool may have changed state without a confirmed receipt; inspect the recorded tool call before retrying.",
		), nil
	}
	return projectScriptRun(run), nil
}

func scriptDiagnosticsResult(diagnostics []agentscript.Diagnostic) agentschema.ToolResult {
	encoded, err := json.Marshal(struct {
		Error       string                   `json:"error"`
		Diagnostics []agentscript.Diagnostic `json:"diagnostics"`
	}{Error: "Script compilation failed.", Diagnostics: diagnostics})
	if err != nil {
		return agenttool.ToolErrorResult("Script compilation failed.", "Script compilation failed.")
	}
	result := agenttool.ToolErrorResult(string(encoded), string(encoded))
	result.Details = append(json.RawMessage(nil), encoded...)
	return result
}

func projectScriptRun(run agentscript.RunResult) agentschema.ToolResult {
	if run.Failure != nil {
		encoded, err := json.Marshal(struct {
			Error *agentscript.Failure `json:"error"`
			Logs  []string             `json:"logs,omitempty"`
		}{Error: run.Failure, Logs: run.Logs})
		if err != nil {
			return agenttool.ToolErrorResult(run.Failure.Message, run.Failure.Message)
		}
		result := agenttool.ToolErrorResult(string(encoded), string(encoded))
		result.Details = append(json.RawMessage(nil), encoded...)
		return result
	}
	model := string(run.Value)
	display := model
	if len(run.Logs) != 0 {
		display = strings.Join(run.Logs, "\n") + "\n\n" + model
	}
	result := agentschema.TextToolResult(model)
	result.DisplayContent = display
	if details, err := json.Marshal(run); err == nil {
		result.Details = details
	}
	return result
}

type scriptToolStackKey struct{}

func contextWithScriptTool(ctx context.Context, name string) context.Context {
	stack, _ := ctx.Value(scriptToolStackKey{}).([]string)
	next := append(append([]string(nil), stack...), name)
	return context.WithValue(ctx, scriptToolStackKey{}, next)
}

func containsScriptTool(ctx context.Context, name string) bool {
	stack, _ := ctx.Value(scriptToolStackKey{}).([]string)
	for _, current := range stack {
		if current == name {
			return true
		}
	}
	return false
}

type scriptHost struct {
	effectUnknown bool
}

func (host *scriptHost) CallTools(ctx context.Context, calls []agentscript.Call) ([]agentscript.Outcome, error) {
	outcomes := make([]agentscript.Outcome, len(calls))
	allowed := make([]agenttool.NestedToolCall, 0, len(calls))
	indices := make([]int, 0, len(calls))
	for index, call := range calls {
		if reason := blockedScriptHostCall(call); reason != "" {
			outcomes[index] = agentscript.Outcome{
				Tool: call.Name, OK: false, Status: string(agentschema.ToolResultBlocked),
				Output: mustJSON(reason), Reason: string(agentschema.ToolSyntheticPolicyBlocked),
			}
			continue
		}
		indices = append(indices, index)
		allowed = append(allowed, agenttool.NestedToolCall{Name: call.Name, Arguments: call.Arguments})
	}
	if len(allowed) == 0 {
		return outcomes, nil
	}
	nested, err := agenttool.CallNestedTools(ctx, allowed)
	if err != nil {
		return nil, err
	}
	if len(nested) != len(allowed) {
		return nil, fmt.Errorf("nested executor returned %d outcomes for %d calls", len(nested), len(allowed))
	}
	for offset, result := range nested {
		if result.Reason == agentschema.ToolSyntheticEffectUnknown {
			host.effectUnknown = true
		}
		artifacts := make([]agentscript.Artifact, len(result.Artifacts))
		for index, artifact := range result.Artifacts {
			artifacts[index] = agentscript.Artifact{
				ID: artifact.ID, ReadablePath: artifact.ReadablePath, ContentType: artifact.ContentType, Complete: artifact.Complete,
			}
		}
		outcomes[indices[offset]] = agentscript.Outcome{
			Tool: result.Name, OK: result.Status == agentschema.ToolResultSuccess, Status: string(result.Status),
			Output: append(json.RawMessage(nil), result.Output...), Truncated: result.Truncated,
			Artifacts: artifacts, Reason: string(result.Reason),
		}
	}
	return outcomes, nil
}

func blockedScriptHostCall(call agentscript.Call) string {
	if strings.TrimSpace(call.Name) == ScriptToolName {
		return "Scripts cannot invoke the immediate script tool."
	}
	return ""
}

func mustJSON(value string) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}
