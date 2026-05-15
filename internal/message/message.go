package message

import (
	"strings"
	"time"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeThinking ContentType = "thinking"
)

type ContentBlock struct {
	Type ContentType `json:"type"`
	Text string      `json:"text,omitempty"`
}

type Message struct {
	Role       Role           `json:"role"`
	Content    []ContentBlock `json:"content"`
	Timestamp  time.Time      `json:"timestamp"`
	Provider   string         `json:"provider,omitempty"`
	Model      string         `json:"model,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Usage      TokenUsage     `json:"usage,omitempty"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

func NewUserText(text string) Message {
	return Message{
		Role:      RoleUser,
		Content:   []ContentBlock{NewTextBlock(text)},
		Timestamp: time.Now().UTC(),
	}
}

func NewSystemText(text string) Message {
	return Message{
		Role:      RoleSystem,
		Content:   []ContentBlock{NewTextBlock(text)},
		Timestamp: time.Now().UTC(),
	}
}

func NewAssistantText(text, provider, model string) Message {
	return Message{
		Role:       RoleAssistant,
		Content:    []ContentBlock{NewTextBlock(text)},
		Timestamp:  time.Now().UTC(),
		Provider:   provider,
		Model:      model,
		StopReason: "stop",
	}
}

func NewTextBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeText, Text: text}
}

func NewThinkingBlock(text string) ContentBlock {
	return ContentBlock{Type: ContentTypeThinking, Text: text}
}

func (m Message) Text() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type == ContentTypeText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func (m Message) Thinking() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type == ContentTypeThinking && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
