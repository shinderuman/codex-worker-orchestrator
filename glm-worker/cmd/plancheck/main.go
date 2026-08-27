package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/plancheckcmd"
)

func main() {
	os.Exit(plancheckcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
