package workflow

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func multilineSummaryImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "line one\nline two",
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func missingTestsImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "done",
		RequirementCoverage: "covered",
		Unverified:          "none",
	})
}

func baseCorrectionCheckpoint() state.ResumeCheckpoint {
	return state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original",
		OriginalPrompt: "original",
		Request:        "request",
	}
}

func TestRunModelUsesSecondCorrectionOnlyForNewViolation(t *testing.T) {
	st := newStateStoreT(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	sessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: multilineSummaryImplementedPacket()},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(baseCorrectionCheckpoint())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusImplemented {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.prompts) != 3 {
		t.Fatalf("model calls = %d want 3", len(r.prompts))
	}
	if r.phases[1] != "worker-new"+resultCorrectionPhaseSuffix || r.phases[2] != "worker-new"+resultCorrectionPhaseSuffix+resultCorrectionPhaseSuffix {
		t.Fatalf("correction phases = %#v", r.phases)
	}
	if !strings.Contains(r.prompts[2], "field summaryに改行") {
		t.Fatalf("2回目の補正promptに新規違反がありません: %s", r.prompts[2])
	}
	afterTaskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	afterSessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	if afterTaskID != taskID || afterSessionID != sessionID {
		t.Fatalf("correction changed lifecycle identity: task=%q/%q session=%q/%q", taskID, afterTaskID, sessionID, afterSessionID)
	}
	stats := currentStats(t, st)
	if stats.ModelCalls != 3 || stats.ResultCorrections != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if st.Exists(state.ResultCorrectionStateFile) {
		t.Fatal("成功後にresult correction stateが残っています")
	}
}

func TestRunModelStopsOnRepeatedFirstCorrectionViolation(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "repeated_violation" || failure.Attempts != 1 {
		t.Fatalf("repeated violation terminal failureを期待: err=%v failure=%#v", err, failure)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("同一違反へ追加補正を実行しています: calls=%d", len(r.prompts))
	}
	assertNoCorrectionRecoveryAction(t, st)
	assertTerminalCorrectionState(t, w, "repeated_violation")
}

func TestRunModelCorrectionBudgetExhaustionIsTerminal(t *testing.T) {
	tests := []struct {
		name  string
		third string
	}{
		{name: "second correction repeats violation", third: multilineSummaryImplementedPacket()},
		{name: "different violation remains after second correction", third: missingTestsImplementedPacket()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := newStateStoreT(t)
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			r := &scriptedRunner{steps: []runnerStep{
				{structured: constraintViolatingImplementedPacket()},
				{structured: multilineSummaryImplementedPacket()},
				{structured: tc.third},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err = w.runModel(baseCorrectionCheckpoint())
			failure, ok := ResultCorrectionFailureFromError(err)
			if !ok || failure.Reason != "budget_exhausted" || failure.Attempts != 2 {
				t.Fatalf("budget exhausted failureを期待: err=%v failure=%#v", err, failure)
			}
			if failure.TaskID != taskID || failure.SessionID == "" || failure.Snapshot.Head == "" || len(failure.Violations) < 2 {
				t.Fatalf("terminal evidenceが不足しています: %#v", failure)
			}
			if len(r.prompts) != 3 {
				t.Fatalf("correction budgetを超えてmodel callしています: calls=%d", len(r.prompts))
			}
			if st.TaskStatus() != state.TaskStatusActive {
				t.Fatalf("terminal failure後status = %s", st.TaskStatus())
			}
			assertNoCorrectionRecoveryAction(t, st)
			assertTerminalCorrectionState(t, w, "budget_exhausted")
		})
	}
}

func TestRunModelCorrectionFailsClosedOnSnapshotChange(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	captures := 0
	w.captureSnapshot = func(string) (state.GitSnapshot, error) {
		captures++
		if captures >= 3 {
			changed := fixedSnapshot
			changed.WorktreeDigest = "changed-worktree"
			return changed, nil
		}
		return fixedSnapshot, nil
	}

	_, err := w.runModel(baseCorrectionCheckpoint())
	failure, ok := ResultCorrectionFailureFromError(err)
	if !ok || failure.Reason != "boundary_changed" || failure.BoundaryMismatch != "repository snapshot changed" {
		t.Fatalf("snapshot boundary failureを期待: err=%v failure=%#v", err, failure)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("snapshot変更後に追加model callしています: calls=%d", len(r.prompts))
	}
	assertNoCorrectionRecoveryAction(t, st)
	assertTerminalCorrectionState(t, w, "boundary_changed")
}

func TestTerminalCorrectionStateBlocksFreshModelAdmission(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacket("must-not-run")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()
	if _, err := w.runModel(baseCorrectionCheckpoint()); err == nil {
		t.Fatal("terminal correction failureを期待")
	}
	if _, err := w.runModel(baseCorrectionCheckpoint()); err == nil {
		t.Fatal("terminal correction stateがfresh model callを許可しました")
	}
	if len(r.prompts) != 2 {
		t.Fatalf("terminal latch後にmodel callが実行されました: %d", len(r.prompts))
	}
}

func assertTerminalCorrectionState(t *testing.T, w *Workflow, reason string) {
	t.Helper()
	record, err := w.loadResultCorrectionRecord()
	if err != nil {
		t.Fatal(err)
	}
	if record.Terminal == nil || record.Terminal.Reason != reason {
		t.Fatalf("terminal correction state = %#v", record)
	}
}

func assertNoCorrectionRecoveryAction(t *testing.T, st *state.StateStore) {
	t.Helper()
	if _, loadErr := st.LoadResumeCheckpoint(); !errors.Is(loadErr, state.ErrNoResumeCheckpoint) {
		t.Fatalf("terminal correctionがresume checkpointを残しています: %v", loadErr)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionNone || plan.Allows(state.ParentActionResume) {
		t.Fatalf("terminal correctionがparent recovery actionを要求しています: %#v", plan)
	}
}

func constraintViolatingImplementedPacket() string {
	return packetBody(packet.Result{
		Status:     packet.StatusImplemented,
		Risk:       packet.RiskLow,
		Summary:    "done",
		Tests:      "pass",
		Unverified: "none",
	})
}

func oversizeImplementedPacket() string {
	return packetBody(packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             strings.Repeat("x", packet.MaxFieldBytes+1),
		RequirementCoverage: "covered",
		Tests:               "pass",
		Unverified:          "none",
	})
}

func TestBoundedTextKeepsSingleLineFieldContract(t *testing.T) {
	omissionMarker := "[前方を省略] "
	withinLimit := "そのまま返す観測値"
	if got := boundedText(withinLimit, packet.MaxFieldBytes); got != withinLimit {
		t.Fatalf("上限内の値は変更しない: %q", got)
	}
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"ascii超過", strings.Repeat("x", packet.MaxFieldBytes+40)},
		{"multibyte超過", strings.Repeat("あ", packet.MaxFieldBytes/3+13)},
		{"切詰め境界がrune途中のmultibyte超過", "x" + strings.Repeat("あ", packet.MaxFieldBytes/3+13)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := boundedText(tc.value, packet.MaxFieldBytes)
			if len(got) > packet.MaxFieldBytes {
				t.Fatalf("出力がbyte上限を超えています: %d bytes", len(got))
			}
			if !strings.HasPrefix(got, omissionMarker) {
				t.Fatalf("切詰め時に省略markerを先頭に置く: %q", got)
			}
			if strings.ContainsAny(got, "\n\r") {
				t.Fatalf("切詰め出力に改行を含めている: %q", got)
			}
			if !utf8.ValidString(got) {
				t.Fatal("切詰め出力が不正UTF-8です")
			}
			if !strings.HasSuffix(tc.value, got[len(omissionMarker):]) {
				t.Fatal("切詰め出力は元の値の末尾を保持すべきです")
			}
		})
	}
}

func TestRunModelCorrectsInvalidResultInSameRunner(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: oversizeImplementedPacket()},
		{structured: implementedPacket("implemented")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusImplemented {
		t.Fatalf("status = %q", result.Status)
	}
	if len(r.prompts) != 2 || !strings.Contains(r.prompts[1], "意味検証に不合格") {
		t.Fatalf("same runnerで修正再依頼されていません: %#v", r.prompts)
	}
	if r.phases[1] != "worker-new"+resultCorrectionPhaseSuffix {
		t.Fatalf("修正再依頼phase = %q", r.phases[1])
	}

	stats := currentStats(t, st)
	if stats.ModelCalls != 2 || stats.ResultCorrections != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "size" || logs[1].Outcome != "success" {
		t.Fatalf("result correction telemetry = %#v", logs)
	}
	if logs[0].RetryOf != "" || logs[0].RetryReason != "" {
		t.Fatalf("最初の失敗callに再試行因果があってはいません: %#v", logs[0])
	}
	if logs[1].RetryOf != logs[0].CallID || logs[1].RetryReason != "invalid-packet-result-correction" {
		t.Fatalf("修正再実行の因果 = retry_of=%q reason=%q want call %q", logs[1].RetryOf, logs[1].RetryReason, logs[0].CallID)
	}
}

func TestRunModelFailsClosedOnStructuredMismatch(t *testing.T) {
	tests := []struct {
		name     string
		role     state.SessionRole
		stage    state.ResumeStage
		readOnly bool
		phase    string
	}{
		{name: "worker", role: state.WorkerRole, stage: state.ResumeStageWorker, phase: "worker-new"},
		{name: "reviewer", role: state.ReviewerRole, stage: state.ResumeStageReview, readOnly: true, phase: "reviewer-1"},
	}
	var workerErr *WorkerError
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{
				{structured: duplicatedImplementedPacket()},
				{structured: implementedPacket("unreachable")},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()

			_, err := w.runModel(state.ResumeCheckpoint{
				Stage:    test.stage,
				Phase:    test.phase,
				Role:     test.role,
				ReadOnly: test.readOnly,
				Model:    "opus",
				Effort:   "high",
				Prompt:   "original",
				Request:  "request",
			})
			if err == nil || !errors.As(err, &workerErr) {
				t.Fatalf("mismatch fail closedを期待: %v", err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("mismatchで修正再依頼は実行しない: calls=%d", len(r.prompts))
			}
			taskID, _ := st.TaskID()
			logs, logErr := st.ReadModelCallLogs(taskID)
			if logErr != nil {
				t.Fatal(logErr)
			}
			if len(logs) != 1 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "schema-mismatch" {
				t.Fatalf("structured mismatch telemetry = %#v", logs)
			}
			if !strings.Contains(logs[0].Error, "structured_output") {
				t.Fatalf("拒否理由がtelemetryへ記録されていません: %q", logs[0].Error)
			}
			stats := currentStats(t, st)
			if stats.ResultCorrections != 0 || stats.PacketRejectByCategory["schema-mismatch"] != 1 {
				t.Fatalf("stats = %#v", stats)
			}
		})
	}
}

func TestRunModelFailsClosedOnStructuredOutputError(t *testing.T) {
	cases := []struct {
		name             string
		runErr           error
		want             string
		wantRetryMetrics int
	}{
		{"retry exhausted", &runner.StructuredOutputError{Subtype: "error_max_structured_output_retries", TerminalReason: "gave up after 3 attempts"}, "error_max_structured_output_retries", 1},
		{"missing on success", &runner.StructuredOutputError{}, "result eventにstructured_outputがありません", 0},
	}
	var workerErr *WorkerError
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{
				{output: "", runErr: c.runErr},
				{structured: implementedPacket("unreachable")},
			}}
			w := newWorkflowT(t, st, r)
			w.temp = t.TempDir()
			w.config.RepoRoot = "/repo"
			w.config.RepoShort = "testrepo1234"

			_, err := w.runModel(state.ResumeCheckpoint{
				Stage:   state.ResumeStageWorker,
				Phase:   "worker-new",
				Role:    state.WorkerRole,
				Model:   "opus",
				Effort:  "high",
				Prompt:  "p",
				Request: "req",
			})
			if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("structured output fail closedを期待: %v", err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("transient retryにも修正再依頼にも入らない: calls=%d", len(r.prompts))
			}
			taskID, _ := st.TaskID()
			logs, logErr := st.ReadModelCallLogs(taskID)
			if logErr != nil {
				t.Fatal(logErr)
			}
			if len(logs) != 1 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "structured-output" {
				t.Fatalf("structured output telemetry = %#v", logs)
			}
			if _, cpErr := st.LoadResumeCheckpoint(); cpErr == nil {
				t.Fatal("resume checkpointが残っています")
			}
			stats := currentStats(t, st)

			if stats.StructuredRetryExhausted != c.wantRetryMetrics || stats.ResultCorrections != 0 {
				t.Fatalf("stats = %#v", stats)
			}
		})
	}
}

func TestExecuteEmitsAcceptedResultExactlyOnce(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: implementedPacketWithRisk("done", "HIGH")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	buf := &bytes.Buffer{}
	w.output = buf

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(buf.String(), `"status":"`); got != 1 {
		t.Fatalf("受理結果の出力回数 = %d\n%s", got, buf.String())
	}
	if strings.Contains(buf.String(), `"status":"PASS"`) || strings.Contains(buf.String(), `"summary":"done"`) {
		t.Fatalf("旧応答が最終stdoutへ混入しています: %s", buf.String())
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
	if len(r.prompts) != 4 {
		t.Fatalf("修正再依頼・再出力は各1回だけ: calls=%d", len(r.prompts))
	}
	if strings.Join(r.models, ",") != "opus,opus,sonnet,sonnet" {
		t.Fatalf("models = %#v", r.models)
	}
}

func TestEmitResultRecordsEmittedPayloadBytes(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}}
	var out bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &out)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	emitted := strings.TrimRight(out.String(), "\n")
	if !strings.Contains(emitted, `"status":"PASS"`) {
		t.Fatalf("受理結果がstdoutへ出ていません: %s", out.String())
	}
	if stats := currentStats(t, st); stats.SolPacketBytes != len(emitted) {
		t.Fatalf("SolPacketBytes = %d want %d(stdout payload bytes):\n%s", stats.SolPacketBytes, len(emitted), out.String())
	}
}

func TestRunModelStopsAfterRepeatedConstraintViolations(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{structured: constraintViolatingImplementedPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	var workerErr *WorkerError
	if err == nil || !errors.As(err, &workerErr) || !strings.Contains(err.Error(), "必須field requirement_coverage") {
		t.Fatalf("修正再依頼後の不合格停止を期待: %v", err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("修正再依頼は1回だけ実施する: calls=%d", len(r.prompts))
	}
}

func TestRunModelPreservesResultCorrectionAcrossRateLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: constraintViolatingImplementedPacket()},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original implementation prompt",
		OriginalPrompt: "original implementation prompt",
		Request:        "request",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("修正再依頼中のrate limit errorを期待: %v", err)
	}

	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !checkpoint.ResultCorrection || !strings.Contains(checkpoint.OriginalPrompt, "意味検証に不合格") {
		t.Fatalf("修正再依頼promptがcheckpointに保持されていません: %#v", checkpoint)
	}
}

func TestRunModelCorrectsArtifactOutsideTaskDir(t *testing.T) {
	st := newStateStoreT(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithArtifacts("invalid artifact", outside)},
		{structured: implementedPacket("corrected")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(state.ResumeCheckpoint{
		Stage:   state.ResumeStageWorker,
		Phase:   "worker-new",
		Role:    state.WorkerRole,
		Model:   "opus",
		Effort:  "high",
		Prompt:  "original",
		Request: "request",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Artifacts) != 0 || len(r.prompts) != 2 {
		t.Fatalf("artifact pathが修正されていません: result=%#v prompts=%d", result, len(r.prompts))
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "artifacts" || !strings.Contains(logs[0].Error, "artifact dir配下") {
		t.Fatalf("artifact validation telemetry = %#v", logs)
	}
}

func TestRunModelPreservesResultCorrectionPromptAcrossRateLimit(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: oversizeImplementedPacket()},
		{output: zaiFiveHourLog, runErr: errors.New("exit status 1")},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "original implementation prompt",
		OriginalPrompt: "original implementation prompt",
		Request:        "request",
	})
	var limitErr runner.ZaiRateLimitError
	if err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("修正再依頼中のrate limit errorを期待: %v", err)
	}

	checkpoint, loadErr := st.LoadResumeCheckpoint()
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if !checkpoint.ResultCorrection || !strings.Contains(checkpoint.OriginalPrompt, "意味検証に不合格") {
		t.Fatalf("修正再依頼promptがcheckpointに保持されていません: %#v", checkpoint)
	}
	resumed := resumePrompt(checkpoint)
	if !strings.Contains(resumed, "意味検証に不合格") || strings.Contains(resumed, "original implementation prompt") {
		t.Fatalf("resumeが修正再依頼工程を指していません: %s", resumed)
	}
}

func TestRetryPromptsDelegatePacketGrammar(t *testing.T) {
	correction := resultCorrectionPrompt("validator detail")
	for _, want := range []string{
		"意味検証に不合格",
		"作業・調査・テストをやり直さず",
		"structured schemaとvalidatorを正として",
		"validator detail",
		rejectedArtifactMarker,
	} {
		if !strings.Contains(correction, want) {
			t.Fatalf("correction promptに%qがありません: %s", want, correction)
		}
	}
	for _, forbidden := range []string{
		"6 KiB",
		"1536 bytes",
		"各fieldのvalueは空にできず",
		"STATUSに応じた必須field",
		"--packet-check",
	} {
		if strings.Contains(correction, forbidden) {
			t.Fatalf("correction promptがpacket grammar %qを再定義しています: %s", forbidden, correction)
		}
	}

	reemit := riskFloorReemitPrompt()
	for _, want := range []string{
		"HIGH RISK最終確認が必要",
		"wrapper risk floor",
		"実装・調査・テストをやり直さず",
		"structured schemaに従って結果だけを再出力",
	} {
		if !strings.Contains(reemit, want) {
			t.Fatalf("reemit promptに%qがありません: %s", want, reemit)
		}
	}
	for _, forbidden := range []string{
		"NEEDS_SOL_REVIEW",
		"RISK: HIGH",
		"TARGETS",
		"SUMMARY",
		"REQUIREMENT_COVERAGE",
		"SOL_QUESTION",
	} {
		if strings.Contains(reemit, forbidden) {
			t.Fatalf("reemit promptがpacket grammar %qを再定義しています: %s", forbidden, reemit)
		}
	}
}

func TestFixRequiredTargetsPacketDispatchesReportOnlyPrompt(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixRequiredPacketWithTargets("PACKET")},
		{structured: implementedPacketWithRisk("report re-emitted", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if got, want := strings.Join(r.models, ","), "opus,sonnet,opus,sonnet"; got != want {
		t.Fatalf("model routing = %q want %q", got, want)
	}
	if len(r.prompts) != 4 {
		t.Fatalf("prompt count = %d want 4", len(r.prompts))
	}
	reportOnly := r.prompts[2]
	for _, want := range []string{
		"報告だけを再出力",
		"実装・working tree変更・追加調査・test/lint/build/self-reviewをやり直さず",
	} {
		if !strings.Contains(reportOnly, want) {
			t.Fatalf("report-only promptに%qがありません: %s", want, reportOnly)
		}
	}
	for _, forbidden := range []string{
		"独立reviewerの指摘を修正してください",
		"修正後に必要なテスト・lint・build・自己レビューまで行ってください",
	} {
		if strings.Contains(reportOnly, forbidden) {
			t.Fatalf("report-only promptにimplementation fix文言%qが入っています: %s", forbidden, reportOnly)
		}
	}
	var reportOnlyPhases []string
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			reportOnlyPhases = append(reportOnlyPhases, l.Phase)
		}
	}
	if !slices.Contains(reportOnlyPhases, "worker-report-only-1") {
		t.Fatalf("telemetryへreport-only phaseが識別できません: %v", reportOnlyPhases)
	}
}

func TestFixRequiredOtherTargetsKeepsImplementationAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	implementation := r.prompts[2]
	for _, want := range []string{
		"独立reviewerの指摘を修正してください",
		"修正後に必要なテスト・lint・build・自己レビューまで行ってください",
	} {
		if !strings.Contains(implementation, want) {
			t.Fatalf("implementation auto-fix promptから%qが失われています: %s", want, implementation)
		}
	}
	if strings.Contains(implementation, "報告だけを再出力") {
		t.Fatalf("通常FIX_REQUIREDへreport-only promptが使われています: %s", implementation)
	}
	var phases []string
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			phases = append(phases, l.Phase)
		}
	}
	if !slices.Contains(phases, "worker-auto-fix-1") {
		t.Fatalf("telemetryへimplementation auto-fix phaseがありません: %v", phases)
	}
}

func TestFixRequiredWithoutTargetsCorrectsBeforeAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	fixWithoutTargets := `{"status":"FIX_REQUIRED","risk":"HIGH","summary":"fix","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":[],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixWithoutTargets},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix, "worker-auto-fix-1", "reviewer-2-high-floor"}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[2])
	}
	autoFix := r.prompts[3]
	if !strings.Contains(autoFix, `"targets":["glm-worker/internal/state/store.go:Read"]`) {
		t.Fatalf("auto-fix promptへ修正対象targetsが伝わっていません: %s", autoFix)
	}
	for i, prompt := range r.prompts {
		if i != 3 && strings.Contains(prompt, "MODE: APPLY_REVIEW_FIX") {
			t.Fatalf("targets未確定のFIX_REQUIREDがauto-fixへdispatchしています: prompt %d", i)
		}
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 5 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	stats := currentStats(t, st)
	if stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}

func TestNeedsSolDecisionWithoutTargetsCorrectsBeforeParentDispatch(t *testing.T) {
	st := newStateStoreT(t)
	decisionWithoutTargets := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":[],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: decisionWithoutTargets},
		{structured: needsSolDecisionPacket()},
	}}
	var emitted bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &emitted)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-new" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[1], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[1])
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("decision待ち状態へ遷移していません: status=%q pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if !strings.Contains(emitted.String(), `"status":"NEEDS_SOL_DECISION"`) || !strings.Contains(emitted.String(), `"targets":["t"]`) {
		t.Fatalf("親境界の結果packetへ修正後targetsが伝わっていません: %s", emitted.String())
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}

func TestFixRequiredBlankTargetsElementCorrectsBeforeAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	fixWithBlankElement := `{"status":"FIX_REQUIRED","risk":"HIGH","summary":"fix","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["   "],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: fixWithBlankElement},
		{structured: fixRequiredPacketWithTargets("glm-worker/internal/state/store.go:Read")},
		{structured: implementedPacketWithRisk("fixed implementation", "HIGH")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix, "worker-auto-fix-1", "reviewer-2-high-floor"}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") || !strings.Contains(r.prompts[2], "TARGETSの要素は空・空白のみ") {
		t.Fatalf("修正再依頼promptへ要素違反が伝わっていません: %s", r.prompts[2])
	}
	autoFix := r.prompts[3]
	if !strings.Contains(autoFix, `"targets":["glm-worker/internal/state/store.go:Read"]`) {
		t.Fatalf("auto-fix promptへ修正対象targetsが伝わっていません: %s", autoFix)
	}
	for i, prompt := range r.prompts {
		if i != 3 && strings.Contains(prompt, "MODE: APPLY_REVIEW_FIX") {
			t.Fatalf("要素違反FIX_REQUIREDがauto-fixへdispatchしています: prompt %d", i)
		}
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 5 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
}

func TestNeedsSolDecisionMixedNoneTargetsCorrectsBeforeParentDispatch(t *testing.T) {
	st := newStateStoreT(t)
	decisionMixedNone := `{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"d","evidence":"e","options":"o","recommendation":"r","test_obligations":"t","targets":["none","glm-worker/internal/packet/validate.go:validateTargets"],"artifacts":[]}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: decisionMixedNone},
		{structured: needsSolDecisionPacket()},
	}}
	var emitted bytes.Buffer
	w := newWorkflowTWithOutput(t, st, r, &emitted)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "worker-new" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[1], "意味検証に不合格") || !strings.Contains(r.prompts[1], "混在できません") {
		t.Fatalf("修正再依頼promptへ混在違反が伝わっていません: %s", r.prompts[1])
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("decision待ち状態へ遷移していません: status=%q pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	if !strings.Contains(emitted.String(), `"status":"NEEDS_SOL_DECISION"`) || !strings.Contains(emitted.String(), `"targets":["t"]`) {
		t.Fatalf("親境界の結果packetへ修正後targetsが伝わっていません: %s", emitted.String())
	}
	if strings.Contains(emitted.String(), `"none"`) {
		t.Fatalf("none混在targetsが親境界へ流出しています: %s", emitted.String())
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 2 || logs[0].Outcome != "invalid_packet" || logs[0].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
}

func TestNeedsSolReviewNoneElementCorrectsBeforeSolReviewDispatch(t *testing.T) {
	st := newStateStoreT(t)
	reviewMixedNone := `{"status":"NEEDS_SOL_REVIEW","risk":"HIGH","summary":"review","requirement_coverage":"covered","invariants":"preserved","test_evidence":"ev","issues":"i","residual_risk":"r","targets":["none","glm-worker/internal/packet/validate.go:validateTargets"],"artifacts":[],"sol_question":"q"}`
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacketWithRisk("high risk work", "HIGH")},
		{structured: reviewMixedNone},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	wantPhases := []string{"worker-new", "reviewer-1-high-floor", "reviewer-1-high-floor" + resultCorrectionPhaseSuffix}
	if strings.Join(r.phases, ",") != strings.Join(wantPhases, ",") {
		t.Fatalf("phases = %v, want %v", r.phases, wantPhases)
	}
	if !strings.Contains(r.prompts[2], "意味検証に不合格") {
		t.Fatalf("修正再依頼promptが選ばれていません: %s", r.prompts[2])
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	review := st.ReadOr("last-review", "")
	if !strings.Contains(review, `"targets":["glm-worker/internal/workflow/workflow_test.go:needsSolReviewPacket"]`) || strings.Contains(review, "none") {
		t.Fatalf("Sol境界のreview packetへ正規targetsだけが伝わっていません: %s", review)
	}
	taskID, _ := st.TaskID()
	logs, logErr := st.ReadModelCallLogs(taskID)
	if logErr != nil {
		t.Fatal(logErr)
	}
	if len(logs) != 3 || logs[1].Outcome != "invalid_packet" || logs[1].PacketRejectReason != "targets-none" {
		t.Fatalf("telemetry = %#v", logs)
	}
	if stats := currentStats(t, st); stats.ResultCorrections != 1 {
		t.Fatalf("result corrections = %d, stats = %#v", stats.ResultCorrections, stats)
	}
}
