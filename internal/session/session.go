package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"agentlab/internal/message"
)

type Metadata struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

type EntryType string

const (
	EntryTypeMessage     EntryType = "message"
	EntryTypeModelChange EntryType = "model_change"
)

type Entry struct {
	Type      EntryType        `json:"type"`
	ID        string           `json:"id"`
	ParentID  string           `json:"parent_id,omitempty"`
	Timestamp time.Time        `json:"timestamp"`
	Message   *message.Message `json:"message,omitempty"`
	Provider  string           `json:"provider,omitempty"`
	Model     string           `json:"model,omitempty"`
}

type ModelSelection struct {
	Provider string
	Model    string
}

type Context struct {
	Messages []message.Message
	Model    *ModelSelection
}

type Storage interface {
	Metadata() Metadata
	LeafID() string
	SetLeafID(id string) error
	CreateEntryID() (string, error)
	AppendEntry(entry Entry) error
	Entry(id string) (Entry, bool)
	Entries() []Entry
	PathToRoot(leafID string) ([]Entry, error)
}

type Session struct {
	storage Storage
}

func New(storage Storage) *Session {
	return &Session{storage: storage}
}

func NewInMemory() (*Session, error) {
	storage, err := NewInMemoryStorage(nil)
	if err != nil {
		return nil, err
	}
	return New(storage), nil
}

func (s *Session) Metadata() Metadata {
	return s.storage.Metadata()
}

func (s *Session) LeafID() string {
	return s.storage.LeafID()
}

func (s *Session) Entries() []Entry {
	return s.storage.Entries()
}

func (s *Session) Branch(id string) error {
	return s.storage.SetLeafID(id)
}

func (s *Session) GetEntry(id string) (Entry, bool) {
	return s.storage.Entry(id)
}

func (s *Session) BranchEntries() ([]Entry, error) {
	return s.storage.PathToRoot(s.storage.LeafID())
}

func (s *Session) AppendMessage(msg message.Message) (string, error) {
	return s.appendEntry(Entry{
		Type:    EntryTypeMessage,
		Message: &msg,
	})
}

func (s *Session) AppendModelChange(provider, model string) (string, error) {
	return s.appendEntry(Entry{
		Type:     EntryTypeModelChange,
		Provider: provider,
		Model:    model,
	})
}

func (s *Session) BuildContext() (Context, error) {
	entries, err := s.BranchEntries()
	if err != nil {
		return Context{}, err
	}

	ctx := Context{
		Messages: make([]message.Message, 0, len(entries)),
	}
	for _, entry := range entries {
		switch entry.Type {
		case EntryTypeMessage:
			if entry.Message != nil {
				ctx.Messages = append(ctx.Messages, *entry.Message)
				if entry.Message.Role == message.RoleAssistant && entry.Message.Provider != "" &&
					entry.Message.Model != "" {
					ctx.Model = &ModelSelection{
						Provider: entry.Message.Provider,
						Model:    entry.Message.Model,
					}
				}
			}
		case EntryTypeModelChange:
			ctx.Model = &ModelSelection{
				Provider: entry.Provider,
				Model:    entry.Model,
			}
		}
	}

	return ctx, nil
}

func (s *Session) appendEntry(entry Entry) (string, error) {
	id, err := s.storage.CreateEntryID()
	if err != nil {
		return "", err
	}

	entry.ID = id
	entry.ParentID = s.storage.LeafID()
	entry.Timestamp = time.Now().UTC()

	if err := s.storage.AppendEntry(entry); err != nil {
		return "", err
	}
	return id, nil
}

func newID(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("create id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
