package app

import (
	"os"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/abeval"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteRepoSearchEvalFallsBackWhenRetainedEventsAreIncomplete(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	appendRepoSearchRouteEvent(t, st, firstTask, state.TaskEventRecord{
		Role: "worker", Phase: state.RepoSearchCategoryWorkerNavigation, Seq: 1,
		Kind: state.RepoSearchEventKind, Subtype: state.RepoSearchOutcomeSearchHit,
		SearchPaths: []string{"a.go", "b.go"}, DurationMS: 400,
	})
	appendRepoSearchRouteEvent(t, st, firstTask, state.TaskEventRecord{
		Role: "worker", Phase: state.RepoSearchCategoryWorkerNavigation, Seq: 2,
		Kind: state.RepoSearchEventKind, Subtype: state.RepoSearchOutcomeSearchErrorFallback,
		DurationMS: 100,
	})
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(st.TaskEventLogPath(firstTask), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{broken\n"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	decoded := executeRepoSearchEval(t, st)
	events := decoded["events"].(map[string]any)
	if events["skipped_lines"].(float64) != 1 {
		t.Fatalf("events = %#v", events)
	}
	report := decoded["report"].(map[string]any)
	tasks := report["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("tasks = %#v", tasks)
	}
	task := tasks[0].(map[string]any)
	if task["task_id"] != firstTask {
		t.Fatalf("task = %#v", task)
	}
	measure := task["measure"].(map[string]any)
	if measure["calls"].(float64) != 2 || measure["results"].(float64) != 2 || measure["duration_ms"].(float64) != 500 {
		t.Fatalf("partial retained events overrode archived projection: %#v", measure)
	}
}

func TestExecuteEvalABUsesCurrentRepoSearchEventProjection(t *testing.T) {
	cfg := evalABTestConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	appendRepoSearchRouteEvent(t, st, taskID, state.TaskEventRecord{
		Role: "worker", Phase: state.RepoSearchCategoryWorkerNavigation, Seq: 1,
		Kind: state.RepoSearchEventKind, Subtype: state.RepoSearchOutcomeSearchHit,
		SearchPaths: []string{"live.go", "owner.go"}, DurationMS: 325,
	})

	spec := evalABSpec()
	orchestrated := evalABOrchestratedRecord(spec)
	orchestrated.GLMUsage = abeval.GLMUsage{Source: abeval.GLMUsageSourceTaskStats, TaskID: taskID}
	dir := writeEvalABRunDir(t, spec, evalABDirectRecord(spec), orchestrated)
	report := executeEvalABReport(t, cfg, dir)

	metrics := report.RepoSearch.Orchestrated
	if metrics == nil || metrics.TaskID != taskID || metrics.Calls != 1 || metrics.Hits != 1 || metrics.Results != 2 || metrics.DurationMS != 325 {
		t.Fatalf("current task repo-search metrics = %#v", metrics)
	}
	raw, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if raw.RepoSearchCalls != 0 || raw.RepoSearchResults != 0 || raw.RepoSearchDurationMS != 0 {
		t.Fatalf("eval-ab projection mutated live task stats: %+v", raw)
	}
}
