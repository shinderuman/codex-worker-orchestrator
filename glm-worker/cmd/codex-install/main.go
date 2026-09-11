package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
)

func main() {
	repoRoot := flag.String("repo-root", "", "repository root")
	codexDir := flag.String("codex-dir", "", "Codex configuration directory")
	flag.Parse()
	if *repoRoot == "" || *codexDir == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: codex-install --repo-root PATH --codex-dir PATH")
		os.Exit(2)
	}
	if err := codexinstall.Install(*repoRoot, *codexDir, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
