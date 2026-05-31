package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type State struct {
	mu            sync.Mutex
	path          string
	Tasks         []ScheduledTask         `json:"tasks"`
	Listeners     map[string]bool         `json:"listeners"`
	Conversations map[string][]ChatRecord `json:"conversations"`
	AuditLog      []AuditRecord           `json:"audit_log"`
}

type ChatRecord struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type AuditRecord struct {
	Time    time.Time `json:"time"`
	Tool    string    `json:"tool"`
	Input   string    `json:"input"`
	Status  string    `json:"status"`
	Summary string    `json:"summary"`
}

func LoadState(dataDir string) (*State, error) {
	path := filepath.Join(dataDir, "state.json")
	st := &State{
		path:          path,
		Listeners:     map[string]bool{},
		Conversations: map[string][]ChatRecord{},
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return st, st.Save()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	st.path = path
	if st.Listeners == nil {
		st.Listeners = map[string]bool{}
	}
	if st.Conversations == nil {
		st.Conversations = map[string][]ChatRecord{}
	}
	return st, nil
}

func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *State) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *State) AddConversation(key, role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	records := append(s.Conversations[key], ChatRecord{Role: role, Content: content, CreatedAt: time.Now()})
	if len(records) > 20 {
		records = records[len(records)-20:]
	}
	s.Conversations[key] = records
	return s.saveLocked()
}

func (s *State) Conversation(key string) []ChatRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChatRecord, len(s.Conversations[key]))
	copy(out, s.Conversations[key])
	return out
}

func (s *State) AddAudit(tool, input, status, summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.AuditLog = append(s.AuditLog, AuditRecord{Time: time.Now(), Tool: tool, Input: input, Status: status, Summary: summary})
	if len(s.AuditLog) > 200 {
		s.AuditLog = s.AuditLog[len(s.AuditLog)-200:]
	}
	_ = s.saveLocked()
}

func targetKey(msgType string, groupID, userID int64) string {
	if msgType == "group" && groupID > 0 {
		return "group:" + formatInt(groupID)
	}
	return "private:" + formatInt(userID)
}
