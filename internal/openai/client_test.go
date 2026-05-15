package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"agentlab/internal/agenttool"
	"agentlab/internal/message"
	"agentlab/internal/provider"
)

func TestChatCallsOpenAIResponsesEndpoint(t *testing.T) {
	var got responseRequest
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("path = %q, want %q", r.URL.Path, "/responses")
		}
		auth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"created_at": 1778880000,
			"model": "gpt-5.4",
			"status": "completed",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"content": [
						{"type": "output_text", "text": " hello from openai "}
					]
				}
			],
			"usage": {"input_tokens": 13, "output_tokens": 5}
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})
	response, err := client.Chat(context.Background(), "gpt-5.4", []message.Message{
		message.NewUserText("hello"),
	}, provider.ChatOptions{
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

	if auth != "Bearer test-key" {
		t.Fatalf("Authorization = %q, want bearer token", auth)
	}
	if got.Model != "gpt-5.4" {
		t.Fatalf("model = %q, want gpt-5.4", got.Model)
	}
	if len(got.Input) != 1 || got.Input[0].Role != "user" || got.Input[0].Content != "hello" {
		t.Fatalf("input = %#v", got.Input)
	}
	if got.Store == nil || *got.Store {
		t.Fatalf("store = %#v, want false", got.Store)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tool count = %d, want 1", len(got.Tools))
	}
	if got.Tools[0].Name != "search_text" || got.Tools[0].Type != "function" {
		t.Fatalf("tool = %#v", got.Tools[0])
	}
	if got.Tools[0].Strict == nil || *got.Tools[0].Strict {
		t.Fatalf("strict = %#v, want false", got.Tools[0].Strict)
	}
	if response.Provider != "openai" {
		t.Fatalf("provider = %q, want openai", response.Provider)
	}
	if response.Model != "gpt-5.4" {
		t.Fatalf("response model = %q, want gpt-5.4", response.Model)
	}
	if response.Text() != "hello from openai" {
		t.Fatalf("response text = %q, want hello from openai", response.Text())
	}
	if response.Usage.InputTokens != 13 || response.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestChatPreservesFunctionCallsAndOutputs(t *testing.T) {
	var got responseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model": "gpt-5.4",
			"status": "completed",
			"output": [
				{
					"type": "function_call",
					"call_id": "call_123",
					"name": "search_text",
					"arguments": "{\"query\":\"aurora\"}",
					"status": "completed"
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})
	response, err := client.Chat(context.Background(), "gpt-5.4", []message.Message{
		{
			Role: message.RoleAssistant,
			ToolCalls: []message.ToolCall{{
				ID:   "call_prev",
				Type: "function",
				Function: message.FunctionCall{
					Name:      "read_file",
					Arguments: json.RawMessage(`{"path":"notes/tasks.md"}`),
				},
			}},
		},
		message.NewToolResult("read_file", `{"content":"ok"}`, "call_prev"),
	}, provider.ChatOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Input) != 2 {
		t.Fatalf("input count = %d, want 2", len(got.Input))
	}
	if got.Input[0].Type != "function_call" || got.Input[0].CallID != "call_prev" {
		t.Fatalf("function call input = %#v", got.Input[0])
	}
	if got.Input[1].Type != "function_call_output" || got.Input[1].CallID != "call_prev" {
		t.Fatalf("function output input = %#v", got.Input[1])
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool call count = %d, want 1", len(response.ToolCalls))
	}
	if response.ToolCalls[0].ID != "call_123" {
		t.Fatalf("tool call id = %q, want call_123", response.ToolCalls[0].ID)
	}
	if response.ToolCalls[0].Function.Name != "search_text" {
		t.Fatalf("tool name = %q, want search_text", response.ToolCalls[0].Function.Name)
	}
	if string(response.ToolCalls[0].Function.Arguments) != `{"query":"aurora"}` {
		t.Fatalf("arguments = %s", response.ToolCalls[0].Function.Arguments)
	}
}

func TestChatReturnsOpenAIErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{BaseURL: server.URL, APIKey: "test-key"})
	_, err := client.Chat(context.Background(), "gpt-5.4", []message.Message{
		message.NewUserText("hello"),
	}, provider.ChatOptions{})
	if err == nil {
		t.Fatal("Chat() error is nil, want error")
	}
}
