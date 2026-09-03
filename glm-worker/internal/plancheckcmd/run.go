package plancheckcmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const guidance = "Plan・IMPLEMENTATION_TASKSを実状態へ同期し、同期済みfinal HEADでplancheckが通過してからinstall・次task・handoffへ進む"

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 {
		if _, err := fmt.Fprintln(stderr, "usage: plancheck <repository-root>"); err != nil {
			return 1
		}
		return 2
	}
	result, err := workflow.CheckFinalHeadPlan(args[0])
	if err != nil {
		if _, writeErr := fmt.Fprintf(stderr, "plan final head: %v\n", err); writeErr != nil {
			return 1
		}
		if _, writeErr := fmt.Fprintf(stderr, "plan final head: %s\n", guidance); writeErr != nil {
			return 1
		}
		return 1
	}
	if _, err := fmt.Fprintln(stdout, result); err != nil {
		return 1
	}
	return 0
}
