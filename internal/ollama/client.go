package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"agentlab/internal/message"
)

type Client struct {
	endpoint   string
	httpClient *http.Client
}

func NewClient(endpoint string) Client {
	return Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: http.DefaultClient,
	}
}

func (c Client) Generate(ctx context.Context, model, prompt string, contextWindow int) (string, error) {
	response, err := c.Chat(ctx, model, []message.Message{message.NewUserText(prompt)}, contextWindow)
	if err != nil {
		return "", err
	}
	return response.Text(), nil
}

func (c Client) Chat(
	ctx context.Context,
	model string,
	messages []message.Message,
	contextWindow int,
) (message.Message, error) {
	reqBody := chatRequest{
		Model:    model,
		Messages: toChatMessages(messages),
		Stream:   false,
		Think:    boolPtr(false),
	}
	if contextWindow > 0 {
		reqBody.Options.NumCtx = contextWindow
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
	return message.Message{
		Role:       message.RoleAssistant,
		Content:    []message.ContentBlock{message.NewTextBlock(strings.TrimSpace(out.Message.Content))},
		Timestamp:  timestamp,
		Provider:   "ollama",
		Model:      model,
		StopReason: stopReason,
	}, nil
}

func toChatMessages(messages []message.Message) []chatMessage {
	out := make([]chatMessage, 0, len(messages))
	for _, msg := range messages {
		role := string(msg.Role)
		if role == "" || msg.Text() == "" {
			continue
		}
		out = append(out, chatMessage{
			Role:    role,
			Content: msg.Text(),
		})
	}
	return out
}

func boolPtr(v bool) *bool {
	return &v
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Think    *bool          `json:"think,omitempty"`
	Options  requestOptions `json:"options,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Message    chatMessage `json:"message"`
	CreatedAt  time.Time   `json:"created_at"`
	DoneReason string      `json:"done_reason"`
	Error      string      `json:"error"`
}

type requestOptions struct {
	NumCtx int `json:"num_ctx,omitempty"`
}
