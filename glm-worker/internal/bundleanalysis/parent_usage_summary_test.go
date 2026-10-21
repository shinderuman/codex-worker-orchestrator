package bundleanalysis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentUsageSummaryBuckets(t *testing.T) {
	t.Parallel()
	terminal := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, terminal.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		terminal.start.Add(-3*time.Hour), parentUsagePhaseLines(t, terminal.start, terminal.completeAt))
	ambiguousThreadID := "33333333-3333-4333-8333-333333333333"
	for _, rel := range []string{
		"sessions/2026/08/30/rollout-ambiguous-a-" + ambiguousThreadID + ".jsonl",
		"archived_sessions/rollout-ambiguous-b-" + ambiguousThreadID + ".jsonl",
	} {
		writeAnalysisRollout(t, terminal.codexHome, rel, ambiguousThreadID,
			terminal.start.Add(-3*time.Hour), nil)
	}
	ambiguousTaskID := "66666666-6666-4666-8666-666666666666"
	missingTaskID := "77777777-7777-4777-8777-777777777777"
	writeParentUsageStatsArchive(t, terminal.st, ambiguousTaskID, ambiguousThreadID, terminal.start)
	writeParentUsageStatsArchive(t, terminal.st, missingTaskID, "", terminal.start)

	usage := buildParentUsageSummaryForAllStats(t, terminal.cfg, terminal.st)
	if usage.Tasks != 3 || usage.Available != 1 || usage.Ambiguous != 1 || usage.Unknown != 1 {
		t.Fatalf("parent usage buckets = %#v", usage)
	}
	wantStatus := map[string]int{
		"included/" + analysisStatusAvailable:             1,
		codexStatusAmbiguous + "/" + codexStatusAmbiguous: 1,
		codexStatusMissing + "/" + codexStatusMissing:     1,
	}
	if !reflect.DeepEqual(usage.ByStatus, wantStatus) {
		t.Fatalf("parent usage by_status = %#v", usage.ByStatus)
	}

	excluded := map[string]int{codexStatusAmbiguous: 1, codexStatusMissing: 1}
	assertParentUsageIntervalTotals(t, usage.TaskExecution,
		1, 1000, 500, 160, 80, 1500,
		1, 1, 1, 1, 1, 10,
		excluded, excluded)
	assertParentUsageIntervalTotals(t, usage.ParentFinalization,
		1, 600, 300, 80, 20, 800,
		1, 0, 1, 1, 0, 10,
		excluded, excluded)
	assertParentUsagePartition(t, usage.TaskExecution, 3)
	assertParentUsagePartition(t, usage.ParentFinalization, 3)
	assertParentUsageBounded(t, usage)
}

func TestParentUsageSummaryAggregates(t *testing.T) {
	t.Parallel()
	cfg, st, codexHome := newCodexBundleTestState(t)
	now := time.Now().UTC()
	availableThread := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	secondAvailableThread := "dddddddd-dddd-4ddd-8ddd-dddddddddddd"
	counterResetThread := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	missingFieldThread := "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	fixtures := []struct {
		threadID string
		rel      string
		start    time.Time
		lines    []string
	}{
		{threadID: availableThread, rel: "sessions/2026/09/16/rollout-aggregate-available.jsonl",
			start: now.Add(-4 * time.Hour), lines: parentUsagePhaseLines(t, now.Add(-4*time.Hour), now.Add(-4*time.Hour).Add(30*time.Minute))},
		{threadID: counterResetThread, rel: "sessions/2026/09/16/rollout-aggregate-counter-reset.jsonl",
			start: now.Add(-3 * time.Hour), lines: parentUsageCounterResetLines(t, now.Add(-3*time.Hour), now.Add(-3*time.Hour).Add(30*time.Minute))},
		{threadID: missingFieldThread, rel: "sessions/2026/09/16/rollout-aggregate-missing-field.jsonl",
			start: now.Add(-2 * time.Hour), lines: parentUsageMissingFieldLines(t, now.Add(-2*time.Hour), now.Add(-2*time.Hour).Add(30*time.Minute))},
		{threadID: secondAvailableThread, rel: "sessions/2026/09/16/rollout-aggregate-second-available.jsonl",
			start: now.Add(-1 * time.Hour), lines: parentUsagePhaseLines(t, now.Add(-1*time.Hour), now.Add(-1*time.Hour).Add(30*time.Minute))},
	}
	for _, fixture := range fixtures {
		taskID, err := st.StartNewTask()
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetParentCodexIdentity(fixture.threadID, codexTestParentSessionID, nil); err != nil {
			t.Fatal(err)
		}
		analysisRetireCurrentTask(t, st, taskID, fixture.start, fixture.start.Add(30*time.Minute))
		writeAnalysisRollout(t, codexHome, fixture.rel, fixture.threadID, fixture.start.Add(-3*time.Hour), fixture.lines)
	}

	usage := buildParentUsageSummaryForAllStats(t, cfg, st)
	if usage.Tasks != 4 || usage.Available != 3 || usage.Ambiguous != 0 || usage.Unknown != 1 {
		t.Fatalf("parent usage coverage = %#v", usage)
	}

	assertParentUsageIntervalTotals(t, usage.TaskExecution,
		2, 2000, 1000, 320, 160, 3000,
		4, 4, 2, 2, 2, 20,
		map[string]int{analysisStatusCounterReset: 1, parentUsageMissingTokenField: 1},
		map[string]int{})
	assertParentUsageIntervalTotals(t, usage.ParentFinalization,
		2, 1200, 600, 160, 40, 1600,
		4, 0, 2, 2, 0, 20,
		map[string]int{analysisStatusNoObservation: 2},
		map[string]int{})
	assertParentUsagePartition(t, usage.TaskExecution, 4)
	assertParentUsagePartition(t, usage.ParentFinalization, 4)
	assertParentUsageBounded(t, usage)
}

func parentUsageCounterResetLines(t *testing.T, start, completeAt time.Time) []string {
	t.Helper()
	return []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		parentUsageTokenCountLine(t, completeAt.Add(-time.Second), 1200, 600, 300, 200, 900),
		analysisTurnLine(t, completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
}

func parentUsageMissingFieldLines(t *testing.T, start, completeAt time.Time) []string {
	t.Helper()
	return []string{
		parentUsageTokenCountLine(t, start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
		analysisTurnLine(t, start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
		analysisTokenCountLine(t, completeAt.Add(-time.Second), 2000, 1000),
		analysisTurnLine(t, completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
	}
}

func buildParentUsageSummaryForAllStats(t *testing.T, cfg config.AppConfig, st *state.StateStore) ParentUsageSummary {
	t.Helper()
	result, err := st.AllTaskStatsWithArchiveScan()
	if err != nil {
		t.Fatal(err)
	}
	return BuildParentUsageSummary(cfg, st, result.Stats)
}

func writeParentUsageStatsArchive(t *testing.T, st *state.StateStore, taskID string, parentThreadID string, startedAt time.Time) {
	t.Helper()
	template, err := json.Marshal(state.TaskStats{
		Version:   3,
		TaskID:    taskID,
		StartedAt: startedAt,
		Status:    state.TaskStatusComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
	var stats state.TaskStats
	if err := json.Unmarshal(template, &stats); err != nil {
		t.Fatal(err)
	}
	stats.ParentCodexThreadID = parentThreadID
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	path := st.TaskStatsArchivePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertParentUsageIntervalTotals(t *testing.T, interval ParentUsageIntervalView,
	tasksSummed int, inputTokens, cachedInputTokens, outputTokens, reasoningTokens, totalTokens int64,
	tasksCounted int, modelTurns, toolCalls, toolResults, compactions int, toolOutputBytes int64,
	tokensExcluded, activityExcluded map[string]int) {
	t.Helper()
	tokens := interval.Tokens
	if tokens.TasksSummed != tasksSummed || tokens.InputTokens != inputTokens ||
		tokens.CachedInputTokens != cachedInputTokens || tokens.OutputTokens != outputTokens ||
		tokens.ReasoningTokens != reasoningTokens || tokens.TotalTokens != totalTokens {
		t.Fatalf("interval tokens = %#v", tokens)
	}
	activity := interval.Activity
	if activity.TasksCounted != tasksCounted || activity.ModelTurns != modelTurns ||
		activity.ToolCalls != toolCalls || activity.ToolResults != toolResults ||
		activity.Compactions != compactions || activity.ToolOutputBytes != toolOutputBytes {
		t.Fatalf("interval activity = %#v", activity)
	}
	assertParentUsageExcludedStatuses(t, "tokens_excluded_by_status", interval.TokensExcludedByStatus, tokensExcluded)
	assertParentUsageExcludedStatuses(t, "activity_excluded_by_status", interval.ActivityExcludedByStatus, activityExcluded)
}

func assertParentUsageExcludedStatuses(t *testing.T, key string, got, want map[string]int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("interval %s = %#v want %#v", key, got, want)
	}
	for status, count := range want {
		if got[status] != count {
			t.Fatalf("interval %s = %#v want %#v", key, got, want)
		}
	}
}

func assertParentUsagePartition(t *testing.T, interval ParentUsageIntervalView, tasks int) {
	t.Helper()
	tokenTotal := interval.Tokens.TasksSummed
	for _, count := range interval.TokensExcludedByStatus {
		tokenTotal += count
	}
	activityTotal := interval.Activity.TasksCounted
	for _, count := range interval.ActivityExcludedByStatus {
		activityTotal += count
	}
	if tokenTotal != tasks || activityTotal != tasks {
		t.Fatalf("interval partition = tokens %d / activity %d for %d tasks: %#v", tokenTotal, activityTotal, tasks, interval)
	}
}

func assertParentUsageBounded(t *testing.T, usage ParentUsageSummary) {
	t.Helper()
	encoded, err := json.Marshal(usage)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{".jsonl", "source", "baseline", "anchor", "generated_at"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("parent usageへlocator/anchor由来の%qが漏れています: %s", leaked, encoded)
		}
	}
}
