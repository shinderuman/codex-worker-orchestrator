package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
)

func main() {
	mode := flag.String("mode", "install", "install or retire repository CLIs")
	buildDir := flag.String("build-dir", "", "directory containing freshly built repository CLIs")
	binDir := flag.String("bin-dir", "", "destination directory for repository CLIs")
	flag.Parse()

	var (
		results []cliinstall.Result
		err     error
	)
	switch *mode {
	case "install":
		results, err = cliinstall.Install(*buildDir, *binDir)
	case "retire":
		results, err = cliinstall.Retire(*binDir)
	default:
		err = fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, result := range results {
		fmt.Printf("%s: %s\n", result.Name, result.Status)
	}
}
