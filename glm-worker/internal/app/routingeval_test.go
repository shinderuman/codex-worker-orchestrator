package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeModelRouting(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printModelRouting(st, &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

func recordModelRoutingFixture(t *testing.T, st *state.StateStore, taskID string, base time.Time) {
	t.Helper()
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-a",
		Role: state.WorkerRole, ModelAlias: "opus", ResolvedModelID: "glm-5.3", Phase: "worker-new",
		StartedAt: base, CompletedAt: base.Add(time.Minute),
		Outcome: "success", PacketStatus: "IMPLEMENTED",
		Prompt: "raw-prompt-must-not-leak", Response: "raw-response-must-not-leak",
		ResolvedModelUsage: map[string]state.ResolvedModelUsage{
			"glm-5.3": {InputTokens: 1000, OutputTokens: 100},
		},
	})
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-b",
		Role: state.ReviewerRole, ModelAlias: "sonnet", ResolvedModelID: "glm-5.3", Phase: "reviewer-1-risk-floor",
		StartedAt: base.Add(time.Hour), CompletedAt: base.Add(time.Hour).Add(time.Minute),
		Outcome: "accepted", PacketStatus: "PASS", EffectiveRisk: "HIGH",
		ResolvedModelUsage: map[string]state.ResolvedModelUsage{
			"glm-5.3": {InputTokens: 500, OutputTokens: 50},
		},
	})
}

func TestExecuteModelRoutingAggregatesSavedTelemetry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordModelRoutingFixture(t, st, taskA, base)

	decoded := executeModelRouting(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "ok" || telemetry["files"].(float64) != 1 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	rounds, _ := decoded["rounds"].(map[string]any)
	if rounds["status"] != "none" {
		t.Fatalf("rounds = %#v", rounds)
	}
	if decoded["repo_root"] != cfg.RepoRoot {
		t.Fatalf("repo_root = %#v", decoded["repo_root"])
	}

	report, _ := decoded["report"].(map[string]any)
	if report["metrics"] == nil || report["sufficiency"] == nil {
		t.Fatalf("report定義sectionがありません: %#v", report)
	}
	sufficiency, _ := report["sufficiency"].(map[string]any)
	if sufficiency["min_quality_calls_per_group"].(float64) != state.ModelRoutingMinQualityCallsPerGroup ||
		sufficiency["min_quality_tasks_per_group"].(float64) != state.ModelRoutingMinQualityTasksPerGroup {
		t.Fatalf("sufficiency = %#v", sufficiency)
	}
	cells, _ := report["cells"].([]any)
	if len(cells) != 2 {
		t.Fatalf("cells = %#v", cells)
	}
	reviewerCell, _ := cells[0].(map[string]any)
	if reviewerCell["role"] != "reviewer" || reviewerCell["phase"] != "reviewer-risk-floor" ||
		reviewerCell["model_alias"] != "sonnet" || reviewerCell["resolved_model"] != "glm-5.3" {
		t.Fatalf("reviewer cell = %#v", reviewerCell)
	}
	if reviewerCell["effective_risk"] != "HIGH" || reviewerCell["convergence_delta"] != state.RoundDeltaUnknown {
		t.Fatalf("reviewer cell軸 = %#v", reviewerCell)
	}
	workerCell, _ := cells[1].(map[string]any)
	if workerCell["role"] != "worker" || workerCell["phase"] != "worker-new" {
		t.Fatalf("worker cell = %#v", workerCell)
	}
	if workerCell["effective_risk"] != state.ModelRoutingUnknownRisk || workerCell["convergence_delta"] != state.RoundDeltaUnknown {
		t.Fatalf("worker cell軸 = %#v", workerCell)
	}
	usage, _ := workerCell["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 1000 || usage["output_tokens"].(float64) != 100 {
		t.Fatalf("worker cell usage = %#v", usage)
	}
	aliasLinks, _ := report["alias_links"].([]any)
	if len(aliasLinks) != 2 {
		t.Fatalf("alias_links = %#v", aliasLinks)
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	if evaluation["quality_delta"] != state.ModelRoutingQualityDeltaUnknown {
		t.Fatalf("evaluation = %#v", evaluation)
	}
	reasons, _ := evaluation["reasons"].([]any)
	if len(reasons) != 1 || reasons[0] != "no attributable downstream quality evidence; operational outcome and packet_status are not model-quality evidence" {
		t.Fatalf("reasons = %#v", reasons)
	}

	var rendered bytes.Buffer
	if err := printModelRouting(st, &rendered); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"raw-prompt-must-not-leak", "raw-response-must-not-leak", "\"prompt\"", "\"response\""} {
		if strings.Contains(rendered.String(), secret) {
			t.Fatalf("出力にprompt/response本文が含まれています: %q in %s", secret, rendered.String())
		}
	}
}

func TestExecuteModelRoutingEmptyState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "modelroutinghash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--model-routing"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeModelRouting {
		t.Fatalf("command = %+v", cmd)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "none" || telemetry["files"].(float64) != 0 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	if decoded["repo_root"] != "" {
		t.Fatalf("repo_root = %#v", decoded["repo_root"])
	}
	report, _ := decoded["report"].(map[string]any)
	for _, key := range []string{"cells", "quality_groups", "alias_links"} {
		value, ok := report[key].([]any)
		if !ok || len(value) != 0 {
			t.Fatalf("reportの%qが空配列ではありません: %#v", key, report[key])
		}
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	if evaluation["quality_delta"] != state.ModelRoutingQualityDeltaUnknown {
		t.Fatalf("evaluation = %#v", evaluation)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--model-routingがstate dirを作成しました: %v", entries)
	}
}

func TestTelemetryScanDirReadErrorIsProcessError(t *testing.T) {
	tests := []struct {
		name string
		mode CommandMode
		dir  func(*state.StateStore) string
	}{
		{name: "call-outliers-telemetry", mode: ModeCallOutliers, dir: func(st *state.StateStore) string { return st.Path("telemetry") }},
		{name: "model-routing-telemetry", mode: ModeModelRouting, dir: func(st *state.StateStore) string { return st.Path("telemetry") }},
		{name: "test-impact-events", mode: ModeTestImpact, dir: func(st *state.StateStore) string { return st.Path("events") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st := state.AttachStateStore(cfg)
			if err := os.MkdirAll(filepath.Dir(test.dir(st)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(test.dir(st), []byte("not-a-dir\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			var out bytes.Buffer
			err := Execute(Command{Mode: test.mode}, cfg, nil, &out, io.Discard)

			if err == nil {
				t.Fatalf("dir読取失敗が正常終了しました: %s", out.String())
			}
			if out.Len() != 0 {
				t.Fatalf("失敗時にstdoutへ出力があります: %q", out.String())
			}
			envelope, _ := writeProcessErrorJSON(t, err)
			if envelope.Error.Kind != "internal" || envelope.Error.Message == "" {
				t.Fatalf("process error = %#v", envelope.Error)
			}
		})
	}
}

func TestModelRoutingJoinsConvergenceDeltaFromRoundRecords(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	recordModelRoutingFixture(t, st, taskA, base)
	if err := st.AppendRoundRecord(state.RoundRecord{
		TaskID:      taskA,
		WorkerPhase: state.RoundWorkerPhaseBaseline,
		CapturedAt:  base,
		Snapshot:    state.SnapshotDigest{Head: "h1", IndexDigest: "i1", WorktreeDigest: "w1"},
		Paths: []state.RoundPathState{
			{Path: "a.go", Class: state.RoundPathClassCode, FullDigest: "d1", SemanticDigest: "s1"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendRoundRecord(state.RoundRecord{
		TaskID:       taskA,
		ReviewNumber: 1,
		WorkerPhase:  "worker-new",
		CapturedAt:   base.Add(30 * time.Minute),
		Snapshot:     state.SnapshotDigest{Head: "h1", IndexDigest: "i2", WorktreeDigest: "w2"},
		Paths: []state.RoundPathState{
			{Path: "a.go", Class: state.RoundPathClassCode, FullDigest: "d2", SemanticDigest: "s2"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	decoded := executeModelRouting(t, st)

	rounds, _ := decoded["rounds"].(map[string]any)
	if rounds["status"] != "ok" {
		t.Fatalf("rounds = %#v", rounds)
	}
	report, _ := decoded["report"].(map[string]any)
	cells, _ := report["cells"].([]any)
	if len(cells) != 2 {
		t.Fatalf("cells = %#v", cells)
	}
	for _, entry := range cells {
		cell, _ := entry.(map[string]any)
		if cell["convergence_delta"] != state.RoundDeltaSemantic {
			t.Fatalf("join済みcellのdelta = %#v", cell)
		}
	}
}

func TestModelRoutingPartialOnUnreadableRoundLog(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordModelRoutingFixture(t, st, taskA, time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC))
	if err := os.MkdirAll(st.RoundLogPath(taskA), 0o700); err != nil {
		t.Fatal(err)
	}

	decoded := executeModelRouting(t, st)

	rounds, _ := decoded["rounds"].(map[string]any)
	if rounds["status"] != "partial" {
		t.Fatalf("rounds = %#v", rounds)
	}
	unreadable, _ := rounds["unreadable_tasks"].([]any)
	if len(unreadable) != 1 {
		t.Fatalf("unreadable_tasks = %#v", unreadable)
	}
	entry, _ := unreadable[0].(map[string]any)
	if entry["task_id"] != taskA || entry["error"] == "" {
		t.Fatalf("unreadable entry = %#v", entry)
	}
	report, _ := decoded["report"].(map[string]any)
	cells, _ := report["cells"].([]any)
	for _, cellEntry := range cells {
		cell, _ := cellEntry.(map[string]any)
		if cell["convergence_delta"] != state.RoundDeltaUnknown {
			t.Fatalf("読取失敗時のdelta = %#v", cell)
		}
	}
}

func TestModelRoutingPartialOnUnreadableTelemetry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskA, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordModelRoutingFixture(t, st, taskA, time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC))
	broken := "44444444-4444-4444-8444-444444444444"
	path := st.ModelCallLogPath(broken)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{\"version\":3,\"call_type\":\"task\",\"not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	decoded := executeModelRouting(t, st)

	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "partial" {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	unreadable, _ := telemetry["unreadable_tasks"].([]any)
	if len(unreadable) != 1 {
		t.Fatalf("unreadable_tasks = %#v", unreadable)
	}
	entry, _ := unreadable[0].(map[string]any)
	if entry["task_id"] != broken || entry["error"] == "" {
		t.Fatalf("unreadable entry = %#v", entry)
	}
}

func TestParseCommandModelRouting(t *testing.T) {
	cmd, err := ParseCommand([]string{"--model-routing"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeModelRouting {
		t.Fatalf("command = %+v", cmd)
	}
	if _, err := ParseCommand([]string{"--model-routing", "extra"}); err == nil {
		t.Fatal("余分な引数が受け入れられています")
	}
}

func TestConvergenceQualityOutcomesRequiresUniqueWorkerAndTerminalIndependentReview(t *testing.T) {
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	records := []state.RoundRecord{
		{Seq: 1, WorkerPhase: state.RoundWorkerPhaseBaseline, CapturedAt: base, Snapshot: state.SnapshotDigest{Head: "h", IndexDigest: "i", WorktreeDigest: "w"}},
		{Seq: 2, ReviewNumber: 1, WorkerPhase: "worker-new", CapturedAt: base.Add(30 * time.Minute), Snapshot: state.SnapshotDigest{Head: "h", IndexDigest: "i2", WorktreeDigest: "w2"}},
	}
	worker := state.ModelCallLog{CallType: state.CallTypeTask, CallID: "worker-1", Role: state.WorkerRole, Phase: "worker-new", PacketStatus: "IMPLEMENTED", StartedAt: base.Add(time.Minute)}
	reviewer := state.ModelCallLog{CallType: state.CallTypeTask, CallID: "reviewer-1", Role: state.ReviewerRole, Phase: "reviewer-1", PacketStatus: "FIX_REQUIRED", StartedAt: base.Add(40 * time.Minute)}

	got := convergenceQualityOutcomes(records, []state.ModelCallLog{worker, reviewer})
	if got["worker-1"] != state.ModelRoutingQualityReviewFixRequired {
		t.Fatalf("quality outcomes = %#v", got)
	}

	reviewer.Phase = "reviewer-1-high-floor"
	reviewer.PacketStatus = "NEEDS_SOL_REVIEW"
	if got := convergenceQualityOutcomes(records, []state.ModelCallLog{worker, reviewer}); len(got) != 0 {
		t.Fatalf("forced high-floor Sol routing became quality evidence: %#v", got)
	}

	reviewer.Phase = "reviewer-1"
	reviewer.PacketStatus = "PASS"
	worker.PacketStatus = "NEEDS_SOL_DECISION"
	if got := convergenceQualityOutcomes(records, []state.ModelCallLog{worker, reviewer}); len(got) != 0 {
		t.Fatalf("non-implemented worker became quality evidence: %#v", got)
	}
	worker.PacketStatus = "IMPLEMENTED"

	second := worker
	second.CallID = "worker-2"
	second.StartedAt = base.Add(2 * time.Minute)
	reviewer.Phase = "reviewer-1"
	reviewer.PacketStatus = "PASS"
	if got := convergenceQualityOutcomes(records, []state.ModelCallLog{worker, second, reviewer}); len(got) != 0 {
		t.Fatalf("ambiguous producing workers became quality evidence: %#v", got)
	}
}
