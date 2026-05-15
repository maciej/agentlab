package message

import (
	"strings"
	"time"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type ContentType string

const (
	ContentTypeText ContentType = "text"
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
}

func NewUserText(text string) Message {
	return Message{
		Role:      RoleUser,
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

func (m Message) Text() string {
	parts := make([]string, 0, len(m.Content))
	for _, block := range m.Content {
		if block.Type == ContentTypeText && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}
