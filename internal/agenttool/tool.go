package agenttool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Definition struct {
	Name        string
	Description string
	Parameters  Schema
	Execute     func(context.Context, json.RawMessage) (any, error)
}

type Schema struct {
	Type                 string            `json:"type"`
	Description          string            `json:"description,omitempty"`
	Properties           map[string]Schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
}

func Object(properties map[string]Schema, required ...string) Schema {
	additionalProperties := false
	return Schema{
		Type:                 "object",
		Properties:           properties,
		Required:             required,
		AdditionalProperties: &additionalProperties,
	}
}

func String(description string) Schema {
	return Schema{Type: "string", Description: description}
}

func Boolean(description string) Schema {
	return Schema{Type: "boolean", Description: description}
}

func Integer(description string) Schema {
	return Schema{Type: "integer", Description: description}
}

type Registry struct {
	definitions []Definition
	byName      map[string]Definition
}

func NewRegistry(definitions []Definition) (*Registry, error) {
	byName := make(map[string]Definition, len(definitions))
	for _, definition := range definitions {
		if strings.TrimSpace(definition.Name) == "" {
			return nil, fmt.Errorf("tool name is required")
		}
		if definition.Execute == nil {
			return nil, fmt.Errorf("tool %q execute function is required", definition.Name)
		}
		if _, exists := byName[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate tool %q", definition.Name)
		}
		byName[definition.Name] = definition
	}
	return &Registry{definitions: append([]Definition(nil), definitions...), byName: byName}, nil
}

func (r *Registry) Definitions() []Definition {
	return append([]Definition(nil), r.definitions...)
}

func (r *Registry) FunctionTools() []FunctionTool {
	tools := make([]FunctionTool, 0, len(r.definitions))
	for _, definition := range r.definitions {
		tools = append(tools, FunctionTool{
			Type: "function",
			Function: FunctionDefinition{
				Name:        definition.Name,
				Description: definition.Description,
				Parameters:  definition.Parameters,
			},
		})
	}
	return tools
}

func (r *Registry) Execute(ctx context.Context, name string, arguments json.RawMessage) (any, error) {
	definition, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	if err := validateArguments(name, definition.Parameters, arguments); err != nil {
		return nil, err
	}
	return definition.Execute(ctx, arguments)
}

type FunctionTool struct {
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

type FunctionDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  Schema `json:"parameters"`
}

func validateArguments(toolName string, schema Schema, arguments json.RawMessage) error {
	if schema.Type != "object" {
		return nil
	}

	var values map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &values); err != nil {
		return fmt.Errorf("parse %s arguments: %w", toolName, err)
	}

	for _, key := range schema.Required {
		if _, ok := values[key]; !ok {
			return fmt.Errorf("parse %s arguments: missing required field %q", toolName, key)
		}
	}

	if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
		for key := range values {
			if _, ok := schema.Properties[key]; !ok {
				return fmt.Errorf("parse %s arguments: unknown field %q", toolName, key)
			}
		}
	}

	for key, value := range values {
		property, ok := schema.Properties[key]
		if !ok {
			continue
		}
		if err := validateValue(property, value); err != nil {
			return fmt.Errorf("parse %s arguments: field %q: %w", toolName, key, err)
		}
	}

	return nil
}

func validateValue(schema Schema, value json.RawMessage) error {
	switch schema.Type {
	case "string":
		var out string
		if err := json.Unmarshal(value, &out); err != nil {
			return fmt.Errorf("must be a string")
		}
	case "boolean":
		var out bool
		if err := json.Unmarshal(value, &out); err != nil {
			return fmt.Errorf("must be a boolean")
		}
	case "integer":
		var out int64
		if err := json.Unmarshal(value, &out); err != nil {
			return fmt.Errorf("must be an integer")
		}
	case "object":
		return validateArguments("nested object", schema, value)
	}
	return nil
}
