package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathtrial"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type trialShadowRunner struct {
	scriptedRunner
	shadowOutput string
	shadowErr    error
	shadowCalls  int
	shadowPhase  string
	shadowDeadln time.Time
}

const trialShadowFindings = `{"findings":[{"target":"glm-worker/internal/runner/probe.go:90","class":"external-model-invocation","issue":"deadlineなし呼出","evidence":"hunk"}],"summary":"s"}`

func (r *trialShadowRunner) RunWithDeadline(
	_ state.SessionRole,
	phase string,
	_ string,
	readOnly bool,
	_ string,
	_ string,
	_ string,
	deadline time.Time,
) (runner.RunResult, error) {
	r.shadowCalls++
	r.shadowPhase = phase
	r.shadowDeadln = deadline
	if deadline.IsZero() {
		return runner.RunResult{}, errors.New("deadlineが空です")
	}
	if !readOnly {
		return runner.RunResult{}, errors.New("shadow呼出がreadOnlyではありません")
	}
	if r.shadowErr != nil {
		return runner.RunResult{}, r.shadowErr
	}
	result := runner.RunResult{
		SessionID:        "trial-session",
		CallID:           "trial-call",
		StructuredOutput: json.RawMessage(r.shadowOutput),
		Response:         r.shadowOutput,
		TopLevelUsage:    runner.TokenUsage{InputTokens: 120, OutputTokens: 30},
		TotalCostUSD:     0.02,
		DurationAPIMS:    1500,
	}
	return result, nil
}

func newFailurePathTrialWorkflow(t *testing.T, shadow *trialShadowRunner, changedPath string) (*Workflow, *state.StateStore) {
	t.Helper()
	st := newStateStoreT(t)
	shadow.scriptedRunner = scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, &shadow.scriptedRunner)
	w.runner = shadow
	w.config.FailurePathTrial = true
	if changedPath != "" {
		writeTrialRepoFile(t, w.config.RepoRoot, changedPath)
		w.collectChangedPaths = func(string, string) ([]string, error) {
			return []string{changedPath}, nil
		}
	}
	return w, st
}

func writeTrialRepoFile(t *testing.T, repoRoot, path string) {
	t.Helper()
	full := filepath.Join(repoRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package runner\n\nvar site = newProcessGroupCmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadTrialRegistryT(t *testing.T, st *state.StateStore) failurepathtrial.Registry {
	t.Helper()
	registry, err := failurepathtrial.LoadRegistry(st.Path(failurepathtrial.RegistryFile))
	if err != nil {
		t.Fatalf("trial registry読み取り: %v", err)
	}
	return registry
}

func TestFailurePathTrialObservedRecordAndTelemetry(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("canonical review結果が変わっています: %s", st.TaskStatus())
	}
	if shadow.shadowCalls != 1 {
		t.Fatalf("shadow呼出数 = %d want 1", shadow.shadowCalls)
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 {
		t.Fatalf("records = %+v", registry.Records)
	}
	record := registry.Records[0]
	if record.Outcome != failurepathtrial.OutcomeObserved {
		t.Fatalf("outcome = %s detail = %s", record.Outcome, record.Detail)
	}
	if len(record.Findings) != 1 || record.Findings[0].Class != failurepathtrial.ClassExternalModelInvocation {
		t.Fatalf("findings = %+v", record.Findings)
	}
	if record.AddedGLM == nil || record.AddedGLM.InputTokens != 120 || record.AddedGLM.Calls != 1 {
		t.Fatalf("added GLM = %+v", record.AddedGLM)
	}
	if record.ReviewPacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("review packet status = %q", record.ReviewPacketStatus)
	}
	if record.CallID != "trial-call" {
		t.Fatalf("call id = %q", record.CallID)
	}
	logs, err := st.ReadModelCallLogs(st.ReadOr("task.id", ""))
	if err != nil {
		t.Fatal(err)
	}
	var trialEntries int
	for _, log := range logs {
		if log.Role == state.FailurePathReviewerRole {
			trialEntries++
			if log.Phase != "failure-path-reviewer-1" || log.Outcome != failurepathtrial.OutcomeObserved {
				t.Fatalf("trial telemetry = %s %s", log.Phase, log.Outcome)
			}
		}
	}
	if trialEntries != 1 {
		t.Fatalf("trial telemetry entries = %d want 1", trialEntries)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.FailurePathReviewerCalls != 1 || stats.WorkerCalls != 1 || stats.ReviewerCalls != 1 {
		t.Fatalf("role別call会計 = worker:%d reviewer:%d trial:%d", stats.WorkerCalls, stats.ReviewerCalls, stats.FailurePathReviewerCalls)
	}
}

func TestFailurePathTrialDisabledByKillSwitch(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	w.config.FailurePathTrial = false

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if shadow.shadowCalls != 0 {
		t.Fatalf("kill switch後にshadow呼出がありました: %d", shadow.shadowCalls)
	}
	if st.Exists(failurepathtrial.RegistryFile) {
		t.Fatal("kill switch後にregistryが書かれました")
	}
}

func TestFailurePathTrialDeadlineFailureRecordsMissing(t *testing.T) {
	shadow := &trialShadowRunner{shadowErr: runner.ErrProbeDeadlineExceeded}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("欠測でcanonical flowが止まっています: %s", st.TaskStatus())
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathtrial.OutcomeMissingDeadline {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.CohortSize() != 1 {
		t.Fatalf("欠測がcohortに保持されていません: %d", registry.CohortSize())
	}
}

func TestFailurePathTrialSchemaInvalidOutputRecordsMissing(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: `{"findings":[],"summary":"s","extra":true}`}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathtrial.OutcomeMissingSchemaInvalid {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.Records[0].Detail == "" {
		t.Fatal("schema違反のdetailが記録されていません")
	}
}

func TestFailurePathTrialAmbiguousPathOnlyChangeRecordsWithoutRun(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/workflow/labels.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if shadow.shadowCalls != 0 {
		t.Fatalf("ambiguous変更でshadow呼出がありました: %d", shadow.shadowCalls)
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathtrial.OutcomeAmbiguous {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.CohortSize() != 0 {
		t.Fatalf("ambiguousがcohortに数えられています: %d", registry.CohortSize())
	}
}

func TestFailurePathTrialCohortCapStopsNewRuns(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	registry := failurepathtrial.Registry{}
	for index := 0; index < failurepathtrial.CohortCap; index++ {
		registry = registry.WithRecord(failurepathtrial.Record{
			TaskID:  fmt.Sprintf("prior-%02d", index),
			Outcome: failurepathtrial.OutcomeObserved,
		})
	}
	if err := failurepathtrial.SaveRegistry(st.Path(failurepathtrial.RegistryFile), registry); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if shadow.shadowCalls != 0 {
		t.Fatalf("上限到達後にshadow呼出がありました: %d", shadow.shadowCalls)
	}
	loaded := loadTrialRegistryT(t, st)
	if len(loaded.Records) != failurepathtrial.CohortCap+1 {
		t.Fatalf("records = %d", len(loaded.Records))
	}
	if loaded.Records[len(loaded.Records)-1].Outcome != failurepathtrial.OutcomeCapped {
		t.Fatalf("最終record = %+v", loaded.Records[len(loaded.Records)-1])
	}
}

func TestFailurePathTrialRunsOncePerTask(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	shadow.steps = []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if shadow.shadowCalls != 1 {
		t.Fatalf("同一taskでshadow呼出が複数回: %d", shadow.shadowCalls)
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 {
		t.Fatalf("records = %+v", registry.Records)
	}
}

func TestFailurePathTrialClassificationFailureRecordsMissing(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, errors.New("changed paths unavailable")
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if shadow.shadowCalls != 0 {
		t.Fatalf("分類失敗時にshadow呼出がありました: %d", shadow.shadowCalls)
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathtrial.OutcomeClassificationMissing {
		t.Fatalf("records = %+v", registry.Records)
	}
	if !strings.Contains(registry.Records[0].Detail, "changed paths unavailable") {
		t.Fatalf("detail = %q", registry.Records[0].Detail)
	}
}

func TestFailurePathTrialWithoutTrialRunnerRecordsMissing(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.config.FailurePathTrial = true
	writeTrialRepoFile(t, w.config.RepoRoot, "glm-worker/internal/runner/probe_extra.go")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/runner/probe_extra.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("canonical review結果が変わっています: %s", st.TaskStatus())
	}
	registry := loadTrialRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathtrial.OutcomeMissingRunUnavailable {
		t.Fatalf("records = %+v", registry.Records)
	}
}

func trialMeasurementFailures(t *testing.T, st *state.StateStore) []state.ModelCallLog {
	t.Helper()
	logs, err := st.ReadModelCallLogs(st.ReadOr("task.id", ""))
	if err != nil {
		t.Fatal(err)
	}
	var failures []state.ModelCallLog
	for _, log := range logs {
		if log.Role == state.FailurePathReviewerRole && log.Outcome == failurePathMeasurementFailedOutcome {
			failures = append(failures, log)
		}
	}
	return failures
}

func TestFailurePathTrialCorruptRegistryRecordsMeasurementFailure(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathtrial.RegistryFile)
	corrupt := []byte(`{"schema":`)
	if err := os.WriteFile(registryPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("canonical review結果が変わっています: %s", st.TaskStatus())
	}
	if shadow.shadowCalls != 0 {
		t.Fatalf("corrupt registryでshadow呼出がありました: %d", shadow.shadowCalls)
	}
	failures := trialMeasurementFailures(t, st)
	if len(failures) != 1 || !strings.Contains(failures[0].Error, "registry-load") {
		t.Fatalf("measurement failure = %+v", failures)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatalf("corrupt registryが空registryへ書き換えられました: %q", after)
	}
}

func TestFailurePathTrialSaveFailureRecordsMeasurementFailure(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathtrial.RegistryFile)
	if err := failurepathtrial.SaveRegistry(registryPath, failurepathtrial.Registry{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(registryPath+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("保存失敗でcanonical flowが止まっています: %s", st.TaskStatus())
	}
	if shadow.shadowCalls != 1 {
		t.Fatalf("shadow呼出数 = %d want 1", shadow.shadowCalls)
	}
	failures := trialMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"registry-save", "outcome=observed", "call_id=trial-call"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadTrialRegistryT(t, st)
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) || registry.CohortSize() != 0 {
		t.Fatalf("保存失敗recordがregistryへ算入されています: %+v", registry.Records)
	}
}

func TestFailurePathTrialLockHeldDuringAppendContinuesCanonical(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathtrial.RegistryFile)
	if err := failurepathtrial.SaveRegistry(registryPath, failurepathtrial.Registry{}); err != nil {
		t.Fatal(err)
	}
	lock, err := repolock.Acquire(registryPath + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("lock競合でcanonical flowが止まっています: %s", st.TaskStatus())
	}
	if shadow.shadowCalls != 1 {
		t.Fatalf("shadow呼出数 = %d want 1", shadow.shadowCalls)
	}
	failures := trialMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"registry-save", "lock競合", "outcome=observed"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadTrialRegistryT(t, st)
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) || registry.CohortSize() != 0 {
		t.Fatalf("lock競合recordがregistryへ算入されています: %+v", registry.Records)
	}
}

func TestFailurePathTrialCapRaceRecordNotCounted(t *testing.T) {
	shadow := &trialShadowRunner{shadowOutput: trialShadowFindings}
	w, st := newFailurePathTrialWorkflow(t, shadow, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathtrial.RegistryFile)
	full := failurepathtrial.Registry{}
	for index := 0; index < failurepathtrial.CohortCap; index++ {
		full = full.WithRecord(failurepathtrial.Record{
			TaskID:  fmt.Sprintf("prior-%02d", index),
			Outcome: failurepathtrial.OutcomeObserved,
		})
	}
	if err := failurepathtrial.SaveRegistry(registryPath, full); err != nil {
		t.Fatal(err)
	}

	trigger := failurepathtrial.TriggerDecision{
		Triggered:    true,
		Classes:      []string{failurepathtrial.ClassExternalModelInvocation},
		TriggerPaths: []string{"glm-worker/internal/runner/probe_extra.go"},
	}
	record := w.newFailurePathTrialRecord(
		st.ReadOr("task.id", ""), 1, "failure-path-reviewer-1", trigger,
		failurepathtrial.OutcomeObserved, nil, "trial-call", "",
	)
	w.appendFailurePathTrialRecord(registryPath, record)

	failures := trialMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"cohort-cap", "outcome=observed", "call_id=trial-call"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadTrialRegistryT(t, st)
	if registry.CohortSize() != failurepathtrial.CohortCap {
		t.Fatalf("cohort = %d want %d", registry.CohortSize(), failurepathtrial.CohortCap)
	}
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) {
		t.Fatalf("cap競合recordがregistryへ算入されています: %+v", registry.Records)
	}
}
