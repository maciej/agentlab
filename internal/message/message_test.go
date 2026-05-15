package message

import "testing"

func TestNewUserText(t *testing.T) {
	msg := NewUserText("hello")

	if msg.Role != RoleUser {
		t.Fatalf("role = %q, want %q", msg.Role, RoleUser)
	}
	if got := msg.Text(); got != "hello" {
		t.Fatalf("Text() = %q, want %q", got, "hello")
	}
	if msg.Timestamp.IsZero() {
		t.Fatal("timestamp is zero")
	}
}

func TestNewSystemText(t *testing.T) {
	msg := NewSystemText("tool instructions")

	if msg.Role != RoleSystem {
		t.Fatalf("role = %q, want %q", msg.Role, RoleSystem)
	}
	if got := msg.Text(); got != "tool instructions" {
		t.Fatalf("Text() = %q, want %q", got, "tool instructions")
	}
}

func TestNewAssistantText(t *testing.T) {
	msg := NewAssistantText("hi", "ollama", "gemma")

	if msg.Role != RoleAssistant {
		t.Fatalf("role = %q, want %q", msg.Role, RoleAssistant)
	}
	if msg.Provider != "ollama" {
		t.Fatalf("provider = %q, want %q", msg.Provider, "ollama")
	}
	if msg.Model != "gemma" {
		t.Fatalf("model = %q, want %q", msg.Model, "gemma")
	}
	if msg.StopReason != "stop" {
		t.Fatalf("stop reason = %q, want %q", msg.StopReason, "stop")
	}
}

func TestNewToolResult(t *testing.T) {
	msg := NewToolResult("search_text", `{"matches":[]}`)

	if msg.Role != RoleTool {
		t.Fatalf("role = %q, want %q", msg.Role, RoleTool)
	}
	if msg.ToolName != "search_text" {
		t.Fatalf("tool name = %q, want search_text", msg.ToolName)
	}
	if got := msg.Text(); got != `{"matches":[]}` {
		t.Fatalf("Text() = %q, want tool result JSON", got)
	}
}

func TestThinkingExcludesFinalText(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Content: []ContentBlock{
			NewThinkingBlock("consider options"),
			NewTextBlock("final answer"),
		},
	}

	if got := msg.Text(); got != "final answer" {
		t.Fatalf("Text() = %q, want %q", got, "final answer")
	}
	if got := msg.Thinking(); got != "consider options" {
		t.Fatalf("Thinking() = %q, want %q", got, "consider options")
	}
}
