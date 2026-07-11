package cmd

import (
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/monitor"
)

// resolveDebounce picks the effective debounce interval from a triaged set of
// sources: an explicit CLI flag wins, otherwise a parseable env var, otherwise
// monitor.DefaultDebounceInterval. When envRaw is non-empty but unparseable,
// returns the default plus a non-nil error so the caller can surface a warning.
func resolveDebounce(flagChanged bool, flagValue time.Duration, envRaw string) (time.Duration, error) {
	if flagChanged {
		return flagValue, nil
	}
	if envRaw != "" {
		d, err := time.ParseDuration(envRaw)
		if err != nil {
			return monitor.DefaultDebounceInterval, err
		}
		return d, nil
	}
	return monitor.DefaultDebounceInterval, nil
}
