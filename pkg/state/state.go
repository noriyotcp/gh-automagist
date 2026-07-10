package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// FileState is one entry in state.json; field names mirror the Ruby implementation for cross-tool interop.
type FileState struct {
	GistID    string `json:"gist_id"`
	UpdatedAt int64  `json:"updated_at"`
	Status    string `json:"status"` // e.g., "active"
}

type Manager struct {
	configDir string
	statePath string
	pidPath   string
	Files     map[string]FileState
}

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

// Load parses state.json; a missing file yields an empty state without error.
func (m *Manager) Load() error {
	data, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
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

// Save persists Files to state.json, creating the config directory if needed.
func (m *Manager) Save() error {
	err := os.MkdirAll(m.configDir, 0755)
	if err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Indented to match Ruby's JSON.pretty_generate output.
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

// AddTrackedFile upserts the file's tracked state.
func (m *Manager) AddTrackedFile(absPath, gistID string, updatedAt int64) {
	m.Files[absPath] = FileState{
		GistID:    gistID,
		UpdatedAt: updatedAt,
		Status:    "active",
	}
}

func (m *Manager) RemoveTrackedFile(absPath string) {
	delete(m.Files, absPath)
}

// WritePID writes the current process's PID to monitor.pid.
func (m *Manager) WritePID() error {
	pid := os.Getpid()
	return os.WriteFile(m.pidPath, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func (m *Manager) DeletePID() error {
	err := os.Remove(m.pidPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// KillMonitor sends SIGKILL to the given pid and clears the PID file.
//
// Returns:
//   - (true, nil) if the process was killed cleanly
//   - (false, nil) if the process was already gone (stale PID file, cleaned up)
//   - (false, err) on real Kill failures (e.g. EPERM). The PID file is NOT cleaned up
//     in this case, because the recorded process may still be alive.
//
// Callers should check GetPID() != 0 before calling.
func (m *Manager) KillMonitor(pid int) (killed bool, err error) {
	process, _ := os.FindProcess(pid) // Unix: always succeeds regardless of process existence
	killErr := process.Kill()
	switch {
	case killErr == nil:
		m.DeletePID()
		return true, nil
	case errors.Is(killErr, os.ErrProcessDone) || errors.Is(killErr, syscall.ESRCH):
		m.DeletePID()
		return false, nil
	default:
		return false, fmt.Errorf("failed to kill monitor (PID %d): %w", pid, killErr)
	}
}

// GetPID reads the monitor.pid file; returns 0 if missing.
func (m *Manager) GetPID() int {
	data, err := os.ReadFile(m.pidPath)
	if err != nil {
		return 0
	}
	var pid int
	fmt.Sscanf(string(data), "%d", &pid)
	return pid
}
