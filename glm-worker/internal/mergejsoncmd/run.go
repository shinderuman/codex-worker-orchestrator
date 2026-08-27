package mergejsoncmd

import (
	"flag"
	"fmt"
	"io"

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
	changed, err := settingsmerge.MergeFiles(*target, *fragment, *override)
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
