package plancheckcmd

import (
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const guidance = "IMPLEMENTATION_RULES.mdのtask完了契約に従いPlan・IMPLEMENTATION_TASKS・Historyを同期し、同一commitへamendしてからinstallすること。同期済みfinal HEADになるまでinstall・次task・handoffへ進まない"

func Run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: plancheck <repository-root>")
		return 2
	}
	result, err := workflow.CheckFinalHeadPlan(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "plan final head: %v\n", err)
		fmt.Fprintf(stderr, "plan final head: %s\n", guidance)
		return 1
	}
	fmt.Fprintln(stdout, result)
	return 0
}
