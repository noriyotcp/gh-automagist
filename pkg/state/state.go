package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// FileState represents the synchronization state of a single monitored file.
// JSON tags are explicitly defined to perfectly match the Ruby implementation's format:
// {"/path/to/file": {"gist_id": "...", "updated_at": 12345, "status": "active"}}
type FileState struct {
	GistID    string `json:"gist_id"`
	UpdatedAt int64  `json:"updated_at"`
	Status    string `json:"status"` // e.g., "active"
}

// Manager handles reading and writing the ~/.config/gh-automagist/state.json file.
type Manager struct {
	configDir string
	statePath string
	pidPath   string
	Files     map[string]FileState
}

// NewManager initializes a new state manager, using the user's home directory.
func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "gh-automagist")
	statePath := filepath.Join(configDir, "state.json")
	pidPath := filepath.Join(configDir, "monitor.pid")

	return &Manager{
		configDir: configDir,
		statePath: statePath,
		pidPath:   pidPath,
		Files:     make(map[string]FileState),
	}, nil
}

// Load reads the state.json file and parses it into the Files map.
// If the file does not exist, it starts with an empty map (graceful fallback).
func (m *Manager) Load() error {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// It's perfectly normal for the file to not exist on first run
			m.Files = make(map[string]FileState)
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	err = json.Unmarshal(data, &m.Files)
	if err != nil {
		return fmt.Errorf("failed to parse state json: %w", err)
	}

	return nil
}

// Save writes the current Files map back to state.json.
// It ensures the directory exists before writing.
func (m *Manager) Save() error {
	// Ensure the config directory exists (mkdir -p)
	err := os.MkdirAll(m.configDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Marshal with indentation to match the Ruby JSON.pretty_generate format
	data, err := json.MarshalIndent(m.Files, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode state json: %w", err)
	}

	err = os.WriteFile(m.statePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	return nil
}

// AddTrackedFile registers a new file to be monitored or updates an existing one.
func (m *Manager) AddTrackedFile(absPath, gistID string, updatedAt int64) {
	m.Files[absPath] = FileState{
		GistID:    gistID,
		UpdatedAt: updatedAt,
		Status:    "active",
	}
}

// RemoveTrackedFile stops monitoring a file.
func (m *Manager) RemoveTrackedFile(absPath string) {
	delete(m.Files, absPath)
}

// WritePID writes the current process ID to monitor.pid.
func (m *Manager) WritePID() error {
	pid := os.Getpid()
	return os.WriteFile(m.pidPath, []byte(fmt.Sprintf("%d", pid)), 0644)
}

// DeletePID removes the monitor.pid file.
func (m *Manager) DeletePID() error {
	err := os.Remove(m.pidPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetPID reads the PID from the monitor.pid file. returns 0 if not found.
func (m *Manager) GetPID() int {
	data, err := os.ReadFile(m.pidPath)
	if err != nil {
		return 0
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	return pid
}
