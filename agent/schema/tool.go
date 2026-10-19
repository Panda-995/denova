package schema

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/invopop/jsonschema"
)

// DataType is a JSON Schema primitive type.
type DataType string

const (
	Object  DataType = "object"
	Number  DataType = "number"
	Integer DataType = "integer"
	String  DataType = "string"
	Array   DataType = "array"
	Null    DataType = "null"
	Boolean DataType = "boolean"
)

// ParameterInfo is the compact form of a tool parameter definition.
type ParameterInfo struct {
	Type      DataType
	ElemInfo  *ParameterInfo
	SubParams map[string]*ParameterInfo
	Desc      string
	Enum      []string
	Required  bool
}

// ParamsOneOf contains either compact parameters or a full JSON Schema.
type ParamsOneOf struct {
	params map[string]*ParameterInfo
	schema *jsonschema.Schema
}

// NewParamsOneOfByParams constructs a compact parameter schema.
func NewParamsOneOfByParams(params map[string]*ParameterInfo) *ParamsOneOf {
	return &ParamsOneOf{params: cloneParameterMap(params)}
}

// NewParamsOneOfByJSONSchema preserves a full schema, including oneOf.
func NewParamsOneOfByJSONSchema(schema *jsonschema.Schema) *ParamsOneOf {
	return &ParamsOneOf{schema: cloneJSONSchema(schema)}
}

// ToJSONSchema returns a provider-visible, inline JSON Schema.
func (params *ParamsOneOf) ToJSONSchema() (*jsonschema.Schema, error) {
	if params == nil {
		return nil, nil
	}
	if params.schema != nil {
		return cloneJSONSchema(params.schema), nil
	}
	properties := make(map[string]any, len(params.params))
	required := make([]string, 0, len(params.params))
	keys := make([]string, 0, len(params.params))
	for key := range params.params {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		properties[key] = parameterSchemaMap(params.params[key])
		if params.params[key] != nil && params.params[key].Required {
			required = append(required, key)
		}
	}
	value := map[string]any{
		"type":       string(Object),
		"properties": properties,
	}
	if len(required) != 0 {
		value["required"] = required
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal compact parameter schema: %w", err)
	}
	result := &jsonschema.Schema{}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, fmt.Errorf("decode compact parameter schema: %w", err)
	}
	return result, nil
}

// ToJSONSchemaMap returns the object shape required by provider SDKs. The outer
// map is intentionally unordered; ordering that matters to tool generation is
// retained inside the raw "properties" value and embedded verbatim on the wire.
func (params *ParamsOneOf) ToJSONSchemaMap() (map[string]any, error) {
	schema, err := params.ToJSONSchema()
	if err != nil || schema == nil {
		return nil, err
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil, fmt.Errorf("marshal JSON Schema: %w", err)
	}
	rawFields := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &rawFields); err != nil {
		return nil, fmt.Errorf("decode JSON Schema object: %w", err)
	}
	result := make(map[string]any, len(rawFields))
	for name, value := range rawFields {
		result[name] = value
	}
	return result, nil
}

func parameterSchemaMap(parameter *ParameterInfo) map[string]any {
	if parameter == nil {
		return map[string]any{}
	}
	result := map[string]any{"type": string(parameter.Type)}
	if parameter.Desc != "" {
		result["description"] = parameter.Desc
	}
	if len(parameter.Enum) != 0 {
		result["enum"] = append([]string(nil), parameter.Enum...)
	}
	if parameter.ElemInfo != nil {
		result["items"] = parameterSchemaMap(parameter.ElemInfo)
	}
	if len(parameter.SubParams) != 0 {
		properties := make(map[string]any, len(parameter.SubParams))
		required := make([]string, 0, len(parameter.SubParams))
		keys := make([]string, 0, len(parameter.SubParams))
		for key := range parameter.SubParams {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			properties[key] = parameterSchemaMap(parameter.SubParams[key])
			if parameter.SubParams[key] != nil && parameter.SubParams[key].Required {
				required = append(required, key)
			}
		}
		result["properties"] = properties
		if len(required) != 0 {
			result["required"] = required
		}
	}
	return result
}

// ToolInfo is the stable provider-neutral description of a tool.
type ToolInfo struct {
	Name  string
	Desc  string
	Extra map[string]any
	*ParamsOneOf
}

type toolInfoJSON struct {
	Name           string                    `json:"name,omitempty"`
	Desc           string                    `json:"desc,omitempty"`
	Extra          map[string]any            `json:"extra,omitempty"`
	HasParamsOneOf bool                      `json:"has_params_one_of,omitempty"`
	Params         map[string]*ParameterInfo `json:"params,omitempty"`
	JSONSchema     *jsonschema.Schema        `json:"json_schema,omitempty"`
}

// MarshalJSON preserves the stable ToolInfo persistence shape.
func (info *ToolInfo) MarshalJSON() ([]byte, error) {
	if info == nil {
		return []byte("null"), nil
	}
	wire := toolInfoJSON{Name: info.Name, Desc: info.Desc, Extra: info.Extra}
	if info.ParamsOneOf != nil {
		wire.HasParamsOneOf = true
		wire.Params = info.ParamsOneOf.params
		wire.JSONSchema = info.ParamsOneOf.schema
	}
	return json.Marshal(wire)
}

// UnmarshalJSON accepts the stable ToolInfo persistence shape.
func (info *ToolInfo) UnmarshalJSON(data []byte) error {
	wire := toolInfoJSON{}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	info.Name = wire.Name
	info.Desc = wire.Desc
	info.Extra = wire.Extra
	info.ParamsOneOf = nil
	if wire.HasParamsOneOf {
		info.ParamsOneOf = &ParamsOneOf{params: wire.Params, schema: wire.JSONSchema}
		if info.ParamsOneOf.params == nil && info.ParamsOneOf.schema == nil {
			info.ParamsOneOf.params = map[string]*ParameterInfo{}
		}
	}
	return nil
}

func CloneToolInfos(values []*ToolInfo) []*ToolInfo {
	if values == nil {
		return nil
	}
	result := make([]*ToolInfo, len(values))
	for index, value := range values {
		result[index] = CloneToolInfo(value)
	}
	return result
}

func CloneToolInfo(info *ToolInfo) *ToolInfo {
	if info == nil {
		return nil
	}
	clone := *info
	clone.Extra = CloneStringAnyMap(info.Extra)
	if info.ParamsOneOf != nil {
		clone.ParamsOneOf = &ParamsOneOf{
			params: cloneParameterMap(info.ParamsOneOf.params),
			schema: cloneJSONSchema(info.ParamsOneOf.schema),
		}
	}
	return &clone
}

func cloneParameterMap(values map[string]*ParameterInfo) map[string]*ParameterInfo {
	if values == nil {
		return nil
	}
	result := make(map[string]*ParameterInfo, len(values))
	for key, value := range values {
		result[key] = cloneParameterInfo(value)
	}
	return result
}

func cloneParameterInfo(info *ParameterInfo) *ParameterInfo {
	if info == nil {
		return nil
	}
	clone := *info
	clone.Enum = append([]string(nil), info.Enum...)
	clone.ElemInfo = cloneParameterInfo(info.ElemInfo)
	clone.SubParams = cloneParameterMap(info.SubParams)
	return &clone
}

func cloneJSONSchema(schema *jsonschema.Schema) *jsonschema.Schema {
	if schema == nil {
		return nil
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return schema
	}
	clone := &jsonschema.Schema{}
	if err := json.Unmarshal(data, clone); err != nil {
		return schema
	}
	return clone
}
