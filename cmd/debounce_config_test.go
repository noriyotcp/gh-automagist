package cmd

import (
	"testing"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveDebounce_FlagBeatsEnv(t *testing.T) {
	d, err := resolveDebounce(true, 3*time.Second, "10s")
	require.NoError(t, err)
	assert.Equal(t, 3*time.Second, d)
}

func TestResolveDebounce_FlagBeatsMalformedEnv(t *testing.T) {
	// Flag path must not even parse the env var.
	d, err := resolveDebounce(true, 2*time.Second, "garbage")
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, d)
}

func TestResolveDebounce_EnvWhenNoFlag(t *testing.T) {
	d, err := resolveDebounce(false, 0, "7s")
	require.NoError(t, err)
	assert.Equal(t, 7*time.Second, d)
}

func TestResolveDebounce_DefaultWhenBothMissing(t *testing.T) {
	d, err := resolveDebounce(false, 0, "")
	require.NoError(t, err)
	assert.Equal(t, monitor.DefaultDebounceInterval, d)
}

func TestResolveDebounce_MalformedEnvFallsBackToDefaultAndErrors(t *testing.T) {
	d, err := resolveDebounce(false, 0, "not-a-duration")
	require.Error(t, err)
	assert.Equal(t, monitor.DefaultDebounceInterval, d,
		"returned interval must be the safe default when env is unparseable")
}

func TestResolveDebounce_ZeroFlagDisablesDebounce(t *testing.T) {
	// A zero (or negative) flag value is a legit "disable debouncing" signal;
	// Watcher.DebounceInterval semantics accept it as "fire OnChange immediately."
	d, err := resolveDebounce(true, 0, "")
	require.NoError(t, err)
	assert.Equal(t, time.Duration(0), d)
}
