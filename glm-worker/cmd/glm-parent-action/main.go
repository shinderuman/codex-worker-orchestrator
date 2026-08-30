package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
)

func main() {
	os.Exit(parentactioncmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
