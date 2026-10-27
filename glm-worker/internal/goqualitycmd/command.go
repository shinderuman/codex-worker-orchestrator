package goqualitycmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func Run(args []string) int {
	return run(args, os.Stdout, os.Stderr)
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] == "--backend" && len(args) != 2) {
		_, _ = fmt.Fprintln(stderr, "usage: goquality <repository> [go test arguments...] | --backend <repository>")
		return 2
	}
	if args[0] == "--backend" {
		return backendSelection(args[1], stdout, stderr)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	code, err := dockertest.Run(ctx, args[0], args[1:], stdout, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality test:", err)
		if code == 0 {
			return 1
		}
	}
	return code
}

func backendSelection(repository string, stdout, stderr io.Writer) int {
	active, err := qualitygate.DockerBackendActive(repository)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "goquality backend:", err)
		return 1
	}
	if active {
		_, _ = fmt.Fprintln(stdout, qualitygate.BackendDockerARM64)
		return 0
	}
	_, _ = fmt.Fprintln(stdout, qualitygate.BackendNative)
	return 0
}
