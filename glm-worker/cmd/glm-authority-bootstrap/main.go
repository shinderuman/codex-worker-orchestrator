package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
)

func main() {
	os.Exit(authoritybootstrapcmd.Run(os.Args[1:], os.Stdout, os.Stderr))
}
