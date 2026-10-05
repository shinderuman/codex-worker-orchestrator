package workflow

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const observationRouteMarker = "OBSERVATION_EXECUTION_ROUTE:"

const observationResultsMarker = "OBSERVATION_MACHINE_EXECUTION_RESULTS:"

func observationExecutionRouteBlock() string {
	return fmt.Sprintf(`
%s
このtaskはpoc/observation宣言のためread-only capabilityで実行されます。production implementationとしてrepositoryへ書き込むEdit/Write/Bashは使えず、昇格は親がExternal feasibility宣言を書き換えてから行います。
実測・test実行・artifact生成など完了に実行が必要な場合は、必要なoperation(shadow-eval/go-test/go-test-race)と対象・理由をNEEDS_SOL_DECISIONとして返してください。機械実行は親がstagedなobservation-execute routeで閉登録operationだけに限定して実行し、入力・deadline・書込範囲をmachineが拘束します。結果は次のdecision継続で%sとしてtyped注入されます。失敗・timeout・partial出力は成功として扱いません。
`, observationRouteMarker, observationResultsMarker)
}

func (w *Workflow) observationExecutionResultsBlock() string {
	current, err := w.state.ObservationExecutionRound()
	if err != nil || current < 1 {
		return ""
	}
	records, err := w.state.ObservationExecutionsForRound(current)
	if err != nil || len(records) == 0 {
		return ""
	}
	var block strings.Builder
	block.WriteString(fmt.Sprintf("\n%s\n", observationResultsMarker))
	block.WriteString(fmt.Sprintf("decision round %dで機械実行した観測結果です。machine executorが生成した正規evidenceとしてread-onlyで解析してください。\n", current))
	for _, record := range records {
		block.WriteString(observationExecutionResultLine(record))
	}
	block.WriteString("同一roundでの同一operation・同一parameterの再実行要求はmachineが拒否します。追加の測定が必要な場合は異なるparameterまたは別operationを根拠付きで指定してください。結果の意味判断とGo/No-Goは親Solが持ちます。\n")
	return block.String()
}

func observationExecutionResultLine(record state.ObservationExecutionRecord) string {
	artifacts := strings.Join(record.Artifacts, ";")
	fields := []string{
		fmt.Sprintf("execution_id=%s", record.ExecutionID),
		fmt.Sprintf("operation=%s", record.Operation),
		fmt.Sprintf("status=%s", record.Status),
	}
	if record.ExitSource != "" {
		fields = append(fields, fmt.Sprintf("exit_source=%s", record.ExitSource))
	}
	if record.Detail != "" {
		fields = append(fields, fmt.Sprintf("detail=%s", record.Detail))
	}
	if artifacts != "" {
		fields = append(fields, fmt.Sprintf("artifacts=%s", artifacts))
	}
	return "- " + strings.Join(fields, " ") + "\n"
}

func withObservationExecutionRoute(prompt string, pocStage bool) string {
	if !pocStage {
		return prompt
	}
	return strings.TrimRight(prompt, "\n") + observationExecutionRouteBlock()
}
