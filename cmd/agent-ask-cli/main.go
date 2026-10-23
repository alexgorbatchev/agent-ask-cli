package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexgorbatchev/agent-ask-cli/internal/request"
	"github.com/spf13/cobra"
)

const defaultTimeout = 5 * time.Minute

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
	if err := cmd.Execute(); err != nil {
		// Run owns terminal NDJSON errors. Parse errors still need a stderr record.
		if !streamWasRun(cmd) {
			_ = request.WriteError(cmd.ErrOrStderr(), err)
		}
		return 1
	}
	return 0
}

func streamWasRun(root *cobra.Command) bool {
	return root.Annotations != nil && root.Annotations["stream-ran"] == "true"
}
