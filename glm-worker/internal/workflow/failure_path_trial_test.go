package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type advisoryScriptedRunner struct {
	scriptedRunner
	advisoryOutput   string
	advisoryErr      error
	advisoryCalls    int
	advisoryPrompt   string
	advisoryDeadline time.Time
}

const advisoryFindingsJSON = `{"findings":[
	{"target":"glm-worker/internal/runner/probe.go:90","class":"external-model-invocation","issue":"deadlineなし呼出","evidence":"hunk","status":"finding"},
	{"target":"glm-worker/internal/state/stats.go:9","class":"metric-reduction-accounting","issue":"欠測の会計位置を確認できず","status":"indeterminate"}
],"summary":"s"}`

func (r *advisoryScriptedRunner) RunWithDeadline(
	_ state.SessionRole,
	_ string,
	_ string,
	readOnly bool,
	_ string,
	prompt string,
	_ string,
	deadline time.Time,
) (runner.RunResult, error) {
	r.advisoryCalls++
	r.advisoryPrompt = prompt
	r.advisoryDeadline = deadline
	if deadline.IsZero() {
		return runner.RunResult{}, errors.New("deadlineが空です")
	}
	if !readOnly {
		return runner.RunResult{}, errors.New("advisory呼出がreadOnlyではありません")
	}
	if r.advisoryErr != nil {
		return runner.RunResult{}, r.advisoryErr
	}
	result := runner.RunResult{
		SessionID:        "advisory-session",
		CallID:           "advisory-call",
		StructuredOutput: json.RawMessage(r.advisoryOutput),
		Response:         r.advisoryOutput,
		TopLevelUsage:    runner.TokenUsage{InputTokens: 120, OutputTokens: 30},
		TotalCostUSD:     0.02,
		DurationAPIMS:    1500,
	}
	return result, nil
}

func newFailurePathAdvisoryWorkflow(t *testing.T, advisory *advisoryScriptedRunner, changedPath string) (*Workflow, *state.StateStore, *bytes.Buffer) {
	t.Helper()
	st := newStateStoreT(t)
	advisory.scriptedRunner = scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	output := &bytes.Buffer{}
	w := newWorkflowTWithOutput(t, st, &advisory.scriptedRunner, output)
	w.runner = advisory
	w.config.FailurePathAdvisory = true
	if changedPath != "" {
		writeAdvisoryRepoFile(t, w.config.RepoRoot, changedPath)
		w.collectChangedPaths = func(string, string) ([]string, error) {
			return []string{changedPath}, nil
		}
	}
	return w, st, output
}

func writeAdvisoryRepoFile(t *testing.T, repoRoot, path string) {
	t.Helper()
	full := filepath.Join(repoRoot, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("package runner\n\nvar site = newProcessGroupCmd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadAdvisoryRegistryT(t *testing.T, st *state.StateStore) failurepathadvisory.Registry {
	t.Helper()
	registry, err := failurepathadvisory.LoadRegistry(st.Path(failurepathadvisory.RegistryFile))
	if err != nil {
		t.Fatalf("advisory registry読み取り: %v", err)
	}
	return registry
}

func TestFailurePathAdvisoryPromptBoundsLargeRequest(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	hugeRequest := strings.Repeat("要求", failurePathAdvisoryRequestBoundBytes)
	attached := w.attachFailurePathAdvisory(hugeRequest, resultFromBody(needsSolReviewPacket()), 1)
	if attached.FailurePathAdvisory == nil {
		t.Fatalf("bounded requestでadvisoryが落ちています: %+v", attached.FailurePathAdvisory)
	}
	prompt := advisory.advisoryPrompt
	if !utf8.ValidString(prompt) {
		t.Fatal("advisory promptがUTF-8境界を壊しています")
	}
	if len(prompt) > failurePathAdvisoryPromptMaxBytes {
		t.Fatalf("advisory prompt size = %d want <= %d", len(prompt), failurePathAdvisoryPromptMaxBytes)
	}
	baseline := failurePathAdvisoryPrompt("", resultFromBody(needsSolReviewPacket()), 1, failurepathadvisory.TriggerDecision{}, "")
	if markers, base := strings.Count(prompt, "[前方を省略]"), strings.Count(baseline, "[前方を省略]"); markers != base+1 {
		t.Fatalf("requestの切詰め表示 = %d baseline = %d", markers, base)
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Advisory.Status != failurepathadvisory.AdvisoryShown {
		t.Fatalf("records = %+v", registry.Records)
	}
}

func TestFailurePathAdvisoryPromptBoundsTriggerAndReviewInputs(t *testing.T) {
	paths := make([]string, 0, 256)
	for index := 0; index < 256; index++ {
		paths = append(paths, fmt.Sprintf("glm-worker/internal/runner/probe_extra_%03d.go", index))
	}
	trigger := failurepathadvisory.TriggerDecision{
		Triggered:    true,
		Classes:      failurepathadvisory.Classes,
		TriggerPaths: paths,
	}
	review := resultFromBody(needsSolReviewPacket())
	review.Issues = strings.Repeat("検", failurePathAdvisoryReviewIssuesBoundBytes)

	prompt := failurePathAdvisoryPrompt(strings.Repeat("要", failurePathAdvisoryRequestBoundBytes), review, 3, trigger, strings.Repeat("改", failurePathAdvisoryDiffBoundBytes))
	baseline := failurePathAdvisoryPrompt("", resultFromBody(needsSolReviewPacket()), 3, failurepathadvisory.TriggerDecision{}, "")

	if !utf8.ValidString(prompt) {
		t.Fatal("advisory promptがUTF-8境界を壊しています")
	}
	if len(prompt) > failurePathAdvisoryPromptMaxBytes {
		t.Fatalf("advisory prompt size = %d want <= %d", len(prompt), failurePathAdvisoryPromptMaxBytes)
	}
	if markers, base := strings.Count(prompt, "[前方を省略]"), strings.Count(baseline, "[前方を省略]"); markers != base+4 {
		t.Fatalf("切詰め表示 = %d baseline = %d", markers, base)
	}
}

func TestFailurePathAdvisoryPromptIncludesTriggerPathDiffOnly(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, _, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	unrelated := filepath.Join(w.config.RepoRoot, "glm-worker/internal/config/note_extra.md")
	if err := os.MkdirAll(filepath.Dir(unrelated), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unrelated, []byte("UNRELATED_CHANGED_TOKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/runner/probe_extra.go", "glm-worker/internal/config/note_extra.md"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	if !strings.Contains(advisory.advisoryPrompt, "newProcessGroupCmd") {
		t.Fatalf("trigger対象pathの変更diffがadvisory入力へ届いていません: %q", advisory.advisoryPrompt)
	}
	if strings.Contains(advisory.advisoryPrompt, "UNRELATED_CHANGED_TOKEN") {
		t.Fatal("trigger対象外pathの変更がadvisory promptへ混入しています")
	}
}

func TestFailurePathAdvisoryDiffFailureFailOpens(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	w.fetchAdvisoryDiff = func(string, string, []string) (string, error) {
		return "", errors.New("snapshot diff unavailable")
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("diff取得不能でcanonical flowが止まっています: %s", st.TaskStatus())
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("diff取得不能時にadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("diff取得不能時にadvisoryがpacketへ載っています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeMissingDiff {
		t.Fatalf("records = %+v", registry.Records)
	}
	if !strings.Contains(registry.Records[0].Detail, "snapshot diff unavailable") {
		t.Fatalf("detail = %q", registry.Records[0].Detail)
	}
	if registry.CohortSize() != 0 {
		t.Fatalf("diff取得不能recordがcohortへ算入されています: %d", registry.CohortSize())
	}
}

func TestFailurePathAdvisoryOversizeChangeFailsOpenBeforeCall(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	bigPath := filepath.Join(w.config.RepoRoot, "glm-worker/internal/runner/probe_extra.go")
	bigContent := []byte(strings.Repeat("a", 200000) + "\nvar site = newProcessGroupCmd\n")
	if err := os.WriteFile(bigPath, bigContent, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("oversize diffでcanonical flowが止まっています: %s", st.TaskStatus())
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("oversize diffでadvisory model callがありました: %d", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("oversize diffでadvisoryがpacketへ載っています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeMissingDiff {
		t.Fatalf("records = %+v", registry.Records)
	}
	if !strings.Contains(registry.Records[0].Detail, "読取上限") {
		t.Fatalf("detail = %q", registry.Records[0].Detail)
	}
	if registry.CohortSize() != 0 {
		t.Fatalf("oversize diff recordがcohortへ算入されています: %d", registry.CohortSize())
	}
}

func TestFailurePathAdvisoryBoundedChangeKeepsPromptBounded(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	bigPath := filepath.Join(w.config.RepoRoot, "glm-worker/internal/runner/probe_extra.go")
	midContent := []byte(strings.Repeat("a", 8192) + "\nvar site = newProcessGroupCmd\n")
	if err := os.WriteFile(bigPath, midContent, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	prompt := advisory.advisoryPrompt
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	if !utf8.ValidString(prompt) {
		t.Fatal("advisory promptがUTF-8境界を壊しています")
	}
	if len(prompt) > failurePathAdvisoryPromptMaxBytes {
		t.Fatalf("advisory prompt size = %d want <= %d", len(prompt), failurePathAdvisoryPromptMaxBytes)
	}
	if !strings.Contains(prompt, "newProcessGroupCmd") {
		t.Fatal("bounded diffがtrigger根拠tokenを保持していません")
	}
	if !strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("上限内のbounded diffでadvisoryが表示されていません")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if registry.Records[0].Advisory == nil || registry.Records[0].Advisory.Status != failurepathadvisory.AdvisoryShown {
		t.Fatalf("advisory outcome = %+v", registry.Records[0].Advisory)
	}
}

func TestFailurePathAdvisoryShownOnSolVisibleReviewPacket(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("canonical review結果が変わっています: %s", st.TaskStatus())
	}
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	if !strings.Contains(advisory.advisoryPrompt, "PRODUCTION_ADVISORY") ||
		!strings.Contains(advisory.advisoryPrompt, "REVIEW_STATUS: NEEDS_SOL_REVIEW") {
		t.Fatalf("advisory prompt = %q", advisory.advisoryPrompt)
	}
	emitted := output.String()
	for _, fragment := range []string{
		`"status":"NEEDS_SOL_REVIEW"`,
		"failure_path_advisory",
		"advisory-call",
		"glm-worker/internal/runner/probe.go:90",
		"indeterminate",
	} {
		if !strings.Contains(emitted, fragment) {
			t.Fatalf("emit packetに %s がありません: %s", fragment, emitted)
		}
	}
	lastReview, err := st.Read("last-review")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(lastReview, "failure_path_advisory") {
		t.Fatal("canonical last-reviewへadvisoryが混入しています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 {
		t.Fatalf("records = %+v", registry.Records)
	}
	record := registry.Records[0]
	if record.Outcome != failurepathadvisory.OutcomeObserved {
		t.Fatalf("outcome = %s detail = %s", record.Outcome, record.Detail)
	}
	if len(record.Findings) != 2 ||
		record.Findings[0].Status != failurepathadvisory.FindingStatusVerified ||
		record.Findings[1].Status != failurepathadvisory.FindingStatusIndeterminate {
		t.Fatalf("findings = %+v", record.Findings)
	}
	if record.Advisory == nil || record.Advisory.Status != failurepathadvisory.AdvisoryShown ||
		record.Advisory.FindingsShown != 1 || record.Advisory.Indeterminate != 1 {
		t.Fatalf("advisory outcome = %+v", record.Advisory)
	}
	if record.AddedGLM == nil || record.AddedGLM.InputTokens != 120 || record.AddedGLM.Calls != 1 {
		t.Fatalf("added GLM = %+v", record.AddedGLM)
	}
	if record.ReviewPacketStatus != "NEEDS_SOL_REVIEW" || record.CallID != "advisory-call" {
		t.Fatalf("review packet status = %q call id = %q", record.ReviewPacketStatus, record.CallID)
	}
	logs, err := st.ReadModelCallLogs(st.ReadOr("task.id", ""))
	if err != nil {
		t.Fatal(err)
	}
	var advisoryEntries int
	for _, log := range logs {
		if log.Role == state.FailurePathReviewerRole {
			advisoryEntries++
			if log.Phase != "failure-path-reviewer-1" || log.Outcome != failurepathadvisory.OutcomeObserved {
				t.Fatalf("advisory telemetry = %s %s", log.Phase, log.Outcome)
			}
		}
	}
	if advisoryEntries != 1 {
		t.Fatalf("advisory telemetry entries = %d want 1", advisoryEntries)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.FailurePathReviewerCalls != 1 || stats.WorkerCalls != 1 || stats.ReviewerCalls != 1 {
		t.Fatalf("role別call会計 = worker:%d reviewer:%d advisory:%d", stats.WorkerCalls, stats.ReviewerCalls, stats.FailurePathReviewerCalls)
	}
}

func TestFailurePathAdvisoryAttachedToPassResultKeepsStatus(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	attached := w.attachFailurePathAdvisory("request", resultFromBody(passPacket()), 1)
	if string(attached.Status) != "PASS" || string(attached.Risk) != "LOW" {
		t.Fatalf("PASS status/riskが変わっています: %s/%s", attached.Status, attached.Risk)
	}
	if attached.FailurePathAdvisory == nil || len(attached.FailurePathAdvisory.Findings) != 1 {
		t.Fatalf("PASS結果へadvisoryが載っていません: %+v", attached.FailurePathAdvisory)
	}
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	registry := loadAdvisoryRegistryT(t, st)
	if registry.Records[0].Advisory == nil || registry.Records[0].Advisory.Status != failurepathadvisory.AdvisoryShown {
		t.Fatalf("advisory outcome = %+v", registry.Records[0].Advisory)
	}
}

func TestFailurePathAdvisorySkipsFixRequiredResult(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, _, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	fixResult := resultFromBody(fixRequiredPacket())
	attached := w.attachFailurePathAdvisory("request", fixResult, 1)
	if attached.FailurePathAdvisory != nil || attached.Issues != fixResult.Issues {
		t.Fatalf("FIX_REQUIRED結果へadvisoryが載っています: %+v", attached)
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("FIX_REQUIRED結果でadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
}

func TestFailurePathAdvisoryRunsOnlyOnSolVisibleReviewResult(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	advisory.steps = []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fixed")},
		{structured: needsSolReviewPacket()},
	}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 1 {
		t.Fatalf("FIX_REQUIRED roundでadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].ReviewPacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("records = %+v", registry.Records)
	}
	if !strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatalf("最終packetへadvisoryがありません: %s", output.String())
	}
}

func TestFailurePathAdvisoryDisabledByKillSwitch(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	w.config.FailurePathAdvisory = false

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("kill switch後にadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	if st.Exists(failurepathadvisory.RegistryFile) {
		t.Fatal("kill switch後にregistryが書かれました")
	}
}

func TestFailurePathAdvisoryDeadlineFailureFailOpens(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryErr: runner.ErrProbeDeadlineExceeded}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("欠測でcanonical flowが止まっています: %s", st.TaskStatus())
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("fail-open時にadvisoryがpacketへ載っています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeMissingDeadline {
		t.Fatalf("records = %+v", registry.Records)
	}
	if record := registry.Records[0]; record.Advisory == nil || record.Advisory.Status != failurepathadvisory.AdvisoryOmittedFailOpen {
		t.Fatalf("advisory outcome = %+v", record.Advisory)
	}
}

func TestFailurePathAdvisorySchemaInvalidOutputFailOpens(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: `{"findings":[],"summary":"s","extra":true}`}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("schema違反時にadvisoryがpacketへ載っています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeMissingSchemaInvalid {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.Records[0].Detail == "" || registry.Records[0].Advisory.Status != failurepathadvisory.AdvisoryOmittedFailOpen {
		t.Fatalf("record = %+v", registry.Records[0])
	}
}

func TestFailurePathAdvisoryIndeterminateOnlyOmitsAdvisory(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: `{"findings":[
	{"target":"glm-worker/internal/state/stats.go:9","class":"metric-reduction-accounting","issue":"会計位置を確認できず","status":"indeterminate"}
],"summary":"s"}`}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("判定不能のみでadvisoryがpacketへ載っています")
	}
	registry := loadAdvisoryRegistryT(t, st)
	record := registry.Records[0]
	if record.Outcome != failurepathadvisory.OutcomeObserved {
		t.Fatalf("outcome = %s", record.Outcome)
	}
	if record.Advisory == nil || record.Advisory.Status != failurepathadvisory.AdvisoryOmittedNoFind || record.Advisory.Indeterminate != 1 {
		t.Fatalf("advisory outcome = %+v", record.Advisory)
	}
}

func TestFailurePathAdvisoryAmbiguousPathOnlyChangeRecordsWithoutRun(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/workflow/labels.go")

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("ambiguous変更でadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeAmbiguous {
		t.Fatalf("records = %+v", registry.Records)
	}
	if registry.CohortSize() != 0 {
		t.Fatalf("ambiguousがcohortに数えられています: %d", registry.CohortSize())
	}
}

func TestFailurePathAdvisoryCohortCapStopsNewRuns(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	registry := failurepathadvisory.Registry{}
	for index := 0; index < failurepathadvisory.CohortCap; index++ {
		registry = registry.WithRecord(failurepathadvisory.Record{
			TaskID:  fmt.Sprintf("prior-%02d", index),
			Outcome: failurepathadvisory.OutcomeObserved,
		})
	}
	if err := failurepathadvisory.SaveRegistry(st.Path(failurepathadvisory.RegistryFile), registry); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("上限到達後にadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("上限到達後にadvisoryがpacketへ載っています")
	}
	loaded := loadAdvisoryRegistryT(t, st)
	if len(loaded.Records) != failurepathadvisory.CohortCap+1 {
		t.Fatalf("records = %d", len(loaded.Records))
	}
	if loaded.Records[len(loaded.Records)-1].Outcome != failurepathadvisory.OutcomeCapped {
		t.Fatalf("最終record = %+v", loaded.Records[len(loaded.Records)-1])
	}
}

func TestFailurePathAdvisoryClassificationFailureRecordsMissing(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return nil, errors.New("changed paths unavailable")
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if advisory.advisoryCalls != 0 {
		t.Fatalf("分類失敗時にadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeClassificationMissing {
		t.Fatalf("records = %+v", registry.Records)
	}
	if !strings.Contains(registry.Records[0].Detail, "changed paths unavailable") {
		t.Fatalf("detail = %q", registry.Records[0].Detail)
	}
}

func TestFailurePathAdvisoryWithoutDeadlineRunnerRecordsMissing(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.config.FailurePathAdvisory = true
	writeAdvisoryRepoFile(t, w.config.RepoRoot, "glm-worker/internal/runner/probe_extra.go")
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"glm-worker/internal/runner/probe_extra.go"}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("canonical review結果が変わっています: %s", st.TaskStatus())
	}
	registry := loadAdvisoryRegistryT(t, st)
	if len(registry.Records) != 1 || registry.Records[0].Outcome != failurepathadvisory.OutcomeMissingRunUnavailable {
		t.Fatalf("records = %+v", registry.Records)
	}
}

func advisoryMeasurementFailures(t *testing.T, st *state.StateStore) []state.ModelCallLog {
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

func TestFailurePathAdvisoryCorruptRegistryFailOpens(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathadvisory.RegistryFile)
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
	if advisory.advisoryCalls != 0 {
		t.Fatalf("corrupt registryでadvisory呼出がありました: %d", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("registry failure時にadvisoryがpacketへ載っています")
	}
	failures := advisoryMeasurementFailures(t, st)
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

func TestFailurePathAdvisorySaveFailureDropsAdvisory(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathadvisory.RegistryFile)
	if err := failurepathadvisory.SaveRegistry(registryPath, failurepathadvisory.Registry{}); err != nil {
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
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("registry保存失敗時にadvisoryがpacketへ載っています")
	}
	failures := advisoryMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"registry-save", "outcome=observed", "call_id=advisory-call"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadAdvisoryRegistryT(t, st)
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) || registry.CohortSize() != 0 {
		t.Fatalf("保存失敗recordがregistryへ算入されています: %+v", registry.Records)
	}
}

func TestFailurePathAdvisoryLockHeldDuringAppendContinuesCanonical(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, output := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathadvisory.RegistryFile)
	if err := failurepathadvisory.SaveRegistry(registryPath, failurepathadvisory.Registry{}); err != nil {
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
	if advisory.advisoryCalls != 1 {
		t.Fatalf("advisory呼出数 = %d want 1", advisory.advisoryCalls)
	}
	if strings.Contains(output.String(), "failure_path_advisory") {
		t.Fatal("lock競合時にadvisoryがpacketへ載いています")
	}
	failures := advisoryMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"registry-save", "lock競合", "outcome=observed"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadAdvisoryRegistryT(t, st)
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) || registry.CohortSize() != 0 {
		t.Fatalf("lock競合recordがregistryへ算入されています: %+v", registry.Records)
	}
}

func TestFailurePathAdvisoryCapRaceRecordNotCounted(t *testing.T) {
	advisory := &advisoryScriptedRunner{advisoryOutput: advisoryFindingsJSON}
	w, st, _ := newFailurePathAdvisoryWorkflow(t, advisory, "glm-worker/internal/runner/probe_extra.go")
	registryPath := st.Path(failurepathadvisory.RegistryFile)
	full := failurepathadvisory.Registry{}
	for index := 0; index < failurepathadvisory.CohortCap; index++ {
		full = full.WithRecord(failurepathadvisory.Record{
			TaskID:  fmt.Sprintf("prior-%02d", index),
			Outcome: failurepathadvisory.OutcomeObserved,
		})
	}
	if err := failurepathadvisory.SaveRegistry(registryPath, full); err != nil {
		t.Fatal(err)
	}

	trigger := failurepathadvisory.TriggerDecision{
		Triggered:    true,
		Classes:      []string{failurepathadvisory.ClassExternalModelInvocation},
		TriggerPaths: []string{"glm-worker/internal/runner/probe_extra.go"},
	}
	record := w.newFailurePathAdvisoryRecord(
		st.ReadOr("task.id", ""), 1, "failure-path-reviewer-1", trigger,
		failurepathadvisory.OutcomeObserved, nil, "advisory-call", "",
	)
	if err := w.appendFailurePathAdvisoryRecord(registryPath, record); err == nil {
		t.Fatal("cohort上限超過appendが成功しました")
	}

	failures := advisoryMeasurementFailures(t, st)
	if len(failures) != 1 {
		t.Fatalf("measurement failures = %+v", failures)
	}
	for _, fragment := range []string{"cohort-cap", "outcome=observed", "call_id=advisory-call"} {
		if !strings.Contains(failures[0].Error, fragment) {
			t.Fatalf("measurement failure error = %q に %s がありません", failures[0].Error, fragment)
		}
	}
	registry := loadAdvisoryRegistryT(t, st)
	if registry.CohortSize() != failurepathadvisory.CohortCap {
		t.Fatalf("cohort = %d want %d", registry.CohortSize(), failurepathadvisory.CohortCap)
	}
	if registry.HasTaskRecord(st.ReadOr("task.id", "")) {
		t.Fatalf("cap競合recordがregistryへ算入されています: %+v", registry.Records)
	}
}
