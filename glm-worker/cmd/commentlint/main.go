package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/commentlintcmd"
)

func main() {
	os.Exit(commentlintcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
