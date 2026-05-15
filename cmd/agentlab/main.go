package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agentlab/internal/agenttool"
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

	var toolRegistry *agenttool.Registry
	if options.SandboxPath != "" {
		options.SandboxPath = absPath(options.SandboxPath)
		workspace, snapshotRoot, err := sandboxfs.NewSnapshot(ctx, options.SandboxPath)
		if err != nil {
			return fmt.Errorf("create sandbox snapshot: %w", err)
		}
		defer os.RemoveAll(snapshotRoot)
		toolRegistry, err = agenttool.NewSandboxRegistry(workspace)
		if err != nil {
			return err
		}
	}

	if _, err := s.AppendMessage(message.NewUserText(options.Prompt)); err != nil {
		return err
	}

	client := ollama.NewClient(cfg.Ollama.Endpoint)
	var response message.Message
	toolCalls := make([]message.ToolCall, 0)
	for turn := 0; ; turn++ {
		sessionContext, err := s.BuildContext()
		if err != nil {
			return err
		}
		var tools []agenttool.FunctionTool
		if toolRegistry != nil {
			tools = toolRegistry.FunctionTools()
		}
		response, err = client.Chat(ctx, cfg.Ollama.Model, sessionContext.Messages, ollama.ChatOptions{
			ContextWindow: cfg.Ollama.ContextWindow,
			Think:         ollama.ThinkMode(cfg.Ollama.Think),
			Tools:         tools,
		})
		if err != nil {
			return err
		}
		if _, err := s.AppendMessage(response); err != nil {
			return err
		}

		if len(response.ToolCalls) == 0 {
			break
		}
		if toolRegistry == nil {
			return fmt.Errorf("model requested tool calls but no tools are configured")
		}
		if turn >= options.MaxToolTurns {
			return fmt.Errorf("model exceeded max tool turns (%d)", options.MaxToolTurns)
		}

		for _, call := range response.ToolCalls {
			toolName := call.Function.Name
			if toolName == "" {
				return fmt.Errorf("model requested unnamed tool")
			}
			result, err := toolRegistry.Execute(ctx, toolName, call.Function.Arguments)
			if err != nil {
				return err
			}
			toolCalls = append(toolCalls, call)
			if _, err := s.AppendMessage(message.NewToolResult(toolName, formatToolResultContent(result))); err != nil {
				return err
			}
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
			fmt.Printf("- %s %s\n", call.Function.Name, strings.TrimSpace(string(call.Function.Arguments)))
		}
	}
	fmt.Println()
	if thinking := response.Thinking(); thinking != "" {
		fmt.Printf("Thinking output:\n%s\n\n", thinking)
	}
	fmt.Println(response.Text())

	return nil
}

func formatToolResultContent(result any) string {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		data = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return string(data)
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
