package cliinstallcmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
)

func Main() int {
	if err := Run(os.Args[1:], os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func Run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("repo-cli-install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	mode := flags.String("mode", "install", "")
	buildDir := flags.String("build-dir", "", "")
	binDir := flags.String("bin-dir", "", "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *binDir == "" {
		return fmt.Errorf("usage: repo-cli-install -mode <install|retire> [-build-dir <path>] -bin-dir <path>")
	}

	var (
		results []cliinstall.Result
		err     error
	)
	switch *mode {
	case "install":
		if _, err := cliinstall.MigrateLegacy(*binDir); err != nil {
			return err
		}
		results, err = cliinstall.Install(*buildDir, *binDir)
	case "retire":
		results, err = cliinstall.Retire(*binDir)
	default:
		return fmt.Errorf("unsupported mode %q", *mode)
	}
	if err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(stdout, "%s: %s\n", result.Name, result.Status); err != nil {
			return err
		}
	}
	return nil
}
