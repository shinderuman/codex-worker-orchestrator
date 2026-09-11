package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstallcmd"
)

func main() {
	os.Exit(cliinstallcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
