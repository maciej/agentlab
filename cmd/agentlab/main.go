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
	"agentlab/internal/provider"
	"agentlab/internal/providerfactory"
	"agentlab/internal/sandboxfs"
	"agentlab/internal/session"

	"github.com/spf13/cobra"
)

const (
	helloPrompt        = "Say hello from AgentLab in one short sentence."
	defaultSandboxPath = "testdata/smoke-sandbox"
	baseSystemPrompt   = "You are AgentLab, a concise agent harness. Answer the user directly. When tools are available, use them to inspect the sandbox before making claims about sandbox contents."
)

type runOptions struct {
	ConfigPath   string
	ProviderName string
	Prompt       string
	SandboxPath  string
	MaxToolTurns int
}

type renderSystemPromptOptions struct {
	SandboxPath string
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
		Use:   "agentlab [prompt]",
		Short: "Run the AgentLab agent harness",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.Prompt = promptFromArgs(options.Prompt, args)
			return run(options)
		},
	}
	cmd.Flags().StringVar(&options.ConfigPath, "config", "", "path to YAML config file")
	cmd.Flags().StringVar(&options.ProviderName, "provider", "", "provider name from config to use")
	cmd.Flags().StringVar(&options.Prompt, "prompt", "", "prompt to send to the model")
	cmd.Flags().
		StringVar(&options.SandboxPath, "sandbox", defaultSandboxPath, "directory to snapshot for read-only tools")
	cmd.Flags().IntVar(&options.MaxToolTurns, "max-tool-turns", 3, "maximum read-only tool calls before stopping")

	cmd.AddCommand(newDebugCommand())

	return cmd
}

func newDebugCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug AgentLab internals",
	}
	cmd.AddCommand(newRenderSystemPromptCommand())
	return cmd
}

func newRenderSystemPromptCommand() *cobra.Command {
	options := renderSystemPromptOptions{
		SandboxPath: defaultSandboxPath,
	}
	cmd := &cobra.Command{
		Use:   "render-system-prompt",
		Short: "Render the system prompt sent to the model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return renderSystemPromptCommand(options)
		},
	}
	cmd.Flags().
		StringVar(&options.SandboxPath, "sandbox", defaultSandboxPath, "directory whose tools are included in the prompt")
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
	selectedProvider, err := cfg.ProviderByName(options.ProviderName)
	if err != nil {
		return err
	}
	model := selectedProvider.Model()
	if _, err := s.AppendModelChange(selectedProvider.Name, model); err != nil {
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

	systemPrompt, err := renderSystemPrompt(toolRegistry)
	if err != nil {
		return err
	}
	if _, err := s.AppendMessage(message.NewSystemText(systemPrompt)); err != nil {
		return err
	}
	if _, err := s.AppendMessage(message.NewUserText(options.Prompt)); err != nil {
		return err
	}

	client, err := providerfactory.NewClient(selectedProvider)
	if err != nil {
		return err
	}
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
		response, err = client.Chat(ctx, model, sessionContext.Messages, provider.ChatOptions{
			ContextWindow: selectedProvider.ContextWindow(),
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
			if _, err := s.AppendMessage(
				message.NewToolResult(toolName, formatToolResultContent(result), call.ID),
			); err != nil {
				return err
			}
		}
	}

	fmt.Printf("Provider: %s\n", selectedProvider.Name)
	fmt.Printf("Provider type: %s\n", selectedProvider.Type)
	fmt.Printf("Model: %s\n", model)
	if selectedProvider.ContextWindow() > 0 {
		fmt.Printf("Context window: %d\n", selectedProvider.ContextWindow())
	}
	if thinking := selectedProvider.Thinking(); thinking != "" && thinking != "false" {
		fmt.Printf("Thinking: %s\n", thinking)
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

func renderSystemPromptCommand(options renderSystemPromptOptions) error {
	toolRegistry, err := registryForPromptRender(options.SandboxPath)
	if err != nil {
		return err
	}
	prompt, err := renderSystemPrompt(toolRegistry)
	if err != nil {
		return err
	}
	fmt.Println(prompt)
	return nil
}

func promptFromArgs(flagPrompt string, args []string) string {
	if flagPrompt != "" || len(args) == 0 {
		return flagPrompt
	}
	return args[0]
}

func registryForPromptRender(sandboxPath string) (*agenttool.Registry, error) {
	if sandboxPath == "" {
		return nil, nil
	}
	return agenttool.NewRegistry(agenttool.SandboxDefinitions(nil))
}

func renderSystemPrompt(toolRegistry *agenttool.Registry) (string, error) {
	var builder strings.Builder
	builder.WriteString(baseSystemPrompt)
	builder.WriteString("\n\n")
	builder.WriteString("Tool definitions:\n")
	if toolRegistry == nil {
		builder.WriteString("[]")
		return builder.String(), nil
	}
	data, err := json.MarshalIndent(toolRegistry.FunctionTools(), "", "  ")
	if err != nil {
		return "", fmt.Errorf("render tool definitions: %w", err)
	}
	builder.Write(data)
	return builder.String(), nil
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
