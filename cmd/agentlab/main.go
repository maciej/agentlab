package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"agentlab/internal/config"
	"agentlab/internal/message"
	"agentlab/internal/ollama"
	"agentlab/internal/session"

	"github.com/spf13/cobra"
)

const helloPrompt = "Say hello from AgentLab in one short sentence."

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "agentlab:", err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "agentlab",
		Short: "Run the AgentLab hello-world agent harness",
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cfgPath)
		},
	}
	cmd.Flags().StringVar(&cfgPath, "config", "", "path to YAML config file")

	return cmd
}

func run(cfgPath string) error {
	cfg, err := config.Load(cfgPath)
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
	if _, err := s.AppendMessage(message.NewUserText(helloPrompt)); err != nil {
		return err
	}
	sessionContext, err := s.BuildContext()
	if err != nil {
		return err
	}

	client := ollama.NewClient(cfg.Ollama.Endpoint)
	response, err := client.Chat(ctx, cfg.Ollama.Model, sessionContext.Messages, cfg.Ollama.ContextWindow)
	if err != nil {
		return err
	}
	if _, err := s.AppendMessage(response); err != nil {
		return err
	}

	fmt.Printf("Provider: %s\n", cfg.Provider)
	fmt.Printf("Model: %s\n", cfg.Ollama.Model)
	if cfg.Ollama.ContextWindow > 0 {
		fmt.Printf("Context window: %d\n", cfg.Ollama.ContextWindow)
	}
	fmt.Printf("Session: %s\n", s.Metadata().ID)
	fmt.Printf("Session entries: %d\n", len(s.Entries()))
	fmt.Printf("Prompt: %s\n\n", helloPrompt)
	fmt.Println(response.Text())

	return nil
}
