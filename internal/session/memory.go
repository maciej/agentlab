package session

import (
	"fmt"
	"time"
)

type InMemoryStorage struct {
	metadata Metadata
	entries  []Entry
	byID     map[string]Entry
	leafID   string
}

func NewInMemoryStorage(options *InMemoryStorageOptions) (*InMemoryStorage, error) {
	if options == nil {
		options = &InMemoryStorageOptions{}
	}

	metadata := options.Metadata
	if metadata.ID == "" {
		id, err := newID(16)
		if err != nil {
			return nil, err
		}
		metadata.ID = id
	}
	if metadata.CreatedAt.IsZero() {
		metadata.CreatedAt = time.Now().UTC()
	}

	storage := &InMemoryStorage{
		metadata: metadata,
		entries:  append([]Entry(nil), options.Entries...),
		byID:     make(map[string]Entry, len(options.Entries)),
		leafID:   options.LeafID,
	}
	for _, entry := range storage.entries {
		if entry.ID == "" {
			return nil, fmt.Errorf("entry id is required")
		}
		if _, exists := storage.byID[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate entry id %q", entry.ID)
		}
		storage.byID[entry.ID] = entry
	}
	if storage.leafID == "" && len(storage.entries) > 0 {
		storage.leafID = storage.entries[len(storage.entries)-1].ID
	}
	if storage.leafID != "" {
		if _, ok := storage.byID[storage.leafID]; !ok {
			return nil, fmt.Errorf("entry %q not found", storage.leafID)
		}
	}

	return storage, nil
}

type InMemoryStorageOptions struct {
	Metadata Metadata
	Entries  []Entry
	LeafID   string
}

func (s *InMemoryStorage) Metadata() Metadata {
	return s.metadata
}

func (s *InMemoryStorage) LeafID() string {
	return s.leafID
}

func (s *InMemoryStorage) SetLeafID(id string) error {
	if id != "" {
		if _, ok := s.byID[id]; !ok {
			return fmt.Errorf("entry %q not found", id)
		}
	}
	s.leafID = id
	return nil
}

func (s *InMemoryStorage) CreateEntryID() (string, error) {
	for range 100 {
		id, err := newID(4)
		if err != nil {
			return "", err
		}
		if _, exists := s.byID[id]; !exists {
			return id, nil
		}
	}
	return newID(16)
}

func (s *InMemoryStorage) AppendEntry(entry Entry) error {
	if entry.ID == "" {
		return fmt.Errorf("entry id is required")
	}
	if _, exists := s.byID[entry.ID]; exists {
		return fmt.Errorf("entry %q already exists", entry.ID)
	}
	if entry.ParentID != "" {
		if _, ok := s.byID[entry.ParentID]; !ok {
			return fmt.Errorf("parent entry %q not found", entry.ParentID)
		}
	}

	s.entries = append(s.entries, entry)
	s.byID[entry.ID] = entry
	s.leafID = entry.ID
	return nil
}

func (s *InMemoryStorage) Entry(id string) (Entry, bool) {
	entry, ok := s.byID[id]
	return entry, ok
}

func (s *InMemoryStorage) Entries() []Entry {
	return append([]Entry(nil), s.entries...)
}

func (s *InMemoryStorage) PathToRoot(leafID string) ([]Entry, error) {
	if leafID == "" {
		return nil, nil
	}

	path := make([]Entry, 0)
	current, ok := s.byID[leafID]
	if !ok {
		return nil, fmt.Errorf("entry %q not found", leafID)
	}
	for {
		path = append(path, current)
		if current.ParentID == "" {
			break
		}
		parentID := current.ParentID
		var ok bool
		current, ok = s.byID[parentID]
		if !ok {
			return nil, fmt.Errorf("parent entry %q not found", parentID)
		}
	}

	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path, nil
}
