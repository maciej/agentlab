package agenttool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"

	"agentlab/internal/sandboxfs"
)

func TestSandboxRegistryExecutesSearchText(t *testing.T) {
	workspace := sandboxfs.New(fstest.MapFS{
		"README.md": {Data: []byte("codename: aurora\n")},
	})
	registry, err := NewSandboxRegistry(workspace)
	if err != nil {
		t.Fatal(err)
	}

	result, err := registry.Execute(
		context.Background(),
		"search_text",
		json.RawMessage(`{"query":"aurora","path":"."}`),
	)
	if err != nil {
		t.Fatal(err)
	}

	grepResult, ok := result.(sandboxfs.GrepResult)
	if !ok {
		t.Fatalf("result type = %T, want sandboxfs.GrepResult", result)
	}
	if len(grepResult.Matches) != 1 {
		t.Fatalf("matches = %d, want 1", len(grepResult.Matches))
	}
}

func TestRegistryValidatesRequiredArguments(t *testing.T) {
	registry, err := NewRegistry([]Definition{{
		Name:       "required_tool",
		Parameters: Object(map[string]Schema{"query": String("query")}, "query"),
		Execute: func(context.Context, json.RawMessage) (any, error) {
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Execute(context.Background(), "required_tool", json.RawMessage(`{}`))
	if err == nil || !strings.Contains(err.Error(), `missing required field "query"`) {
		t.Fatalf("error = %v, want missing required field", err)
	}
}

func TestRegistryRejectsUnknownArguments(t *testing.T) {
	registry, err := NewRegistry([]Definition{{
		Name:       "closed_tool",
		Parameters: Object(map[string]Schema{"query": String("query")}, "query"),
		Execute: func(context.Context, json.RawMessage) (any, error) {
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	_, err = registry.Execute(context.Background(), "closed_tool", json.RawMessage(`{"query":"x","extra":true}`))
	if err == nil || !strings.Contains(err.Error(), `unknown field "extra"`) {
		t.Fatalf("error = %v, want unknown field", err)
	}
}

func TestRegistryFormatsFunctionTools(t *testing.T) {
	registry, err := NewRegistry([]Definition{{
		Name:        "search_text",
		Description: "search sandbox text files.",
		Parameters: Object(map[string]Schema{
			"query": String("query"),
			"path":  String("path"),
		}, "query"),
		Execute: func(context.Context, json.RawMessage) (any, error) {
			return nil, nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	tools := registry.FunctionTools()
	if len(tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(tools))
	}
	if tools[0].Type != "function" {
		t.Fatalf("type = %q, want function", tools[0].Type)
	}
	if tools[0].Function.Name != "search_text" {
		t.Fatalf("name = %q, want search_text", tools[0].Function.Name)
	}
	if tools[0].Function.Parameters.Required[0] != "query" {
		t.Fatalf("required = %#v, want query", tools[0].Function.Parameters.Required)
	}
}
