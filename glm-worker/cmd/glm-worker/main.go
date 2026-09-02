package main

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--authority" {
		if err := authoritybootstrapcmd.Execute(os.Args[2:], os.Stdout); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := app.Run(os.Args[1:]); err != nil {
		_ = app.WriteProcessError(os.Stderr, err)
		os.Exit(1)
	}
}
