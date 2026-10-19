// Package tool defines tool execution contracts, schemas, registration, argument
// validation, artifacts, and invocation services. Built-in tools live in tools.
package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	agentschema "github.com/alfredxw/denova/agent/schema"
	"github.com/invopop/jsonschema"
)

// ToolOption is reserved for per-invocation, provider-neutral tool settings.
// Its named type prevents provider SDK options from leaking into this seam.
type ToolOption struct {
	Key   string
	Value any
}

// InvokeFunc is a typed tool implementation.
type InvokeFunc[T, D any] func(ctx context.Context, input T) (D, error)

// SchemaModifierFn maps application-specific struct tags into JSON Schema.
type SchemaModifierFn func(jsonTagName string, fieldType reflect.Type, tag reflect.StructTag, schema *jsonschema.Schema)

// UnmarshalArguments customizes typed argument decoding.
type UnmarshalArguments func(ctx context.Context, arguments string) (any, error)

// MarshalOutput customizes the structured result produced by a typed tool.
type MarshalOutput func(ctx context.Context, output any) (agentschema.ToolResult, error)

type inferOptions struct {
	schemaModifier     SchemaModifierFn
	unmarshalArguments UnmarshalArguments
	marshalOutput      MarshalOutput
}

// InferOption configures InferTool and GoStruct2 helpers.
type InferOption func(*inferOptions)

// WithSchemaModifier applies a custom modifier after reflection.
func WithSchemaModifier(modifier SchemaModifierFn) InferOption {
	return func(options *inferOptions) {
		options.schemaModifier = modifier
	}
}

// WithUnmarshalArguments opts into application-defined argument decoding.
func WithUnmarshalArguments(unmarshal UnmarshalArguments) InferOption {
	return func(options *inferOptions) {
		options.unmarshalArguments = unmarshal
	}
}

// WithMarshalOutput opts into application-defined result encoding.
func WithMarshalOutput(marshal MarshalOutput) InferOption {
	return func(options *inferOptions) {
		options.marshalOutput = marshal
	}
}

func collectInferOptions(opts []InferOption) *inferOptions {
	options := &inferOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
	return options
}

// GoStruct2ParamsOneOf reflects T into an inline, provider-visible schema.
func GoStruct2ParamsOneOf[T any](opts ...InferOption) (*agentschema.ParamsOneOf, error) {
	options := collectInferOptions(opts)
	typeOfT := reflect.TypeFor[T]()
	reflector := &jsonschema.Reflector{Anonymous: true, DoNotReference: true}
	schema := reflector.ReflectFromType(typeOfT)
	if schema == nil {
		return nil, fmt.Errorf("reflect tool schema for %v: empty schema", typeOfT)
	}
	schema.Version = ""
	schema.ID = ""
	schema.Ref = ""
	schema.Definitions = nil
	if options.schemaModifier != nil {
		applySchemaModifier(typeOfT, reflect.StructTag(""), "_root", schema, options.schemaModifier)
	}
	return agentschema.NewParamsOneOfByJSONSchema(schema), nil
}

// GoStruct2ToolInfo reflects T and attaches the supplied stable tool identity.
func GoStruct2ToolInfo[T any](name, description string, opts ...InferOption) (*agentschema.ToolInfo, error) {
	params, err := GoStruct2ParamsOneOf[T](opts...)
	if err != nil {
		return nil, err
	}
	return &agentschema.ToolInfo{Name: name, Desc: description, ParamsOneOf: params}, nil
}

// InferTool creates a strict typed tool.
func InferTool[T, D any](name, description string, invoke InvokeFunc[T, D], opts ...InferOption) (Tool, error) {
	if strings.TrimSpace(name) == "" {
		return nil, errors.New("infer tool: name is required")
	}
	if invoke == nil {
		return nil, fmt.Errorf("infer tool %q: invoke function is required", name)
	}
	info, err := GoStruct2ToolInfo[T](name, description, opts...)
	if err != nil {
		return nil, fmt.Errorf("infer tool %q: %w", name, err)
	}
	schema, err := info.ToJSONSchema()
	if err != nil {
		return nil, fmt.Errorf("infer tool %q schema: %w", name, err)
	}
	return &inferredTool[T, D]{
		info:    info,
		schema:  schema,
		invoke:  invoke,
		options: collectInferOptions(opts),
	}, nil
}

// NewTool binds a typed function to an explicit ToolInfo.
func NewTool[T, D any](info *agentschema.ToolInfo, invoke InvokeFunc[T, D], opts ...InferOption) Tool {
	var schema *jsonschema.Schema
	if info != nil {
		schema, _ = info.ToJSONSchema()
	}
	return &inferredTool[T, D]{info: agentschema.CloneToolInfo(info), schema: schema, invoke: invoke, options: collectInferOptions(opts)}
}

type inferredTool[T, D any] struct {
	info    *agentschema.ToolInfo
	schema  *jsonschema.Schema
	invoke  InvokeFunc[T, D]
	options *inferOptions
}

func (tool *inferredTool[T, D]) Info(context.Context) (*agentschema.ToolInfo, error) {
	return agentschema.CloneToolInfo(tool.info), nil
}

func (tool *inferredTool[T, D]) Run(ctx context.Context, arguments string, _ ...ToolOption) (agentschema.ToolResult, error) {
	var input T
	normalizedArguments, err := normalizeToolArgumentsWithSchema(arguments, tool.schema)
	if err != nil {
		return agentschema.ToolResult{}, fmt.Errorf("normalize arguments for tool %q: %w", toolName(tool.info), err)
	}
	if tool.options != nil && tool.options.unmarshalArguments != nil {
		decoded, err := tool.options.unmarshalArguments(ctx, normalizedArguments)
		if err != nil {
			return agentschema.ToolResult{}, fmt.Errorf("decode arguments for tool %q: %w", toolName(tool.info), err)
		}
		value, ok := decoded.(T)
		if !ok {
			return agentschema.ToolResult{}, fmt.Errorf("decode arguments for tool %q: got %T, want %T", toolName(tool.info), decoded, input)
		}
		input = value
	} else if err := json.Unmarshal([]byte(normalizedArguments), &input); err != nil {
		return agentschema.ToolResult{}, fmt.Errorf("decode arguments for tool %q: %w", toolName(tool.info), err)
	}

	output, err := tool.invoke(ctx, input)
	if err != nil {
		wrapped := fmt.Errorf("invoke tool %q: %w", toolName(tool.info), err)
		// A tool can commit its domain effect and then fail while reporting or
		// finalizing it. Preserve a structured terminal receipt for lifecycle
		// middleware instead of replacing it with an empty result.
		if value, ok := any(output).(agentschema.ToolResult); ok {
			return value, wrapped
		}
		if value, ok := any(output).(*agentschema.ToolResult); ok && value != nil {
			return *value, wrapped
		}
		return agentschema.ToolResult{}, wrapped
	}
	if tool.options != nil && tool.options.marshalOutput != nil {
		result, err := tool.options.marshalOutput(ctx, output)
		if err != nil {
			return agentschema.ToolResult{}, fmt.Errorf("encode result for tool %q: %w", toolName(tool.info), err)
		}
		return result, nil
	}
	if value, ok := any(output).(agentschema.ToolResult); ok {
		return value, nil
	}
	if value, ok := any(output).(*agentschema.ToolResult); ok {
		if value == nil {
			return agentschema.ToolResult{}, fmt.Errorf("encode result for tool %q: nil ToolResult", toolName(tool.info))
		}
		return *value, nil
	}
	if value, ok := any(output).(string); ok {
		return agentschema.TextToolResult(value), nil
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		return agentschema.ToolResult{}, fmt.Errorf("encode result for tool %q: %w", toolName(tool.info), err)
	}
	return agentschema.TextToolResult(string(encoded)), nil
}

// validateToolSchema rejects malformed schemas at the registry boundary,
// before a provider can see a tool that the local final-argument validator
// cannot interpret consistently.
func validateToolSchema(schema *jsonschema.Schema) error {
	if schema == nil {
		return nil
	}
	if _, err := json.Marshal(schema); err != nil {
		return fmt.Errorf("encode JSON schema: %w", err)
	}
	return validateToolSchemaAt("$", schema)
}

func validateToolSchemaAt(path string, schema *jsonschema.Schema) error {
	if schema == nil {
		return fmt.Errorf("%s contains a nil schema", path)
	}
	switch schema.Type {
	case "", "object", "array", "string", "boolean", "number", "integer", "null":
	default:
		return fmt.Errorf("%s has unsupported type %q", path, schema.Type)
	}
	if schema.Pattern != "" {
		if _, err := regexp.Compile(schema.Pattern); err != nil {
			return fmt.Errorf("%s has invalid pattern: %w", path, err)
		}
	}
	if schema.MinLength != nil && schema.MaxLength != nil && *schema.MinLength > *schema.MaxLength {
		return fmt.Errorf("%s has minLength greater than maxLength", path)
	}
	if schema.MinItems != nil && schema.MaxItems != nil && *schema.MinItems > *schema.MaxItems {
		return fmt.Errorf("%s has minItems greater than maxItems", path)
	}
	if schema.MinProperties != nil && schema.MaxProperties != nil && *schema.MinProperties > *schema.MaxProperties {
		return fmt.Errorf("%s has minProperties greater than maxProperties", path)
	}
	children := []struct {
		name   string
		values []*jsonschema.Schema
	}{
		{name: "allOf", values: schema.AllOf},
		{name: "anyOf", values: schema.AnyOf},
		{name: "oneOf", values: schema.OneOf},
		{name: "prefixItems", values: schema.PrefixItems},
	}
	for _, group := range children {
		for index, child := range group.values {
			if err := validateToolSchemaAt(fmt.Sprintf("%s.%s[%d]", path, group.name, index), child); err != nil {
				return err
			}
		}
	}
	single := []struct {
		name  string
		value *jsonschema.Schema
	}{
		{name: "not", value: schema.Not}, {name: "if", value: schema.If},
		{name: "then", value: schema.Then}, {name: "else", value: schema.Else},
		{name: "items", value: schema.Items}, {name: "contains", value: schema.Contains},
		{name: "additionalProperties", value: schema.AdditionalProperties},
		{name: "propertyNames", value: schema.PropertyNames},
		{name: "contentSchema", value: schema.ContentSchema},
	}
	for _, child := range single {
		if child.value != nil {
			if err := validateToolSchemaAt(path+"."+child.name, child.value); err != nil {
				return err
			}
		}
	}
	if schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			if err := validateToolSchemaAt(path+".properties."+pair.Key, pair.Value); err != nil {
				return err
			}
		}
	}
	maps := []struct {
		name   string
		values map[string]*jsonschema.Schema
	}{
		{name: "$defs", values: schema.Definitions},
		{name: "dependentSchemas", values: schema.DependentSchemas},
		{name: "patternProperties", values: schema.PatternProperties},
	}
	for _, group := range maps {
		for name, child := range group.values {
			if err := validateToolSchemaAt(path+"."+group.name+"."+name, child); err != nil {
				return err
			}
		}
	}
	return nil
}

func toolName(info *agentschema.ToolInfo) string {
	if info == nil {
		return ""
	}
	return info.Name
}

func applySchemaModifier(t reflect.Type, tag reflect.StructTag, name string, schema *jsonschema.Schema, modifier SchemaModifierFn) {
	if schema == nil || t == nil {
		return
	}
	modifier(name, t, tag, schema)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		applySchemaModifier(t.Elem(), tag, name, schema.Items, modifier)
		return
	}
	if t.Kind() != reflect.Struct || schema.Properties == nil {
		return
	}
	for index := 0; index < t.NumField(); index++ {
		field := t.Field(index)
		if field.PkgPath != "" {
			continue
		}
		jsonName := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name
		}
		property, exists := schema.Properties.Get(jsonName)
		if !exists {
			continue
		}
		applySchemaModifier(field.Type, field.Tag, jsonName, property, modifier)
	}
}
