package monitor

import "github.com/noriyo_tcp/gh-automagist/pkg/state"

// ShouldSuppress returns true when a fsnotify write is a pull echo. The window
// bounds coincidental future SHA collisions; the SHA gate bounds genuine
// different-content edits happening inside the window.
func ShouldSuppress(fs state.FileState, currentSHA string, nowUnix int64) bool {
	if fs.PullSuppressUntil == 0 || nowUnix >= fs.PullSuppressUntil {
		return false
	}
	if fs.ContentSHA == "" {
		return false
	}
	return currentSHA == fs.ContentSHA
}
