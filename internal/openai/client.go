package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"agentlab/internal/agenttool"
	"agentlab/internal/message"
	"agentlab/internal/provider"
)

const defaultBaseURL = "https://api.openai.com/v1"

type ClientOptions struct {
	BaseURL string
	APIKey  string
}

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

var _ provider.Client = Client{}

func NewClient(options ClientOptions) Client {
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	apiKey := strings.TrimSpace(options.APIKey)
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}
	return Client{
		baseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: http.DefaultClient,
	}
}

func (c Client) Chat(
	ctx context.Context,
	model string,
	messages []message.Message,
	options provider.ChatOptions,
) (message.Message, error) {
	reqBody := responseRequest{
		Model: model,
		Input: toResponseInput(messages),
		Store: boolPtr(false),
		Tools: toResponseTools(options.Tools),
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return message.Message{}, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/responses", bytes.NewReader(data))
	if err != nil {
		return message.Message{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return message.Message{}, fmt.Errorf("call openai: %w", err)
	}
	defer resp.Body.Close()

	var out responseBody
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return message.Message{}, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if out.Error.Message != "" {
			return message.Message{}, fmt.Errorf("openai returned %s: %s", resp.Status, out.Error.Message)
		}
		return message.Message{}, fmt.Errorf("openai returned %s", resp.Status)
	}

	timestamp := time.Now().UTC()
	if out.CreatedAt > 0 {
		timestamp = time.Unix(out.CreatedAt, 0).UTC()
	}
	stopReason := out.Status
	if stopReason == "completed" || stopReason == "" {
		stopReason = "stop"
	}

	content := make([]message.ContentBlock, 0)
	toolCalls := make([]message.ToolCall, 0)
	for _, item := range out.Output {
		switch item.Type {
		case "message":
			for _, part := range item.Content {
				if part.Type == "output_text" && strings.TrimSpace(part.Text) != "" {
					content = append(content, message.NewTextBlock(strings.TrimSpace(part.Text)))
				}
			}
		case "function_call":
			arguments := json.RawMessage(strings.TrimSpace(item.Arguments))
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			toolCalls = append(toolCalls, message.ToolCall{
				ID:   item.CallID,
				Type: "function",
				Function: message.FunctionCall{
					Index:     len(toolCalls),
					Name:      item.Name,
					Arguments: arguments,
				},
			})
		}
	}

	return message.Message{
		Role:       message.RoleAssistant,
		Content:    content,
		Timestamp:  timestamp,
		Provider:   "openai",
		Model:      out.Model,
		StopReason: stopReason,
		ToolCalls:  toolCalls,
		Usage: message.TokenUsage{
			InputTokens:  out.Usage.InputTokens,
			OutputTokens: out.Usage.OutputTokens,
		},
	}, nil
}

func toResponseInput(messages []message.Message) []responseInputItem {
	out := make([]responseInputItem, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case message.RoleSystem, message.RoleUser, message.RoleAssistant:
			if text := msg.Text(); text != "" {
				out = append(out, responseInputItem{
					Type:    "message",
					Role:    string(msg.Role),
					Content: text,
				})
			}
			for _, call := range msg.ToolCalls {
				out = append(out, responseInputItem{
					Type:      "function_call",
					CallID:    call.ID,
					Name:      call.Function.Name,
					Arguments: string(call.Function.Arguments),
					Status:    "completed",
				})
			}
		case message.RoleTool:
			callID := msg.ToolCallID
			if callID == "" {
				callID = msg.ToolName
			}
			out = append(out, responseInputItem{
				Type:   "function_call_output",
				CallID: callID,
				Output: msg.Text(),
			})
		}
	}
	return out
}

func toResponseTools(tools []agenttool.FunctionTool) []responseTool {
	out := make([]responseTool, 0, len(tools))
	for _, tool := range tools {
		if tool.Type != "function" {
			continue
		}
		out = append(out, responseTool{
			Type:        "function",
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
			Strict:      boolPtr(false),
		})
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}

type responseRequest struct {
	Model string              `json:"model"`
	Input []responseInputItem `json:"input"`
	Store *bool               `json:"store,omitempty"`
	Tools []responseTool      `json:"tools,omitempty"`
}

type responseInputItem struct {
	Type      string `json:"type"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Status    string `json:"status,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responseTool struct {
	Type        string           `json:"type"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Parameters  agenttool.Schema `json:"parameters"`
	Strict      *bool            `json:"strict,omitempty"`
}

type responseBody struct {
	Model     string               `json:"model"`
	Status    string               `json:"status"`
	CreatedAt int64                `json:"created_at"`
	Output    []responseOutputItem `json:"output"`
	Usage     responseUsage        `json:"usage"`
	Error     responseError        `json:"error"`
}

type responseOutputItem struct {
	Type      string                  `json:"type"`
	Role      string                  `json:"role"`
	Content   []responseOutputContent `json:"content"`
	CallID    string                  `json:"call_id"`
	Name      string                  `json:"name"`
	Arguments string                  `json:"arguments"`
	Status    string                  `json:"status"`
}

type responseOutputContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type responseError struct {
	Message string `json:"message"`
}
