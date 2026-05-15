package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agentlab/internal/agenttool"
	"agentlab/internal/message"
	"agentlab/internal/provider"
)

type ThinkMode string

const (
	ThinkDisabled ThinkMode = ""
	ThinkFalse    ThinkMode = "false"
	ThinkTrue     ThinkMode = "true"
	ThinkLow      ThinkMode = "low"
	ThinkMedium   ThinkMode = "medium"
	ThinkHigh     ThinkMode = "high"
)

type ClientOptions struct {
	Think ThinkMode
}

type Client struct {
	endpoint   string
	httpClient *http.Client
	options    ClientOptions
}

var _ provider.Client = Client{}

func NewClient(endpoint string) Client {
	return NewClientWithOptions(endpoint, ClientOptions{})
}

func NewClientWithOptions(endpoint string, options ClientOptions) Client {
	return Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: http.DefaultClient,
		options:    options,
	}
}

func (c Client) Generate(ctx context.Context, model, prompt string, contextWindow int) (string, error) {
	response, err := c.Chat(ctx, model, []message.Message{message.NewUserText(prompt)}, provider.ChatOptions{
		ContextWindow: contextWindow,
	})
	if err != nil {
		return "", err
	}
	return response.Text(), nil
}

func (c Client) Chat(
	ctx context.Context,
	model string,
	messages []message.Message,
	options provider.ChatOptions,
) (message.Message, error) {
	reqBody := chatRequest{
		Model:    model,
		Messages: toChatMessages(messages),
		Stream:   false,
		Think:    c.options.Think,
		Tools:    options.Tools,
	}
	if reqBody.Think == ThinkDisabled {
		reqBody.Think = ThinkFalse
	}
	if options.ContextWindow > 0 {
		reqBody.Options.NumCtx = options.ContextWindow
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return message.Message{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return message.Message{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return message.Message{}, fmt.Errorf("call ollama: %w", err)
	}
	defer resp.Body.Close()

	var out chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return message.Message{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error != "" {
			return message.Message{}, fmt.Errorf("ollama returned %s: %s", resp.Status, out.Error)
		}
		return message.Message{}, fmt.Errorf("ollama returned %s", resp.Status)
	}

	stopReason := out.DoneReason
	if stopReason == "" {
		stopReason = "stop"
	}
	timestamp := out.CreatedAt
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	content := make([]message.ContentBlock, 0, 2)
	if text := strings.TrimSpace(out.Message.Content); text != "" {
		content = append(content, message.NewTextBlock(text))
	}
	if thinking := strings.TrimSpace(out.Message.Thinking); thinking != "" {
		content = append(content, message.NewThinkingBlock(thinking))
	}

	return message.Message{
		Role:       message.RoleAssistant,
		Content:    content,
		Timestamp:  timestamp,
		Provider:   "ollama",
		Model:      model,
		StopReason: stopReason,
		ToolCalls:  out.Message.ToolCalls,
		Usage: message.TokenUsage{
			InputTokens:  out.PromptEvalCount,
			OutputTokens: out.EvalCount,
		},
	}, nil
}

func toChatMessages(messages []message.Message) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, msg := range messages {
		role := string(msg.Role)
		text := msg.Text()
		if role == "" || (text == "" && len(msg.ToolCalls) == 0) {
			continue
		}
		out = append(out, chatMessage{
			Role:      role,
			Content:   text,
			Thinking:  msg.Thinking(),
			ToolName:  msg.ToolName,
			ToolCalls: msg.ToolCalls,
		})
	}
	return out
}

func (m ThinkMode) MarshalJSON() ([]byte, error) {
	switch m {
	case ThinkDisabled, ThinkFalse:
		return []byte("false"), nil
	case ThinkTrue:
		return []byte("true"), nil
	case ThinkLow, ThinkMedium, ThinkHigh:
		return json.Marshal(string(m))
	default:
		return nil, fmt.Errorf("unsupported ollama think mode %q", m)
	}
}

func (m *ThinkMode) UnmarshalJSON(data []byte) error {
	var boolValue bool
	if err := json.Unmarshal(data, &boolValue); err == nil {
		if boolValue {
			*m = ThinkTrue
		} else {
			*m = ThinkFalse
		}
		return nil
	}

	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err != nil {
		return err
	}
	*m = ThinkMode(stringValue)
	return nil
}

type chatRequest struct {
	Model    string                   `json:"model"`
	Messages []chatMessage            `json:"messages"`
	Stream   bool                     `json:"stream"`
	Think    ThinkMode                `json:"think"`
	Options  requestOptions           `json:"options,omitempty"`
	Tools    []agenttool.FunctionTool `json:"tools,omitempty"`
}

type chatMessage struct {
	Role      string             `json:"role"`
	Content   string             `json:"content,omitempty"`
	Thinking  string             `json:"thinking,omitempty"`
	ToolName  string             `json:"tool_name,omitempty"`
	ToolCalls []message.ToolCall `json:"tool_calls,omitempty"`
}

type chatResponse struct {
	Message         chatMessage `json:"message"`
	CreatedAt       time.Time   `json:"created_at"`
	DoneReason      string      `json:"done_reason"`
	PromptEvalCount int         `json:"prompt_eval_count"`
	EvalCount       int         `json:"eval_count"`
	Error           string      `json:"error"`
}

type requestOptions struct {
	NumCtx int `json:"num_ctx,omitempty"`
}
