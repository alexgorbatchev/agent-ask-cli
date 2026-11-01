package main

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexgorbatchev/agent-ask-cli/internal/request"
	"github.com/spf13/cobra"
)

const (
	defaultTimeout = 5 * time.Minute
	streamRanMark  = "stream-ran"
)

func main() {
	os.Exit(execute())
}

func execute() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := newRootCommand()
	cmd.SetContext(ctx)
	return run(cmd)
}

func run(cmd *cobra.Command) int {
	out := &outputTracker{w: cmd.OutOrStdout()}
	cmd.SetOut(out)
	err := cmd.Execute()
	if isMarked(cmd, streamRanMark) {
		// Run owns the terminal NDJSON record, so stderr only carries a failure the
		// stream could not deliver.
		if err != nil && out.lastFailed {
			_ = request.WriteError(cmd.ErrOrStderr(), err)
		}
		return exitCode(err)
	}
	switch {
	case out.err != nil:
		err = request.OutputFailure()
	case isMarked(cmd, helpRejectedMark):
		err = errUnknownHelpTarget
	}
	if err != nil {
		_ = request.WriteError(cmd.ErrOrStderr(), err)
	}
	return exitCode(err)
}

func exitCode(err error) int {
	if err != nil {
		return 1
	}
	return 0
}

// mark records an outcome on the root command, the one value run shares with the
// command tree.
func mark(root *cobra.Command, name string) {
	if root.Annotations == nil {
		root.Annotations = map[string]string{}
	}
	root.Annotations[name] = "true"
}

func isMarked(root *cobra.Command, name string) bool {
	return root.Annotations[name] == "true"
}

// outputTracker observes stdout writes. Cobra's help and completion paths drop
// write errors and its version path returns them unclassified, so run reads the
// outcome here to report every failure the same way.
type outputTracker struct {
	w          io.Writer
	err        error
	lastFailed bool
}

func (t *outputTracker) Write(p []byte) (int, error) {
	n, err := t.w.Write(p)
	t.lastFailed = err != nil
	if t.err == nil {
		t.err = err
	}
	return n, err
}
