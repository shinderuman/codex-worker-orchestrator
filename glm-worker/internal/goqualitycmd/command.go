package goqualitycmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
)

func Run(args []string) int {
	return run(args, os.Stdout, os.Stderr)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		_, _ = fmt.Fprintln(stderr, "usage: goquality <repository> [go test arguments...]")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	code, err := dockertest.Run(ctx, args[0], args[1:], stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality test-linux:", err)
		if code == 0 {
			return 1
		}
	}
	return code
}
