package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/goqualitycmd"
)

func main() {
	os.Exit(goqualitycmd.Run(os.Args[1:]))
}
