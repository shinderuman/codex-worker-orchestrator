package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/webgptdispatchcmd"
)

func main() {
	os.Exit(webgptdispatchcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
