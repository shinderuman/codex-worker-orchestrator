package main

import (
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:]); err != nil {

		_ = app.WriteProcessError(os.Stderr, err)
		os.Exit(1)
	}
}
