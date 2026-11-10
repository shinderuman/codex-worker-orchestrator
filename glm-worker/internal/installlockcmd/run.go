package installlockcmd

import (
	"fmt"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/installlock"
)

func Run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: install-lock --directory|--acquire")
	}
	switch args[0] {
	case "--directory":
		dir, err := installlock.Directory()
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, dir)
		return err
	case "--acquire":
		file := os.NewFile(9, "install-lifecycle-lock")
		defer func() { _ = file.Close() }()
		return installlock.Acquire(file)
	default:
		return fmt.Errorf("usage: install-lock --directory|--acquire")
	}
}
