package provider

import (
	"context"

	"agentlab/internal/agenttool"
	"agentlab/internal/message"
)

type ChatOptions struct {
	ContextWindow int
	Tools         []agenttool.FunctionTool
}

type Client interface {
	Chat(ctx context.Context, model string, messages []message.Message, options ChatOptions) (message.Message, error)
}
