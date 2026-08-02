package remediation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// State is the durable, non-secret record shared by the plan service and the
// separately deployed remediation worker. It deliberately stores plans,
// approvals and audit events, not kubeconfig, bearer tokens or raw requests.
type State struct {
	Plans     map[string]Plan     `json:"plans"`
	Approvals map[string]Approval `json:"approvals"`
	Applied   map[string]int64    `json:"applied"`
	Audit     []AuditEvent        `json:"audit"`
}

type Store interface {
	Load() (State, error)
	Save(State) error
}

// FileStore is an atomic, single-writer store for a PVC-mounted remediation
// record. A production multi-writer deployment must replace it with a store
// that supplies distributed compare-and-swap semantics.
type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{path: filepath.Join(dir, "remediation-state.json")}
}

func (s *FileStore) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	payload, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return emptyState(), nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read remediation store: %w", err)
	}
	state := emptyState()
	if err := json.Unmarshal(payload, &state); err != nil {
		return State{}, fmt.Errorf("decode remediation store: %w", err)
	}
	state.ensureMaps()
	return state, nil
}

func (s *FileStore) Save(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state.ensureMaps()
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode remediation store: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create remediation store: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".remediation-*.tmp")
	if err != nil {
		return fmt.Errorf("create remediation record: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("secure remediation record: %w", err)
	}
	if _, err := temporary.Write(payload); err != nil {
		temporary.Close()
		return fmt.Errorf("write remediation record: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync remediation record: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close remediation record: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("commit remediation record: %w", err)
	}
	return nil
}

func emptyState() State {
	return State{Plans: map[string]Plan{}, Approvals: map[string]Approval{}, Applied: map[string]int64{}, Audit: []AuditEvent{}}
}

func (s *State) ensureMaps() {
	if s.Plans == nil {
		s.Plans = map[string]Plan{}
	}
	if s.Approvals == nil {
		s.Approvals = map[string]Approval{}
	}
	if s.Applied == nil {
		s.Applied = map[string]int64{}
	}
	if s.Audit == nil {
		s.Audit = []AuditEvent{}
	}
}
