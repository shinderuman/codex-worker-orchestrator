package mergejsoncmd

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/claudeoverride"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func Run(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("merge-json", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	target := flags.String("target", "", "")
	fragment := flags.String("fragment", "", "")
	override := flags.String("env-override", "", "")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *target == "" || *fragment == "" {
		return fmt.Errorf("usage: merge-json -target <path> -fragment <path> [-env-override <path>]")
	}
	overridePath := *override
	if overridePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("ホームディレクトリを取得できません: %w", err)
		}
		overridePath = claudeoverride.ResolvePath(home)
	}
	changed, err := settingsmerge.MergeFiles(*target, *fragment, overridePath)
	if err != nil {
		return err
	}
	result := "unchanged"
	if changed {
		result = "updated"
	}
	_, err = fmt.Fprintln(stdout, result)
	return err
}
