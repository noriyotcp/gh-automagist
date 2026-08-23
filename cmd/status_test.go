package cmd

import (
	"errors"
	"testing"

	"github.com/noriyo_tcp/gh-automagist/pkg/notify"
	"github.com/stretchr/testify/assert"
)

func TestStatusBadge_RendersBothAxes(t *testing.T) {
	cases := []struct {
		name   string
		status notify.FileStatus
		want   string
	}{
		{
			name:   "quiet on both axes",
			status: notify.FileStatus{},
			want:   "[in sync]",
		},
		{
			name:   "gist moved since we last observed it",
			status: notify.FileStatus{RemoteNewer: true},
			want:   "[remote: newer ⇧]",
		},
		{
			name:   "local edit never pushed",
			status: notify.FileStatus{LocalDirty: true},
			want:   "[local: unsynced]",
		},
		{
			name:   "both sides moved",
			status: notify.FileStatus{LocalDirty: true, RemoteNewer: true},
			want:   "[diverged: local + remote]",
		},
		{
			name:   "gist fetch failed",
			status: notify.FileStatus{Err: errors.New("simulated 500")},
			want:   "[error: simulated 500]",
		},
		{
			name:   "local file unreadable",
			status: notify.FileStatus{LocalErr: errors.New("no such file")},
			want:   "[local unreadable: no such file]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, statusBadge(tc.status))
		})
	}
}

func TestStatusBadge_LocalDirtyNeverReadsAsInSync(t *testing.T) {
	// The regression this guards: a file edited while the daemon was down used
	// to fall through to the default badge and report itself as in sync.
	badge := statusBadge(notify.FileStatus{LocalDirty: true, RemoteNewer: false})

	assert.NotEqual(t, "[in sync]", badge)
}
