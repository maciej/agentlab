package main

import (
	"context"
	"encoding/json"
	"testing"
	"testing/fstest"

	"agentlab/internal/sandboxfs"
)

func TestParseToolCall(t *testing.T) {
	call, ok, err := parseToolCall("```json\n{\"tool\":\"list_files\",\"arguments\":{\"path\":\".\"}}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if call.Tool != "list_files" {
		t.Fatalf("tool = %q, want list_files", call.Tool)
	}
}

func TestParseToolCallFindsEmbeddedJSON(t *testing.T) {
	call, ok, err := parseToolCall("Sure:\n{\"tool\":\"search_text\",\"arguments\":{\"query\":\"aurora\"}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if call.Tool != "search_text" {
		t.Fatalf("tool = %q, want search_text", call.Tool)
	}
}

func TestExecuteSearchTextTool(t *testing.T) {
	w := sandboxfs.New(fstest.MapFS{
		"README.md": {Data: []byte("codename: aurora\n")},
	})

	result, err := executeTool(context.Background(), w, toolCall{
		Tool:      "search_text",
		Arguments: json.RawMessage(`{"query":"aurora","path":"."}`),
	})
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
