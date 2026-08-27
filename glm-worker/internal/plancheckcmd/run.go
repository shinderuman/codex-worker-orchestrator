package plancheckcmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const guidance = "IMPLEMENTATION_RULES.mdのtask完了契約に従いPlan・IMPLEMENTATION_TASKS・Historyを同期し、同一commitへamendしてからinstallすること。同期済みfinal HEADになるまでinstall・次task・handoffへ進まない"

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
