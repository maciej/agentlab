package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentlab/internal/config"
	"agentlab/internal/message"
	"agentlab/internal/ollama"
	"agentlab/internal/sandboxfs"
	"agentlab/internal/session"

	"github.com/spf13/cobra"
)

const helloPrompt = "Say hello from AgentLab in one short sentence."
const defaultSandboxPath = "testdata/smoke-sandbox"

type runOptions struct {
	ConfigPath   string
	Prompt       string
	SandboxPath  string
	MaxToolTurns int
}

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "agentlab:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var options runOptions

	cmd := &cobra.Command{
		Use:   "agentlab",
		Short: "Run the AgentLab agent harness",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options.Prompt == "" && len(args) > 0 {
				options.Prompt = strings.Join(args, " ")
			}
			return run(options)
		},
	}
	cmd.Flags().StringVar(&options.ConfigPath, "config", "", "path to YAML config file")
	cmd.Flags().StringVar(&options.Prompt, "prompt", "", "prompt to send to the model")
	cmd.Flags().StringVar(&options.SandboxPath, "sandbox", defaultSandboxPath, "directory to snapshot for read-only tools")
	cmd.Flags().IntVar(&options.MaxToolTurns, "max-tool-turns", 3, "maximum read-only tool calls before stopping")

	return cmd
}

func run(options runOptions) error {
	if options.Prompt == "" {
		options.Prompt = helloPrompt
	}
	if options.MaxToolTurns <= 0 {
		options.MaxToolTurns = 1
	}

	cfg, err := config.Load(options.ConfigPath)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	s, err := session.NewInMemory()
	if err != nil {
		return err
	}
	if _, err := s.AppendModelChange(cfg.Provider, cfg.Ollama.Model); err != nil {
		return err
	}

	var workspace *sandboxfs.Workspace
	var snapshotRoot string
	if options.SandboxPath != "" {
		options.SandboxPath = absPath(options.SandboxPath)
		workspace, snapshotRoot, err = sandboxfs.NewSnapshot(ctx, options.SandboxPath)
		if err != nil {
			return fmt.Errorf("create sandbox snapshot: %w", err)
		}
		defer os.RemoveAll(snapshotRoot)
		if _, err := s.AppendMessage(message.NewSystemText(toolInstructions())); err != nil {
			return err
		}
	}

	if _, err := s.AppendMessage(message.NewUserText(options.Prompt)); err != nil {
		return err
	}

	client := ollama.NewClient(cfg.Ollama.Endpoint)
	var response message.Message
	toolCalls := make([]toolCall, 0)
	for turn := 0; ; turn++ {
		sessionContext, err := s.BuildContext()
		if err != nil {
			return err
		}
		response, err = client.Chat(ctx, cfg.Ollama.Model, sessionContext.Messages, ollama.ChatOptions{
			ContextWindow: cfg.Ollama.ContextWindow,
			Think:         ollama.ThinkMode(cfg.Ollama.Think),
		})
		if err != nil {
			return err
		}
		if _, err := s.AppendMessage(response); err != nil {
			return err
		}

		call, ok, err := parseToolCall(response.Text())
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		if workspace == nil {
			return fmt.Errorf("model requested tool %q but no sandbox is configured", call.Tool)
		}
		if turn >= options.MaxToolTurns {
			return fmt.Errorf("model exceeded max tool turns (%d)", options.MaxToolTurns)
		}

		result, err := executeTool(ctx, workspace, call)
		if err != nil {
			return err
		}
		toolCalls = append(toolCalls, call)
		if _, err := s.AppendMessage(message.NewUserText(formatToolResult(call, result))); err != nil {
			return err
		}
	}

	fmt.Printf("Provider: %s\n", cfg.Provider)
	fmt.Printf("Model: %s\n", cfg.Ollama.Model)
	if cfg.Ollama.ContextWindow > 0 {
		fmt.Printf("Context window: %d\n", cfg.Ollama.ContextWindow)
	}
	if cfg.Ollama.Think != "" && cfg.Ollama.Think != "false" {
		fmt.Printf("Thinking: %s\n", cfg.Ollama.Think)
	}
	fmt.Printf("Session: %s\n", s.Metadata().ID)
	fmt.Printf("Session entries: %d\n", len(s.Entries()))
	fmt.Printf("Prompt: %s\n", options.Prompt)
	if options.SandboxPath != "" {
		fmt.Printf("Sandbox: %s\n", options.SandboxPath)
	}
	if len(toolCalls) > 0 {
		fmt.Printf("Tool calls: %d\n", len(toolCalls))
		for _, call := range toolCalls {
			fmt.Printf("- %s %s\n", call.Tool, strings.TrimSpace(string(call.Arguments)))
		}
	}
	fmt.Println()
	if thinking := response.Thinking(); thinking != "" {
		fmt.Printf("Thinking output:\n%s\n\n", thinking)
	}
	fmt.Println(response.Text())

	return nil
}

type toolCall struct {
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
}

func toolInstructions() string {
	return `You have read-only tools over a sandbox snapshot. The tools cannot access the host filesystem.

When you need a tool, respond with exactly one JSON object and no prose:
{"tool":"list_files","arguments":{"path":".","recursive":true}}
{"tool":"read_file","arguments":{"path":"README.md","start_line":1,"line_count":80}}
{"tool":"search_text","arguments":{"query":"needle","path":".","case_sensitive":false,"regex":false}}

Available tools:
- list_files: list files under a sandbox-relative path. Arguments: path, recursive, max_entries.
- read_file: read a sandbox-relative text file. Arguments: path, start_line, line_count, max_bytes.
- search_text: search sandbox text files. Arguments: query, path, case_sensitive, regex, max_matches.

Paths must be relative to the sandbox root. Use "." for the sandbox root.
After receiving a tool result, answer the user's original request normally unless another tool is needed.`
}

func parseToolCall(text string) (toolCall, bool, error) {
	candidate := strings.TrimSpace(text)
	candidate = strings.TrimPrefix(candidate, "```json")
	candidate = strings.TrimPrefix(candidate, "```")
	candidate = strings.TrimSuffix(candidate, "```")
	candidate = strings.TrimSpace(candidate)
	if !strings.HasPrefix(candidate, "{") {
		start := strings.Index(candidate, "{")
		end := strings.LastIndex(candidate, "}")
		if start < 0 || end <= start {
			return toolCall{}, false, nil
		}
		candidate = candidate[start : end+1]
	}

	var call toolCall
	if err := json.Unmarshal([]byte(candidate), &call); err != nil {
		return toolCall{}, false, fmt.Errorf("parse tool call: %w", err)
	}
	if call.Tool == "" {
		return toolCall{}, false, nil
	}
	if len(call.Arguments) == 0 {
		call.Arguments = json.RawMessage(`{}`)
	}
	return call, true, nil
}

func executeTool(ctx context.Context, workspace *sandboxfs.Workspace, call toolCall) (any, error) {
	switch call.Tool {
	case "list_files":
		var args struct {
			Path       string `json:"path"`
			Recursive  bool   `json:"recursive"`
			MaxEntries int    `json:"max_entries"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse list_files arguments: %w", err)
		}
		return workspace.List(ctx, sandboxfs.ListRequest{
			Path:       args.Path,
			Recursive:  args.Recursive,
			MaxEntries: args.MaxEntries,
		})
	case "read_file":
		var args struct {
			Path      string `json:"path"`
			StartLine int    `json:"start_line"`
			LineCount int    `json:"line_count"`
			MaxBytes  int64  `json:"max_bytes"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse read_file arguments: %w", err)
		}
		return workspace.Read(ctx, sandboxfs.ReadRequest{
			Path:      args.Path,
			StartLine: args.StartLine,
			LineCount: args.LineCount,
			MaxBytes:  args.MaxBytes,
		})
	case "search_text":
		var args struct {
			Path          string `json:"path"`
			Query         string `json:"query"`
			CaseSensitive bool   `json:"case_sensitive"`
			Regex         bool   `json:"regex"`
			MaxMatches    int    `json:"max_matches"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return nil, fmt.Errorf("parse search_text arguments: %w", err)
		}
		return workspace.Grep(ctx, sandboxfs.GrepRequest{
			Path:          args.Path,
			Query:         args.Query,
			CaseSensitive: args.CaseSensitive,
			Regex:         args.Regex,
			MaxMatches:    args.MaxMatches,
		})
	default:
		return nil, fmt.Errorf("unknown tool %q", call.Tool)
	}
}

func formatToolResult(call toolCall, result any) string {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		data = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return fmt.Sprintf("Tool result for %s:\n```json\n%s\n```\nUse this result to continue the original task.", call.Tool, string(data))
}

func absPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
