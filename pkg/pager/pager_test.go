package pager

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout swaps os.Stdout for a pipe, drains it into buf until w is
// closed, and restores os.Stdout on cleanup. Callers write via the returned
// writer or through code paths that reference os.Stdout.
func captureStdout(t *testing.T) (w *os.File, buf *bytes.Buffer, done <-chan struct{}) {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	buffer := &bytes.Buffer{}
	ch := make(chan struct{})
	go func() {
		_, _ = io.Copy(buffer, r)
		close(ch)
	}()

	t.Cleanup(func() {
		os.Stdout = orig
	})
	return w, buffer, ch
}

func TestRun_NoPager_WritesDirectlyToStdout(t *testing.T) {
	w, buf, done := captureStdout(t)

	err := Run(true, func(out io.Writer) error {
		_, err := out.Write([]byte("hello world\n"))
		return err
	})
	w.Close()
	<-done

	require.NoError(t, err)
	assert.Equal(t, "hello world\n", buf.String())
}

func TestRun_NonTty_SkipsPager(t *testing.T) {
	// Under `go test`, os.Stdout is a pipe (not a tty). Even without noPager,
	// Run should therefore write directly to stdout — this test guards that
	// tty-detection branch stays wired to the non-pager path.
	w, buf, done := captureStdout(t)

	err := Run(false, func(out io.Writer) error {
		_, err := out.Write([]byte("piped\n"))
		return err
	})
	w.Close()
	<-done

	require.NoError(t, err)
	assert.Equal(t, "piped\n", buf.String())
}

func TestRun_FnError_Propagates(t *testing.T) {
	w, _, done := captureStdout(t)
	sentinel := errors.New("fn boom")

	err := Run(true, func(out io.Writer) error {
		return sentinel
	})
	w.Close()
	<-done

	assert.ErrorIs(t, err, sentinel)
}
