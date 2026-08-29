package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const testImpactOtherTask = "22222222-2222-4222-8222-222222222222"

func executeTestImpact(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printTestImpact(st, &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

func appendTestImpactEventLog(t *testing.T, st *state.StateStore, taskID string, records ...state.TaskEventRecord) {
	t.Helper()
	for index := range records {
		record := records[index]
		record.TaskID = taskID
		if err := st.AppendTaskEvent(record); err != nil {
			t.Fatal(err)
		}
	}
}

func testImpactOperationFixture(callID string, toolName string, toolID string, category string, durationMS int64, base time.Time) []state.TaskEventRecord {
	return []state.TaskEventRecord{
		{
			CallID: callID, Role: "worker", Phase: "worker-new",
			Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []state.TaskBlockSummary{
				{Type: "tool_use", Name: toolName, ToolID: toolID, OperationCategory: category, Bytes: 80},
			},
		},
		{
			CallID: callID, Role: "worker", Phase: "worker-new",
			Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []state.TaskBlockSummary{
				{Type: "tool_result", Name: toolName, ToolID: toolID, OperationCategory: category, Bytes: 100, DurationMS: durationMS},
			},
		},
	}
}

func testImpactRoundRecords(t *testing.T, st *state.StateStore, taskID string, base time.Time) {
	t.Helper()
	if err := st.AppendRoundRecord(state.RoundRecord{
		TaskID:      taskID,
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
		TaskID:       taskID,
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
}

func findTestImpactTask(t *testing.T, decoded map[string]any, taskID string) map[string]any {
	t.Helper()
	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	for _, entry := range tasks {
		task, _ := entry.(map[string]any)
		if task["task_id"] == taskID {
			return task
		}
	}
	t.Fatalf("reportにtask %sがありません: %#v", taskID, report["tasks"])
	return nil
}

func findTestImpactOperation(task map[string]any, category string) map[string]any {
	operations, _ := task["operations"].([]any)
	for _, entry := range operations {
		operation, _ := entry.(map[string]any)
		if operation["category"] == category {
			return operation
		}
	}
	return nil
}

func TestExecuteTestImpactAggregatesSavedEvents(t *testing.T) {
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
	appendTestImpactEventLog(t, st, taskA, testImpactOperationFixture("call-a", "Bash", "t1", state.OperationCategoryTest, 1500, base)...)
	appendTestImpactEventLog(t, st, testImpactOtherTask, testImpactOperationFixture("call-b", "Edit", "w1", state.OperationCategoryFileWrite, 40, base)...)
	recordModelRoutingFixture(t, st, taskA, base)
	testImpactRoundRecords(t, st, taskA, base)

	decoded := executeTestImpact(t, st)

	events, _ := decoded["events"].(map[string]any)
	if events["status"] != "ok" || events["files"].(float64) != 2 {
		t.Fatalf("events = %#v", events)
	}
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "ok" || telemetry["files"].(float64) != 1 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	rounds, _ := decoded["rounds"].(map[string]any)
	if rounds["status"] != "ok" {
		t.Fatalf("rounds = %#v", rounds)
	}
	if decoded["repo_root"] != cfg.RepoRoot {
		t.Fatalf("repo_root = %#v", decoded["repo_root"])
	}

	report, _ := decoded["report"].(map[string]any)
	if report["retention"].(float64) != 10 {
		t.Fatalf("retention = %#v", report["retention"])
	}
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 2 {
		t.Fatalf("tasks = %#v", tasks)
	}
	taskASummary := findTestImpactTask(t, decoded, taskA)
	testOperation := findTestImpactOperation(taskASummary, state.OperationCategoryTest)
	if testOperation == nil || testOperation["uses"].(float64) != 1 || testOperation["results"].(float64) != 1 ||
		testOperation["measured"].(float64) != 1 || testOperation["measured_sum_ms"].(float64) != 1500 {
		t.Fatalf("taskA test operation = %#v", testOperation)
	}
	review, _ := taskASummary["review"].(map[string]any)
	if review["outcome"] != state.ModelRoutingQualityReviewPass || review["pass_calls"].(float64) != 1 {
		t.Fatalf("taskA review = %#v", review)
	}
	taskBSummary := findTestImpactTask(t, decoded, testImpactOtherTask)
	writeOperation := findTestImpactOperation(taskBSummary, state.OperationCategoryFileWrite)
	if writeOperation == nil || writeOperation["uses"].(float64) != 1 || writeOperation["measured_sum_ms"].(float64) != 40 {
		t.Fatalf("taskB write operation = %#v", writeOperation)
	}
	if findTestImpactOperation(taskBSummary, state.OperationCategoryTest) != nil {
		t.Fatalf("taskBにtest operationがあります: %#v", taskBSummary)
	}
	reviewB, _ := taskBSummary["review"].(map[string]any)
	if reviewB["outcome"] != state.TestImpactReviewOutcomeUnknown {
		t.Fatalf("taskB review = %#v", reviewB)
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	if evaluation["suite_coverage"] != state.TestImpactSuiteCoverageUnknown {
		t.Fatalf("suite coverage = %#v", evaluation["suite_coverage"])
	}
	candidates, _ := evaluation["omission_candidates"].([]any)
	if len(candidates) != 0 {
		t.Fatalf("omission candidates = %#v", candidates)
	}
	reasons, _ := evaluation["reasons"].([]any)
	if len(reasons) == 0 || !strings.Contains(fmt.Sprint(reasons[0]), "suite-level coverage is unknown") {
		t.Fatalf("reasons = %#v", reasons)
	}

	var rendered bytes.Buffer
	if err := printTestImpact(st, &rendered); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"raw-prompt-must-not-leak", "raw-response-must-not-leak", "\"prompt\"", "\"response\""} {
		if strings.Contains(rendered.String(), secret) {
			t.Fatalf("出力にprompt/response本文が含まれています: %q in %s", secret, rendered.String())
		}
	}
}

func TestExecuteTestImpactEmptyState(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "testimpacthash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--test-impact"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeTestImpact {
		t.Fatalf("command = %+v", cmd)
	}
	out := &bytes.Buffer{}
	if err := Execute(cmd, cfg, nil, out, io.Discard); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	events, _ := decoded["events"].(map[string]any)
	if events["status"] != "none" || events["files"].(float64) != 0 {
		t.Fatalf("events = %#v", events)
	}
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "none" || telemetry["files"].(float64) != 0 {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 0 {
		t.Fatalf("tasks = %#v", tasks)
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	reasons, _ := evaluation["reasons"].([]any)
	if len(reasons) != 2 || !strings.Contains(fmt.Sprint(reasons[1]), "no task event logs are retained") {
		t.Fatalf("reasons = %#v", reasons)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--test-impactがstate dirを作成しました: %v", entries)
	}
}

func TestTestImpactPartialOnUnreadableEventLog(t *testing.T) {
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
	appendTestImpactEventLog(t, st, taskA, testImpactOperationFixture("call-a", "Bash", "t1", state.OperationCategoryTest, 1500, base)...)
	if err := os.MkdirAll(st.TaskEventLogPath(testImpactOtherTask), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(st.TaskEventLogPath("not-a-uuid")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskEventLogPath("not-a-uuid"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	decoded := executeTestImpact(t, st)

	events, _ := decoded["events"].(map[string]any)
	if events["status"] != "partial" || events["files"].(float64) != 1 {
		t.Fatalf("events = %#v", events)
	}
	unreadable, _ := events["unreadable_tasks"].([]any)
	if len(unreadable) != 1 || unreadable[0].(map[string]any)["task_id"] != testImpactOtherTask {
		t.Fatalf("unreadable = %#v", unreadable)
	}
	ignored, _ := events["ignored_files"].([]any)
	if len(ignored) != 1 || ignored[0] != "not-a-uuid.jsonl" {
		t.Fatalf("ignored = %#v", ignored)
	}
	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["task_id"] != taskA {
		t.Fatalf("tasks = %#v", tasks)
	}
}

func TestTestImpactSkipsCorruptEventLines(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 29, 9, 0, 0, 0, time.UTC)
	fixture := testImpactOperationFixture("call-a", "Bash", "t1", state.OperationCategoryTest, 1500, base)
	fixture[0].TaskID = testImpactOtherTask
	fixture[0].Version = 1
	fixture[1].TaskID = testImpactOtherTask
	good, err := json.Marshal(fixture[0])
	if err != nil {
		t.Fatal(err)
	}
	path := st.TaskEventLogPath(testImpactOtherTask)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append([]byte("not json\n"), append(good, '\n')...), 0o600); err != nil {
		t.Fatal(err)
	}

	decoded := executeTestImpact(t, st)

	events, _ := decoded["events"].(map[string]any)
	if events["status"] != "ok" || events["files"].(float64) != 1 || events["skipped_lines"].(float64) != 1 {
		t.Fatalf("events = %#v", events)
	}
	task := findTestImpactTask(t, decoded, testImpactOtherTask)
	if findTestImpactOperation(task, state.OperationCategoryTest) == nil {
		t.Fatalf("test operationがありません: %#v", task)
	}
}
