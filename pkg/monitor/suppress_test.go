package monitor

import (
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

func TestShouldSuppress(t *testing.T) {
	const sha = "a1b2c3d4"
	const otherSHA = "deadbeef"

	tests := []struct {
		name    string
		fs      state.FileState
		curSHA  string
		nowUnix int64
		want    bool
	}{
		{
			name:    "window active, sha matches → suppress",
			fs:      state.FileState{PullSuppressUntil: 200, ContentSHA: sha},
			curSHA:  sha,
			nowUnix: 150,
			want:    true,
		},
		{
			name:    "window active, sha differs → do not suppress",
			fs:      state.FileState{PullSuppressUntil: 200, ContentSHA: sha},
			curSHA:  otherSHA,
			nowUnix: 150,
			want:    false,
		},
		{
			name:    "window expired, sha matches → do not suppress",
			fs:      state.FileState{PullSuppressUntil: 100, ContentSHA: sha},
			curSHA:  sha,
			nowUnix: 200,
			want:    false,
		},
		{
			name:    "window boundary (now == until) → do not suppress",
			fs:      state.FileState{PullSuppressUntil: 200, ContentSHA: sha},
			curSHA:  sha,
			nowUnix: 200,
			want:    false,
		},
		{
			name:    "PullSuppressUntil zero → do not suppress",
			fs:      state.FileState{PullSuppressUntil: 0, ContentSHA: sha},
			curSHA:  sha,
			nowUnix: 150,
			want:    false,
		},
		{
			name:    "ContentSHA empty → do not suppress (no baseline)",
			fs:      state.FileState{PullSuppressUntil: 200, ContentSHA: ""},
			curSHA:  sha,
			nowUnix: 150,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldSuppress(tt.fs, tt.curSHA, tt.nowUnix)
			if got != tt.want {
				t.Errorf("ShouldSuppress(%+v, %q, %d) = %v, want %v",
					tt.fs, tt.curSHA, tt.nowUnix, got, tt.want)
			}
		})
	}
}
