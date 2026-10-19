package lifecycle

import (
	"testing"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
)

func mustStaticTools(t testing.TB, definitions ...agenttool.ToolDefinition) agenttool.Toolset {
	t.Helper()
	set, err := agenttool.StaticTools(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func mustStaticToolsIdentified(t testing.TB, identity agentschema.CapabilityIdentity, definitions ...agenttool.ToolDefinition) agenttool.Toolset {
	t.Helper()
	set, err := agenttool.StaticToolsIdentified(identity, definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}
