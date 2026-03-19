package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// FileStateAdapter persists state to a JSON file with atomic writes.
type FileStateAdapter struct {
	path string
	mu   sync.Mutex
}

// NewFileStateAdapter creates a new file-based state adapter.
func NewFileStateAdapter(path string) *FileStateAdapter {
	return &FileStateAdapter{path: path}
}

func (a *FileStateAdapter) Load(_ context.Context) (*domain.State, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.NewState(), nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state domain.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	// Ensure maps are initialized
	if state.Environments == nil {
		state.Environments = make(map[string]*domain.EnvironmentEntry)
	}

	return &state, nil
}

func (a *FileStateAdapter) Save(_ context.Context, state *domain.State) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.writeAtomic(state)
}

func (a *FileStateAdapter) GetEnvironment(ctx context.Context, name string) (*domain.EnvironmentEntry, error) {
	state, err := a.Load(ctx)
	if err != nil {
		return nil, err
	}
	entry, ok := state.Environments[name]
	if !ok {
		return nil, nil
	}
	return entry, nil
}

func (a *FileStateAdapter) SetEnvironment(ctx context.Context, name string, entry *domain.EnvironmentEntry) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	state, err := a.loadUnsafe()
	if err != nil {
		return err
	}
	state.Environments[name] = entry
	return a.writeAtomic(state)
}

func (a *FileStateAdapter) RemoveEnvironment(ctx context.Context, name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	state, err := a.loadUnsafe()
	if err != nil {
		return err
	}
	delete(state.Environments, name)
	return a.writeAtomic(state)
}

// loadUnsafe reads state without acquiring the mutex (caller must hold it).
func (a *FileStateAdapter) loadUnsafe() (*domain.State, error) {
	data, err := os.ReadFile(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return domain.NewState(), nil
		}
		return nil, fmt.Errorf("reading state file: %w", err)
	}

	var state domain.State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing state file: %w", err)
	}

	if state.Environments == nil {
		state.Environments = make(map[string]*domain.EnvironmentEntry)
	}

	return &state, nil
}

// writeAtomic writes state to a temp file and renames it to the target path.
func (a *FileStateAdapter) writeAtomic(state *domain.State) error {
	dir := filepath.Dir(a.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling state: %w", err)
	}

	tmpFile := a.path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o644); err != nil {
		return fmt.Errorf("writing temp state file: %w", err)
	}

	if err := os.Rename(tmpFile, a.path); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("renaming state file: %w", err)
	}

	return nil
}
