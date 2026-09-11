package codexinstallcmd

import (
	"flag"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
)

func Run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("codex-install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repoRoot := flags.String("repo-root", "", "")
	codexDir := flags.String("codex-dir", "", "")
	if err := flags.Parse(args); err != nil {
		return usageError()
	}
	if *repoRoot == "" || *codexDir == "" || flags.NArg() != 0 {
		return usageError()
	}
	return codexinstall.Install(*repoRoot, *codexDir, stdout)
}

func usageError() error {
	return fmt.Errorf("usage: codex-install --repo-root PATH --codex-dir PATH")
}
