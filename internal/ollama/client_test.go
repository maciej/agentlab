package ollama

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
			"message": {"role": "assistant", "content": " hello from ollama "},
			"done_reason": "stop"
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	response, err := client.Chat(context.Background(), "gemma", []message.Message{
		message.NewUserText("hello"),
	}, 32768)
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
	if got.Think == nil || *got.Think {
		t.Fatal("think should be false")
	}
	if got.Options.NumCtx != 32768 {
		t.Fatalf("num_ctx = %d, want 32768", got.Options.NumCtx)
	}
	if response.Role != message.RoleAssistant {
		t.Fatalf("response role = %q, want %q", response.Role, message.RoleAssistant)
	}
	if response.Text() != "hello from ollama" {
		t.Fatalf("response text = %q, want %q", response.Text(), "hello from ollama")
	}
	if response.Provider != "ollama" {
		t.Fatalf("response provider = %q, want %q", response.Provider, "ollama")
	}
	if response.Model != "gemma" {
		t.Fatalf("response model = %q, want %q", response.Model, "gemma")
	}
}
