package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// callOutliersTelemetryは分析対象telemetry dirの所在と読み取り状態。statusはok(全fileを
// 読めた)・partial(読めないtask fileがあった)・none(dir不在・空、または.jsonl fileがない)。
// ignored_filesはtask IDとして生成形式に合わないfile名で、読まずにfail visibleにする。
type callOutliersTelemetry struct {
	Status          string                   `json:"status"`
	Dir             string                   `json:"dir"`
	Files           int                      `json:"files"`
	IgnoredFiles    []string                 `json:"ignored_files,omitempty"`
	UnreadableTasks []callOutliersUnreadable `json:"unreadable_tasks,omitempty"`
}

type callOutliersUnreadable struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type callOutliersOutput struct {
	Telemetry callOutliersTelemetry   `json:"telemetry"`
	Report    state.CallOutlierReport `json:"report"`
}

// printCallOutliersは保存済みtelemetry JSONLだけからtask/phase/session/model別の呼出分布・
// task単位増幅・outlierをmachine JSON 1行で出す参照専用command。state書換・repo lock・
// AI呼出を行わず、prompt/response本文も読み取り対象のrecordに含まれていても出さない。
// telemetry dirの読取り失敗(不在以外)はnoneへ偽装せずerrorへ流し、process境界の
// internal error・non-zero exitへ出す。読めないtask fileがあっても他taskの集計は出し、
// unreadable_tasksへ失敗を残す。
func printCallOutliers(st *state.StateStore, stdout io.Writer) error {
	dir := st.Path("telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("telemetry dirを読めません: %w", err)
	}

	output := callOutliersOutput{
		Telemetry: callOutliersTelemetry{Status: "ok", Dir: dir},
	}
	taskLogs := make([]state.TaskCallLogs, 0, len(entries))
	considered := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		considered++
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !state.ValidGeneratedUUID(taskID) {
			output.Telemetry.IgnoredFiles = append(output.Telemetry.IgnoredFiles, name)
			continue
		}
		logs, readErr := st.ReadModelCallLogs(taskID)
		if readErr == nil {
			output.Telemetry.Files++
			taskLogs = append(taskLogs, state.TaskCallLogs{TaskID: taskID, Logs: logs})
			continue
		}
		output.Telemetry.Status = "partial"
		output.Telemetry.UnreadableTasks = append(output.Telemetry.UnreadableTasks, callOutliersUnreadable{
			TaskID: taskID,
			Error:  readErr.Error(),
		})
	}
	if considered == 0 {
		output.Telemetry.Status = "none"
		output.Report = state.BuildCallOutlierReport(nil)
		return writeJSON(stdout, output)
	}

	output.Report = state.BuildCallOutlierReport(taskLogs)
	return writeJSON(stdout, output)
}
