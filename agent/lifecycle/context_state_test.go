package lifecycle

import (
	agentschema "github.com/alfredxw/denova/agent/schema"
)

func testContextStateFragment(revision, content string) agentschema.ContextFragment {
	return agentschema.ContextFragment{
		Source: "test.workspace", Purpose: "provide current workspace state", Resource: "workspace",
		Revision: revision, StateID: "workspace", Stability: agentschema.ContextSessionState, Placement: agentschema.ContextStateMessage,
		Rendering: agentschema.ContextRenderVerbatim, Content: content, HardLimit: 64 << 10,
	}
}
