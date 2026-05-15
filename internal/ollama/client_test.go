package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentlab/internal/agenttool"
	"agentlab/internal/message"
)

func TestChatCallsOllamaChatEndpoint(t *testing.T) {
	var got chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/api/chat")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created_at": "2026-05-14T20:30:00Z",
			"message": {
				"role": "assistant",
				"content": " hello from ollama ",
				"thinking": " considered greeting options "
			},
			"done_reason": "stop",
			"prompt_eval_count": 11,
			"eval_count": 7
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	response, err := client.Chat(context.Background(), "gemma", []message.Message{
		message.NewUserText("hello"),
	}, ChatOptions{
		ContextWindow: 32768,
		Think:         ThinkTrue,
		Tools: []agenttool.FunctionTool{{
			Type: "function",
			Function: agenttool.FunctionDefinition{
				Name:        "search_text",
				Description: "search text",
				Parameters: agenttool.Object(map[string]agenttool.Schema{
					"query": agenttool.String("query"),
				}, "query"),
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if got.Model != "gemma" {
		t.Fatalf("model = %q, want %q", got.Model, "gemma")
	}
	if len(got.Messages) != 1 {
		t.Fatalf("message count = %d, want 1", len(got.Messages))
	}
	if got.Messages[0].Role != "user" {
		t.Fatalf("role = %q, want %q", got.Messages[0].Role, "user")
	}
	if got.Messages[0].Content != "hello" {
		t.Fatalf("content = %q, want %q", got.Messages[0].Content, "hello")
	}
	if got.Think != ThinkTrue {
		t.Fatalf("think = %q, want %q", got.Think, ThinkTrue)
	}
	if got.Options.NumCtx != 32768 {
		t.Fatalf("num_ctx = %d, want 32768", got.Options.NumCtx)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(got.Tools))
	}
	if got.Tools[0].Function.Name != "search_text" {
		t.Fatalf("tool name = %q, want search_text", got.Tools[0].Function.Name)
	}
	if response.Role != message.RoleAssistant {
		t.Fatalf("response role = %q, want %q", response.Role, message.RoleAssistant)
	}
	if response.Text() != "hello from ollama" {
		t.Fatalf("response text = %q, want %q", response.Text(), "hello from ollama")
	}
	if response.Thinking() != "considered greeting options" {
		t.Fatalf("response thinking = %q, want %q", response.Thinking(), "considered greeting options")
	}
	if response.Provider != "ollama" {
		t.Fatalf("response provider = %q, want %q", response.Provider, "ollama")
	}
	if response.Model != "gemma" {
		t.Fatalf("response model = %q, want %q", response.Model, "gemma")
	}
	if response.Usage.InputTokens != 11 {
		t.Fatalf("input tokens = %d, want 11", response.Usage.InputTokens)
	}
	if response.Usage.OutputTokens != 7 {
		t.Fatalf("output tokens = %d, want 7", response.Usage.OutputTokens)
	}
}

func TestChatPreservesToolCallsAndToolMessages(t *testing.T) {
	var got chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"message": {
				"role": "assistant",
				"tool_calls": [
					{
						"type": "function",
						"function": {
							"index": 0,
							"name": "search_text",
							"arguments": {"query": "aurora"}
						}
					}
				]
			},
			"done_reason": "stop"
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	response, err := client.Chat(context.Background(), "gemma", []message.Message{
		{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{{
				Type: "function",
				Function: message.FunctionCall{
					Index:     0,
					Name:      "search_text",
					Arguments: json.RawMessage(`{"query":"aurora"}`),
				},
			}},
		},
		message.NewToolResult("search_text", `{"matches":[]}`),
	}, ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(got.Messages))
	}
	if len(got.Messages[0].ToolCalls) != 1 {
		t.Fatalf("request tool calls = %d, want 1", len(got.Messages[0].ToolCalls))
	}
	if got.Messages[1].Role != "tool" || got.Messages[1].ToolName != "search_text" {
		t.Fatalf("tool message = %#v", got.Messages[1])
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("response tool calls = %d, want 1", len(response.ToolCalls))
	}
	if response.ToolCalls[0].Function.Name != "search_text" {
		t.Fatalf("response tool name = %q, want search_text", response.ToolCalls[0].Function.Name)
	}
	if string(response.ToolCalls[0].Function.Arguments) != `{"query": "aurora"}` {
		t.Fatalf("arguments = %s", response.ToolCalls[0].Function.Arguments)
	}
}

func TestChatDefaultsThinkToFalse(t *testing.T) {
	var got chatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"message": {"role": "assistant", "content": "ok"},
			"done_reason": "stop"
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.Chat(context.Background(), "gemma", []message.Message{
		message.NewUserText("hello"),
	}, ChatOptions{}); err != nil {
		t.Fatal(err)
	}

	if got.Think != ThinkFalse {
		t.Fatalf("think = %q, want %q", got.Think, ThinkFalse)
	}
}
