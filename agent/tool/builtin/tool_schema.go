package builtin

import (
	"fmt"

	agentschema "github.com/alfredxw/denova/agent/schema"
	agenttool "github.com/alfredxw/denova/agent/tool"
	"github.com/invopop/jsonschema"
)

func reflectedToolSchema[T any]() (*jsonschema.Schema, error) {
	params, err := agenttool.GoStruct2ParamsOneOf[T]()
	if err != nil {
		return nil, err
	}
	return params.ToJSONSchema()
}

func newSchemaTool[T, D any](name, description string, schema *jsonschema.Schema, invoke agenttool.InvokeFunc[T, D]) (agenttool.Tool, error) {
	if schema == nil {
		return nil, fmt.Errorf("build %s tool: schema is nil", name)
	}
	info := &agentschema.ToolInfo{
		Name: name, Desc: description, ParamsOneOf: agentschema.NewParamsOneOfByJSONSchema(schema),
	}
	return agenttool.NewTool(info, invoke), nil
}

// Batch tools validate each item themselves, preserving successful siblings.
func batchToolSchema[T any](field string) (*jsonschema.Schema, error) {
	schema, err := reflectedToolSchema[T]()
	if err != nil {
		return nil, err
	}
	array, ok := schema.Properties.Get(field)
	if !ok || array.Items == nil {
		return nil, fmt.Errorf("batch field %q has no item schema", field)
	}
	array.Items.Comments = "agent:independent-batch-item"
	return schema, nil
}
