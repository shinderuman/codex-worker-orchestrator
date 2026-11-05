package uninstallcmd

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudesettings"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func Run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	repo := flags.String("repo-root", "", "repository root")
	bin := flags.String("bin-dir", "", "installed binary directory")
	codex := flags.String("codex-dir", "", "Codex config directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *repo == "" || *bin == "" || *codex == "" {
		return fmt.Errorf("usage: uninstall -repo-root PATH -bin-dir PATH -codex-dir PATH")
	}
	if err := run(*repo, *bin, *codex, stdout); err != nil {
		return fmt.Errorf("uninstall incomplete: %w", err)
	}
	_, err := fmt.Fprintln(stdout, "uninstall complete")
	return err
}

func run(repoRoot, binDir, codexDir string, stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	location, err := claudesettings.Resolve(home, os.Getenv("CLAUDE_CONFIG_DIR"), os.Getenv("CLAUDE_SETTINGS_FILE"))
	if err != nil {
		return fmt.Errorf("resolve Claude settings: %w", err)
	}
	for _, path := range []string{repoRoot, binDir, codexDir, location.SettingsPath} {
		if err := rejectSymlinkComponents(path); err != nil {
			return err
		}
	}

	var failures []error
	attempt := func(label string, fn func() error) {
		if err := fn(); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", label, err))
			fmt.Fprintf(stdout, "uninstall %s: failed: %v\n", label, err)
		} else {
			fmt.Fprintf(stdout, "uninstall %s: completed or unchanged\n", label)
		}
	}
	attempt("repository hooks", func() error {
		cmd := exec.Command("sh", filepath.Join(repoRoot, "scripts", "manage-pull-hook.sh"),
			"retire", repoRoot, filepath.Join(binDir, "glm-publication-guard"))
		cmd.Stdout = stdout
		cmd.Stderr = io.Discard
		return cmd.Run()
	})
	attempt("Codex", func() error { return codexinstall.Retire(codexDir, stdout) })
	attempt("Claude", func() error { return settingsmerge.RetireFiles(location.SettingsPath) })
	attempt("CLI binaries", func() error {
		results, err := cliinstall.Retire(binDir)
		for _, result := range results {
			fmt.Fprintf(stdout, "%s: %s\n", result.Name, result.Status)
		}
		return err
	})
	return errors.Join(failures...)
}

func rejectSymlinkComponents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	current := string(filepath.Separator)
	volume := filepath.VolumeName(absolute)
	if volume != "" {
		current = volume + string(filepath.Separator)
	}
	relative := absolute[len(current):]
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink path component: %s", current)
		}
	}
	return nil
}
