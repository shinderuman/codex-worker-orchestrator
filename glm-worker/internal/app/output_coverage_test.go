package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeArchivedGapStats(t *testing.T, st *state.StateStore, taskID string, modelCalls int) {
	t.Helper()
	archivedAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	stats := state.TaskStats{
		Version:    3,
		TaskID:     taskID,
		StartedAt:  archivedAt,
		ArchivedAt: &archivedAt,
		Status:     state.TaskStatusActive,
		ModelCalls: modelCalls,
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Path("stats"), taskID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func recordCoverageTaskCall(st *state.StateStore, taskID string) {
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:     taskID,
		CallType:   state.CallTypeTask,
		ModelAlias: "opus",
	})
}

func TestPrintStatsKeepsHistoricalPacketCompactions(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	archivedAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	stats := state.TaskStats{
		Version:           3,
		TaskID:            "legacy-compaction-task",
		StartedAt:         archivedAt,
		ArchivedAt:        &archivedAt,
		Status:            state.TaskStatusComplete,
		PacketCompactions: 2,
	}
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(st.Path("stats"), "legacy-compaction-task.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeStatsOutput(t, st)
	if output.PacketCompactions != 2 {
		t.Fatalf("旧archiveのpacket_compactions集計 = %d: %#v", output.PacketCompactions, output)
	}
}

func TestPrintStatsTelemetryCoverageHistoricalGapAndCurrentTask(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	gapTask := "ccc205d1-1111-4222-8333-444444444444"
	writeArchivedGapStats(t, st, gapTask, 1)

	current, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	st.RecordModelCall(state.ReviewerRole, "sonnet")
	recordCoverageTaskCall(st, current)
	recordCoverageTaskCall(st, current)

	output := executeStatsOutput(t, st)
	if output.ModelCalls != 3 {
		t.Fatalf("model_calls = %d", output.ModelCalls)
	}
	coverage := output.TelemetryCoverage
	if coverage.Status != "incomplete" || coverage.StatsCalls != 3 || coverage.RawRecords != 2 || coverage.MissingCalls != 1 || coverage.ExcessRecords != 0 {
		t.Fatalf("coverage = %#v", coverage)
	}
	if coverage.UsageKnown {
		t.Fatalf("gapがあるのにusage_totals_known = true: %#v", coverage)
	}
	if len(coverage.Tasks) != 1 {
		t.Fatalf("coverage.tasks = %#v", coverage.Tasks)
	}
	gap := coverage.Tasks[0]
	if gap.TaskID != gapTask || gap.Classification != "historical-gap" || gap.StatsCalls != 1 || gap.RawRecords != 0 || gap.MissingCalls != 1 {
		t.Fatalf("historical gap明細 = %#v", gap)
	}
}

func TestPrintStatsTelemetryCoverageComplete(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	recordCoverageTaskCall(st, current)

	output := executeStatsOutput(t, st)
	coverage := output.TelemetryCoverage
	if coverage.Status != "complete" || coverage.StatsCalls != 1 || coverage.RawRecords != 1 || coverage.MissingCalls != 0 {
		t.Fatalf("complete時のcoverage = %#v", coverage)
	}
	if !coverage.UsageKnown {
		t.Fatalf("complete時のusage_totals_known = false: %#v", coverage)
	}
	if len(coverage.Tasks) != 0 {
		t.Fatalf("complete時にtask明細 = %#v", coverage.Tasks)
	}
}

func TestPrintStatsTelemetryCoverageCurrentTaskShortageAndUnreadable(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	st.RecordModelCall(state.WorkerRole, "opus")
	recordCoverageTaskCall(st, current)

	brokenTask := "broken0001-1111-4222-8333-444444444444"
	writeArchivedGapStats(t, st, brokenTask, 1)
	telemetryPath := st.ModelCallLogPath(brokenTask)
	if err := os.MkdirAll(filepath.Dir(telemetryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(telemetryPath, []byte("{not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeStatsOutput(t, st)
	coverage := output.TelemetryCoverage
	if coverage.Status != "unreadable" || coverage.MissingCalls != 1 {
		t.Fatalf("coverage = %#v", coverage)
	}
	if coverage.UsageKnown {
		t.Fatalf("unreadableがあるのにusage_totals_known = true: %#v", coverage)
	}
	details := map[string]statsCoverageTask{}
	for _, task := range coverage.Tasks {
		details[task.TaskID] = task
	}
	if len(details) != 2 {
		t.Fatalf("coverage.tasks = %#v", coverage.Tasks)
	}
	incomplete := details[current]
	if incomplete.Classification != "incomplete" || incomplete.StatsCalls != 2 || incomplete.RawRecords != 1 || incomplete.MissingCalls != 1 || incomplete.ExcessRecords != 0 {
		t.Fatalf("現在task明細 = %#v", incomplete)
	}
	unreadable := details[brokenTask]
	if unreadable.Classification != "unreadable" || unreadable.StatsCalls != 1 {
		t.Fatalf("unreadable task明細 = %#v", unreadable)
	}
}

func TestPrintStatsTelemetryCoverageOrphanOnlyIsIncomplete(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	recordCoverageTaskCall(st, current)

	orphanTask := "orphan0001-1111-4222-8333-444444444444"
	recordCoverageTaskCall(st, orphanTask)

	output := executeStatsOutput(t, st)
	coverage := output.TelemetryCoverage
	if coverage.Status != "incomplete" || coverage.StatsCalls != 1 || coverage.RawRecords != 1 || coverage.MissingCalls != 0 || coverage.ExcessRecords != 0 {
		t.Fatalf("orphanのみ時のcoverage = %#v", coverage)
	}
	if coverage.OrphanFiles != 1 {
		t.Fatalf("orphan_files = %d", coverage.OrphanFiles)
	}
	if coverage.UsageKnown {
		t.Fatalf("orphanがあるのにusage_totals_known = true: %#v", coverage)
	}
	if len(coverage.Tasks) != 0 {
		t.Fatalf("集計task自体が完全一致ならtask明細は出しません: %#v", coverage.Tasks)
	}
}

func TestPrintStatsTelemetryCoverageExcessOnlyTaskLineHasUsageUnknown(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	current, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	recordCoverageTaskCall(st, current)
	recordCoverageTaskCall(st, current)

	output := executeStatsOutput(t, st)
	coverage := output.TelemetryCoverage
	if coverage.Status != "incomplete" || coverage.MissingCalls != 0 || coverage.ExcessRecords != 1 {
		t.Fatalf("過剰のみ時のcoverage = %#v", coverage)
	}
	if coverage.UsageKnown {
		t.Fatalf("過剰があるのにusage_totals_known = true: %#v", coverage)
	}
	if len(coverage.Tasks) != 1 {
		t.Fatalf("coverage.tasks = %#v", coverage.Tasks)
	}
	excess := coverage.Tasks[0]
	if excess.TaskID != current || excess.Classification != "incomplete" || excess.StatsCalls != 1 || excess.RawRecords != 2 || excess.MissingCalls != 0 || excess.ExcessRecords != 1 {
		t.Fatalf("過剰のみ時のtask明細 = %#v", excess)
	}
}
