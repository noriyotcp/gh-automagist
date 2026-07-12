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

	RemoteUpdatedAt int64  `json:"remote_updated_at,omitempty"`
	ContentSHA      string `json:"content_sha,omitempty"`

	// PullSuppressUntil is a unix-second deadline; paired with ContentSHA it
	// gates the daemon's post-pull PATCH via pkg/monitor.ShouldSuppress.
	PullSuppressUntil int64 `json:"pull_suppress_until,omitempty"`
}

// MonitorInfo is the daemon's self-report, written when the monitor comes up
// and removed at shutdown. Kept in a separate file from state.json (which is
// the tracked-files map) so runtime metadata does not mix with tracked data.
type MonitorInfo struct {
	PID       int    `json:"pid"`
	Version   string `json:"version"`
	Commit    string `json:"commit,omitempty"`
	StartedAt int64  `json:"started_at"`
}

type Manager struct {
	configDir       string
	statePath       string
	pidPath         string
	monitorInfoPath string
	Files           map[string]FileState
}

func NewManager() (*Manager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not determine home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "gh-automagist")
	statePath := filepath.Join(configDir, "state.json")
	pidPath := filepath.Join(configDir, "monitor.pid")
	monitorInfoPath := filepath.Join(configDir, "monitor.info")

	return &Manager{
		configDir:       configDir,
		statePath:       statePath,
		pidPath:         pidPath,
		monitorInfoPath: monitorInfoPath,
		Files:           make(map[string]FileState),
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

// Save persists Files to state.json atomically (tmp + rename), creating the
// config directory if needed. A partial write during shutdown leaves the
// previous file intact rather than truncated.
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

	tmpPath := m.statePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}
	if err := os.Rename(tmpPath, m.statePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("failed to rename state file into place: %w", err)
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

// WriteMonitorInfo persists the daemon's runtime metadata alongside monitor.pid.
// Callers write this once at daemon startup; status reads it to show the
// running daemon's version and detect daemon-vs-binary drift.
func (m *Manager) WriteMonitorInfo(info MonitorInfo) error {
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode monitor info: %w", err)
	}
	return os.WriteFile(m.monitorInfoPath, data, 0644)
}

// ReadMonitorInfo returns nil (no error) when the file does not exist, so
// old daemons that only wrote monitor.pid are handled the same as a missing
// info file — callers treat "no info" as "version unknown".
func (m *Manager) ReadMonitorInfo() (*MonitorInfo, error) {
	data, err := os.ReadFile(m.monitorInfoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read monitor info: %w", err)
	}
	var info MonitorInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to parse monitor info: %w", err)
	}
	return &info, nil
}

func (m *Manager) DeleteMonitorInfo() error {
	err := os.Remove(m.monitorInfoPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// KillMonitor sends SIGKILL to the given pid and clears the PID file
// (and monitor.info if present).
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
		m.DeleteMonitorInfo()
		return true, nil
	case errors.Is(killErr, os.ErrProcessDone) || errors.Is(killErr, syscall.ESRCH):
		m.DeletePID()
		m.DeleteMonitorInfo()
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
