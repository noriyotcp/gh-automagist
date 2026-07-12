package monitor

import "github.com/noriyo_tcp/gh-automagist/pkg/state"

// ShouldSuppress reports whether a fsnotify write event should be treated as
// an echo of a recent `pull` and skipped, avoiding a redundant Gist PATCH.
//
// Both a time window (fs.PullSuppressUntil) and content check (fs.ContentSHA)
// must agree: the window narrows the blast radius so a real edit made minutes
// later with a coincidentally matching SHA is never suppressed, and the SHA
// check prevents suppressing a legit different-content edit made inside the
// window. ContentSHA being empty short-circuits — no suppression without a
// baseline to compare against.
func ShouldSuppress(fs state.FileState, currentSHA string, nowUnix int64) bool {
	if fs.PullSuppressUntil == 0 || nowUnix >= fs.PullSuppressUntil {
		return false
	}
	if fs.ContentSHA == "" {
		return false
	}
	return currentSHA == fs.ContentSHA
}
