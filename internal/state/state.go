package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"mineru-ocr/internal/artifact"

	"github.com/zeebo/xxh3"
)

// ErrCorrupt reports that a state file was unreadable and got backed up.
var ErrCorrupt = errors.New("state: corrupt")

// Error is the persisted failure of one file.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FileRecord is the persisted outcome for one source file.
type FileRecord struct {
	Size       int64     `json:"size"`
	Hash       string    `json:"hash"`
	Tier       string    `json:"tier"`
	ZipPath    string    `json:"zip_path"`
	Status     string    `json:"status"`
	Error      *Error    `json:"error"`
	DurationMS int64     `json:"duration_ms"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ServiceRecord is the service instance this tool started for the root.
type ServiceRecord struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

// State is one root's state file contents.
type State struct {
	SchemaVersion int                   `json:"schema_version"`
	ToolVersion   string                `json:"tool_version"`
	Files         map[string]FileRecord `json:"files"`
	Service       *ServiceRecord        `json:"service"`
	mu            sync.Mutex            `json:"-"`
}

// Path returns the state file path for root, a directory or a file.
func Path(root string) string {
	info, err := os.Stat(root)
	if err == nil && info.IsDir() {
		return filepath.Join(root, ".mineru-state.json")
	}
	return filepath.Join(filepath.Dir(root), ".mineru-state.json")
}

func emptyState() *State {
	return &State{
		SchemaVersion: 1,
		ToolVersion:   "0.1.0",
		Files:         map[string]FileRecord{},
	}
}

// Load reads the state file for root, backing up and resetting a corrupt one.
func Load(root string) (*State, error) {
	p := Path(root)
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return emptyState(), nil
		}
		return nil, err
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		_ = os.Rename(p, p+".bak")
		return emptyState(), ErrCorrupt
	}
	if s.Files == nil {
		s.Files = map[string]FileRecord{}
	}
	if s.SchemaVersion == 0 {
		s.SchemaVersion = 1
	}
	if s.ToolVersion == "" {
		s.ToolVersion = "0.1.0"
	}
	return &s, nil
}

// Save atomically rewrites the state file for root.
func (s *State) Save(root string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Files == nil {
		s.Files = map[string]FileRecord{}
	}
	p := Path(root)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ShouldSkip reports whether path is completed, unchanged and has a valid zip.
func (s *State) ShouldSkip(path, hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.Files[path]
	if !ok {
		return false
	}
	if rec.Status != "completed" {
		return false
	}
	if rec.Hash != hash {
		return false
	}
	if rec.ZipPath == "" {
		return false
	}
	return artifact.Validate(rec.ZipPath) == nil
}

// Fingerprint hashes path with xxh3-128, returning the hash and size.
func Fingerprint(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	h := xxh3.Hash128(data)
	return fmt.Sprintf("%x", h.Bytes()), info.Size(), nil
}
