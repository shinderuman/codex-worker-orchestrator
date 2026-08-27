package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslintcmd"
)

func main() {
	os.Exit(harnesslintcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
