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
	if len(args) == 0 {
		return usage(stderr)
	}
	if args[0] == "--backend" {
		if len(args) != 2 {
			return usage(stderr)
		}
		return backendSelection(args[1], stdout, stderr)
	}
	if args[0] == "--staging" {
		return stagedExecution(args[1:], stdout, stderr)
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

func stagedExecution(args []string, stdout, stderr io.Writer) int {
	if len(args) < 3 || args[1] != "--responses" {
		return usage(stderr)
	}
	testArgs := args[3:]
	if len(testArgs) == 0 {
		testArgs = []string{"./..."}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := &dockertest.StagingClient{StagingDir: args[0], ResponseDir: args[2]}
	return client.Run(ctx, testArgs, stdout, stderr)
}

func usage(stderr io.Writer) int {
	_, _ = fmt.Fprintln(stderr, "usage: goquality <repository> [go test arguments...] | --backend <repository> | --staging <dir> --responses <dir> [go test arguments...]")
	return 2
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
