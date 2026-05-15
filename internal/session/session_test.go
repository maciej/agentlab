package session

import (
	"testing"

	"agentlab/internal/message"
)

func TestSessionBuildContextUsesCurrentBranch(t *testing.T) {
	s, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}

	rootID, err := s.AppendMessage(message.NewUserText("root"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(message.NewAssistantText("first branch", "ollama", "gemma")); err != nil {
		t.Fatal(err)
	}

	if err := s.Branch(rootID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(message.NewUserText("second branch")); err != nil {
		t.Fatal(err)
	}

	ctx, err := s.BuildContext()
	if err != nil {
		t.Fatal(err)
	}

	if len(ctx.Messages) != 2 {
		t.Fatalf("message count = %d, want 2", len(ctx.Messages))
	}
	if got := ctx.Messages[0].Text(); got != "root" {
		t.Fatalf("first message = %q, want %q", got, "root")
	}
	if got := ctx.Messages[1].Text(); got != "second branch" {
		t.Fatalf("second message = %q, want %q", got, "second branch")
	}
}

func TestSessionBuildContextTracksLatestModel(t *testing.T) {
	s, err := NewInMemory()
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.AppendModelChange("ollama", "gemma"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendMessage(message.NewAssistantText("hello", "ollama", "qwen")); err != nil {
		t.Fatal(err)
	}

	ctx, err := s.BuildContext()
	if err != nil {
		t.Fatal(err)
	}

	if ctx.Model == nil {
		t.Fatal("model is nil")
	}
	if ctx.Model.Provider != "ollama" {
		t.Fatalf("provider = %q, want %q", ctx.Model.Provider, "ollama")
	}
	if ctx.Model.Model != "qwen" {
		t.Fatalf("model = %q, want %q", ctx.Model.Model, "qwen")
	}
}
