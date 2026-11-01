package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type correctionResumeInput struct {
	TaskID       string
	SessionID    string
	SourceCallID string
	ResponseHash string
	Result       packet.Result
}

func (w *Workflow) resultCorrectionResumePrompt(checkpoint state.ResumeCheckpoint) (string, error) {
	input, err := w.resultCorrectionResumeInput(checkpoint)
	if err != nil {
		return "", err
	}
	if input == nil {
		return resumePrompt(checkpoint), nil
	}
	body, err := json.Marshal(input.Result)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("MODE: REPORT_CORRECTION_ONLY\n保存済みの作業報告を形式訂正します。実装の採否は後続の独立Reviewと親判断で決定します。\n今回の処理は報告形式の訂正だけです。Read/Grep/Bash/ログ再読/テスト再実行を行わず、直ちにOUTPUT_JSONと同じJSON objectをstructured outputとして返してください。\nOUTPUT_JSONは同じTask/session/sourceの保存済みGLM報告からrequirement_coverageだけを短縮し、既存validatorで機械検証済みです。他のfield、status、risk、tests、unverified、targets、artifactsを変更しないでください。SOURCE情報を出力objectへ加えないでください。独立Reviewと通常の品質ゲートはこの訂正後に行います。\nSOURCE_TASK: %s\nSOURCE_SESSION: %s\nSOURCE_CALL: %s\nSOURCE_RESPONSE_SHA256: %s\nVALIDATED_SUMMARY_UTF8_BYTES: %d\nVALIDATED_COVERAGE_UTF8_BYTES: %d\nOUTPUT_JSON:\n%s\n", input.TaskID, input.SessionID, input.SourceCallID, input.ResponseHash, len(input.Result.Summary), len(input.Result.RequirementCoverage), body), nil
}

func (w *Workflow) resultCorrectionResumeInput(checkpoint state.ResumeCheckpoint) (*correctionResumeInput, error) {
	if !correctionResumeCandidateCheckpoint(checkpoint) || !w.state.Exists(state.ResultCorrectionStateFile) {
		return nil, nil
	}
	if err := w.validateResultCorrectionBoundary(checkpoint); err != nil {
		return nil, err
	}
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		return nil, err
	}
	logs, err := w.state.ReadModelCallLogs(record.TaskID)
	if err != nil {
		return nil, err
	}
	return w.correctionResumeRecordInput(checkpoint, record, logs)
}

func correctionResumeCandidateCheckpoint(checkpoint state.ResumeCheckpoint) bool {
	return checkpoint.ResultCorrection && checkpoint.Role == state.WorkerRole && checkpoint.Stage == state.ResumeStageWorker
}

func (w *Workflow) correctionResumeRecordInput(checkpoint state.ResumeCheckpoint, record *resultCorrectionRecord, logs []state.ModelCallLog) (*correctionResumeInput, error) {
	for index := len(logs) - 1; index >= 0; index-- {
		call := logs[index]
		if !rejectedCoverageCallBinding(call, record, checkpoint) {
			continue
		}
		return w.correctionResumeCandidateInput(record, call)
	}
	return nil, fmt.Errorf("result correction has no matching rejected report")
}

func rejectedCoverageCallBinding(call state.ModelCallLog, record *resultCorrectionRecord, checkpoint state.ResumeCheckpoint) bool {
	basePhase := strings.TrimSuffix(checkpoint.Phase, resultCorrectionPhaseSuffix)
	return call.CallType == state.CallTypeTask && call.Outcome == rejectedReportCallOutcome &&
		call.TaskID == record.TaskID && call.SessionID == record.SessionID && call.Role == record.Role &&
		(call.Phase == checkpoint.Phase || call.Phase == basePhase)
}

func (w *Workflow) correctionResumeCandidateInput(record *resultCorrectionRecord, call state.ModelCallLog) (*correctionResumeInput, error) {
	if resultRecoveryDigest([]byte(call.Response)) != call.ResponseSHA256 {
		return nil, fmt.Errorf("rejected report response identity changed")
	}
	result, err := packet.ParseStructured([]byte(call.Response))
	if err != nil {
		return nil, err
	}
	if result.Status != packet.StatusImplemented || len(result.RequirementCoverage) <= packet.MaxFieldBytes {
		return nil, nil
	}
	if err := w.correctedCoverageWithinBudget(&result, record); err != nil {
		return nil, err
	}
	return &correctionResumeInput{TaskID: record.TaskID, SessionID: record.SessionID, SourceCallID: call.CallID, ResponseHash: call.ResponseSHA256, Result: result}, nil
}

func (w *Workflow) correctedCoverageWithinBudget(result *packet.Result, record *resultCorrectionRecord) error {
	result.RequirementCoverage = correctionCoverageExcerpt(result.RequirementCoverage)
	if err := packet.ValidateWorkerResult(*result); err != nil {
		return err
	}
	if err := packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(record.TaskID)); err != nil {
		return err
	}
	body, err := json.Marshal(*result)
	if err != nil {
		return err
	}
	if len(body) > packet.MaxPacketBytes {
		return fmt.Errorf("corrected report exceeds the packet byte budget")
	}
	return nil
}

func correctionCoverageExcerpt(value string) string {
	end := packet.MaxFieldBytes/4 - len("…")
	for !utf8.ValidString(value[:end]) {
		end--
	}
	return value[:end] + "…"
}
