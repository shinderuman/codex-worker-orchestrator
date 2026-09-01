package app

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeRepoSearchEval(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printRepoSearchEval(st, &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

func appendRepoSearchRouteEvent(t *testing.T, st *state.StateStore, taskID string, record state.TaskEventRecord) {
	t.Helper()
	record.TaskID = taskID
	if err := st.AppendTaskEvent(record); err != nil {
		t.Fatal(err)
	}
}

func TestExecuteRepoSearchEvalAggregatesRoutesWithoutRawQueries(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordRepoSearchOutcome(state.RepoSearchCategoryWorkerNavigation, state.RepoSearchOutcomeSearchHit, 2, 1200*time.Millisecond)
	appendRepoSearchRouteEvent(t, st, taskID, state.TaskEventRecord{
		Role: "worker", Phase: state.RepoSearchCategoryWorkerNavigation, Seq: 1,
		Timestamp: base, Kind: state.RepoSearchEventKind, Subtype: state.RepoSearchOutcomeSearchHit,
		SearchPaths: []string{"a.go", "b.go"}, DurationMS: 1200,
	})

	eventData, err := os.ReadFile(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(eventData), "search_query") {
		t.Fatalf("current repo-search eventがsearch_query keyを永続化しています: %s", eventData)
	}

	decoded := executeRepoSearchEval(t, st)

	events, _ := decoded["events"].(map[string]any)
	if events["status"] != "ok" || events["files"].(float64) != 1 {
		t.Fatalf("events = %#v", events)
	}
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["status"] != "none" {
		t.Fatalf("telemetry = %#v", telemetry)
	}
	if decoded["repo_root"] != cfg.RepoRoot {
		t.Fatalf("repo_root = %#v", decoded["repo_root"])
	}
	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["task_id"] != taskID || task["event_stats_consistency"] != state.RepoSearchConsistencyOk {
		t.Fatalf("task = %#v", task)
	}
	measure, _ := task["measure"].(map[string]any)
	if measure["calls"].(float64) != 1 || measure["hits"].(float64) != 1 || measure["results"].(float64) != 2 ||
		measure["duration_ms"].(float64) != 1200 {
		t.Fatalf("measure = %#v", measure)
	}
	review, _ := task["review"].(map[string]any)
	if review["outcome"] != state.TestImpactReviewOutcomeUnknown {
		t.Fatalf("review = %#v", review)
	}
	totals, _ := report["totals"].(map[string]any)
	if totals["calls"].(float64) != 1 || totals["hits"].(float64) != 1 {
		t.Fatalf("totals = %#v", totals)
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	if evaluation["codex_reduction_delta"] != state.RepoSearchDeltaUnknown || evaluation["quality_delta"] != state.RepoSearchDeltaUnknown {
		t.Fatalf("evaluation = %#v", evaluation)
	}
	reasons, _ := evaluation["reasons"].([]any)
	if len(reasons) == 0 || !strings.Contains(fmt.Sprint(reasons[0]), "orchestrated-side only") {
		t.Fatalf("reasons = %#v", reasons)
	}

	var rendered bytes.Buffer
	if err := printRepoSearchEval(st, &rendered); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rendered.String(), "search_query") {
		t.Fatalf("eval出力へobsolete raw query fieldが漏れています: %s", rendered.String())
	}
}

func TestExecuteRepoSearchEvalFlagsStatsEventsMismatch(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordRepoSearchOutcome(state.RepoSearchCategoryReviewerIndependent, state.RepoSearchOutcomeIndependentHit, 1, 400*time.Millisecond)
	appendRepoSearchRouteEvent(t, st, taskID, state.TaskEventRecord{
		Role: "reviewer", Phase: state.RepoSearchCategoryReviewerIndependent, Seq: 1,
		Timestamp: base, Kind: state.RepoSearchEventKind, Subtype: state.RepoSearchOutcomeIndependentEmpty,
		SearchPaths: nil, DurationMS: 400,
	})

	decoded := executeRepoSearchEval(t, st)

	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	task := tasks[0].(map[string]any)
	if task["event_stats_consistency"] != state.RepoSearchConsistencyMismatch {
		t.Fatalf("task = %#v", task)
	}
	measure, _ := task["measure"].(map[string]any)
	if measure["calls"].(float64) != 1 || measure["hits"].(float64) != 1 {
		t.Fatalf("measure = %#v", measure)
	}
}

func TestExecuteRepoSearchEvalEmptyStateStaysReadOnly(t *testing.T) {
	base := t.TempDir()
	cfg := config.AppConfig{StateBase: base, RepoHash: "reposearchhash", RepoRoot: "/repo"}
	cmd, err := ParseCommand([]string{"--repo-search-eval"})
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Mode != ModeRepoSearchEval {
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
	report, _ := decoded["report"].(map[string]any)
	tasks, _ := report["tasks"].([]any)
	if len(tasks) != 0 {
		t.Fatalf("tasks = %#v", tasks)
	}
	evaluation, _ := report["evaluation"].(map[string]any)
	reasons, _ := evaluation["reasons"].([]any)
	if len(reasons) == 0 {
		t.Fatalf("reasons = %#v", reasons)
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("--repo-search-evalがstate dirを作成しました: %v", entries)
	}
}
