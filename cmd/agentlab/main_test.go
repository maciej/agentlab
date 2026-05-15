package main

import (
	"strings"
	"testing"
)

func TestPromptFromArgsUsesFirstArgumentWhenFlagPromptEmpty(t *testing.T) {
	got := promptFromArgs("", []string{"inspect the sandbox"})
	if got != "inspect the sandbox" {
		t.Fatalf("promptFromArgs() = %q, want positional prompt", got)
	}
}

func TestPromptFromArgsPrefersFlagPrompt(t *testing.T) {
	got := promptFromArgs("flag prompt", []string{"positional prompt"})
	if got != "flag prompt" {
		t.Fatalf("promptFromArgs() = %q, want flag prompt", got)
	}
}

func TestRenderSystemPromptIncludesToolDefinitions(t *testing.T) {
	registry, err := registryForPromptRender(defaultSandboxPath)
	if err != nil {
		t.Fatal(err)
	}

	got, err := renderSystemPrompt(registry)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		baseSystemPrompt,
		"Tool definitions:",
		`"name": "list_files"`,
		`"name": "read_file"`,
		`"name": "search_text"`,
		`"parameters"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("renderSystemPrompt() missing %q:\n%s", want, got)
		}
	}
}

func TestRenderSystemPromptWithoutTools(t *testing.T) {
	got, err := renderSystemPrompt(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, "Tool definitions:\n[]") {
		t.Fatalf("renderSystemPrompt() = %q, want empty tool definitions suffix", got)
	}
}
