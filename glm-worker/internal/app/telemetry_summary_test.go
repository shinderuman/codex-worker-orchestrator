package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type telemetryCompactFixture struct {
	cfg                config.AppConfig
	st                 *state.StateStore
	oldTelemetryTaskID string
	currentTaskID      string
	periodStart        time.Time
}

func TestTelemetryCompactSummaryUnifiedAcrossCommands(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)
	for _, query := range [][]string{
		{"history"},
		{"history", "--task", fixture.currentTaskID},
		{"history", "--since", fixture.periodStart.Format(time.RFC3339)},
		{"current"},
		{"current", "--task", fixture.currentTaskID},
	} {
		t.Run(strings.Join(query, " "), func(t *testing.T) {
			var statsOut, outliersOut bytes.Buffer
			statsCmd, err := ParseCommand(append(append([]string{"--stats"}, query...), "--compact"))
			if err != nil {
				t.Fatal(err)
			}
			if err := Execute(statsCmd, fixture.cfg, nil, &statsOut, nil); err != nil {
				t.Fatal(err)
			}
			outliersCmd, err := ParseCommand(append(append([]string{"--call-outliers"}, query...), "--compact"))
			if err != nil {
				t.Fatal(err)
			}
			if err := Execute(outliersCmd, fixture.cfg, nil, &outliersOut, nil); err != nil {
				t.Fatal(err)
			}
			if statsOut.String() != outliersOut.String() {
				t.Fatalf("同一queryのcompact summaryがcommand間で diverge しました: %q vs %q", statsOut.String(), outliersOut.String())
			}
			decoded := decodeSingleLineJSON(t, statsOut.String())
			if decoded["version"].(float64) != telemetryCompactSummaryVersion {
				t.Fatalf("version = %#v", decoded["version"])
			}
		})
	}
}

func TestTelemetryCompactSummaryHistorySections(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats", "history", "--compact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, fixture.cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "raw-prompt-must-not-leak") {
		t.Fatalf("compact summaryへraw promptが漏れています: %s", out.String())
	}
	decoded := decodeSingleLineJSON(t, out.String())

	query, _ := decoded["query"].(map[string]any)
	if query["scope"] != state.TelemetryScopeHistory ||
		query["telemetry_period_basis"] != telemetryQueryPeriodBasisRecord ||
		query["stats_period_basis"] != telemetryQueryPeriodBasisTask {
		t.Fatalf("query = %#v", query)
	}

	scan, _ := decoded["scan"].(map[string]any)
	if scan["status"] != "ok" || scan["files_considered"].(float64) != 3 || scan["malformed_records"].(float64) != 1 {
		t.Fatalf("scan = %#v", scan)
	}

	cohorts, _ := decoded["cohorts"].([]any)
	if len(cohorts) != 2 {
		t.Fatalf("cohorts = %#v", cohorts)
	}
	oldCohort, _ := cohorts[0].(map[string]any)
	if oldCohort["version"].(float64) != 3 || oldCohort["schema_revision"].(float64) != 0 ||
		oldCohort["current_schema"] != false || oldCohort["excluded_reason"] != nil {
		t.Fatalf("旧cohort = %#v", oldCohort)
	}
	oldOutliers, _ := oldCohort["outliers"].(map[string]any)
	if oldOutliers["status"] != telemetryCompactOutliersEvaluated ||
		oldOutliers["eligible_groups"].(float64) != 1 ||
		oldOutliers["outlier_calls"].(float64) != 1 || oldOutliers["outlier_tasks"].(float64) != 0 ||
		oldOutliers["truncated_calls"].(float64) != 0 {
		t.Fatalf("旧cohort outliers = %#v", oldOutliers)
	}
	topCalls, _ := oldOutliers["top_calls"].([]any)
	if len(topCalls) != 1 {
		t.Fatalf("top_calls = %#v", topCalls)
	}
	topCall, _ := topCalls[0].(map[string]any)
	if topCall["call_id"] != "old-spike" || topCall["task_id"] != fixture.oldTelemetryTaskID ||
		topCall["turns"].(float64) != 100 {
		t.Fatalf("top_call = %#v", topCall)
	}
	coverage, _ := oldCohort["coverage"].(map[string]any)
	if coverage["usage_totals_known"] != false || coverage["task_calls_missing_usage"].(float64) != 20 {
		t.Fatalf("旧cohort coverage = %#v", coverage)
	}

	currentCohort, _ := cohorts[1].(map[string]any)
	if currentCohort["current_schema"] != true ||
		currentCohort["excluded_reason"] != state.TelemetryExclusionCurrentSchema {
		t.Fatalf("current cohort = %#v", currentCohort)
	}
	currentOutliers, _ := currentCohort["outliers"].(map[string]any)
	if currentOutliers["status"] != telemetryCompactOutliersNotEvaluated ||
		currentOutliers["outlier_calls"].(float64) != 0 || currentOutliers["truncated_calls"].(float64) != 0 {
		t.Fatalf("current cohort outliers = %#v", currentOutliers)
	}

	stats, _ := decoded["stats"].(map[string]any)
	if stats["period_basis"] != telemetryQueryPeriodBasisTask ||
		stats["cohort_version"].(float64) != float64(state.ModelCallLogVersion) ||
		stats["cohort_schema_revision"].(float64) != float64(state.ModelCallLogSchemaRevision) {
		t.Fatalf("stats header = %#v", stats)
	}
	if stats["tasks_considered"].(float64) != 3 || stats["current_schema_tasks"].(float64) != 1 ||
		stats["tasks_without_current_schema_records"].(float64) != 2 {
		t.Fatalf("stats attribution = %#v", stats)
	}
	if stats["worker_calls"].(float64) != 1 || stats["fix_commands"].(float64) != 1 ||
		stats["resume_commands"].(float64) != 1 || stats["rate_limits"].(float64) != 1 ||
		stats["sol_packet_bytes"].(float64) != 12345 {
		t.Fatalf("stats totals = %#v", stats)
	}
	promptTokens, _ := stats["total_prompt_tokens_by_alias"].(map[string]any)
	if promptTokens["haiku"].(float64) != 400 || len(promptTokens) != 1 {
		t.Fatalf("total prompt tokens = %#v", promptTokens)
	}
	outputTokens, _ := stats["output_tokens_by_alias"].(map[string]any)
	if outputTokens["haiku"].(float64) != 100 || len(outputTokens) != 1 {
		t.Fatalf("output tokens = %#v", outputTokens)
	}
	turns, _ := stats["turns_by_alias"].(map[string]any)
	if turns["haiku"].(float64) != 30 || len(turns) != 1 {
		t.Fatalf("turns = %#v", turns)
	}
	parentOutcomes, _ := stats["parent_outcomes"].(map[string]any)
	if parentOutcomes["implemented"].(float64) != 1 || len(parentOutcomes) != 1 {
		t.Fatalf("parent outcomes = %#v", parentOutcomes)
	}
	fixOrigins, _ := stats["parent_fix_origins"].(map[string]any)
	if fixOrigins["codex-review"].(float64) != 2 || len(fixOrigins) != 1 {
		t.Fatalf("parent fix origins = %#v", fixOrigins)
	}

	parentUsage, _ := decoded["parent_usage"].(map[string]any)
	if parentUsage["tasks"].(float64) != 3 || parentUsage["available"].(float64) != 0 ||
		parentUsage["ambiguous"].(float64) != 0 || parentUsage["unknown"].(float64) != 3 {
		t.Fatalf("parent usage = %#v", parentUsage)
	}
	byStatus, _ := parentUsage["by_status"].(map[string]any)
	if byStatus["missing/missing"].(float64) != 3 || len(byStatus) != 1 {
		t.Fatalf("parent usage by_status = %#v", byStatus)
	}

	bounds, _ := decoded["bounds"].(map[string]any)
	if bounds["top_outlier_calls"].(float64) != telemetryCompactTopOutlierCalls ||
		bounds["top_outlier_tasks"].(float64) != telemetryCompactTopOutlierTasks {
		t.Fatalf("bounds = %#v", bounds)
	}
}

func TestTelemetryCompactSummaryHistoryPeriodAndTaskFilter(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)

	var sinceOut bytes.Buffer
	sinceCmd, err := ParseCommand([]string{"--call-outliers", "history", "--compact",
		"--since", fixture.periodStart.Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(sinceCmd, fixture.cfg, nil, &sinceOut, nil); err != nil {
		t.Fatal(err)
	}
	since := decodeSingleLineJSON(t, sinceOut.String())
	cohorts, _ := since["cohorts"].([]any)
	if len(cohorts) != 1 {
		t.Fatalf("期間filter後のcohorts = %#v", cohorts)
	}
	remaining, _ := cohorts[0].(map[string]any)
	if remaining["schema_revision"].(float64) != float64(state.ModelCallLogSchemaRevision) {
		t.Fatalf("残るcohort = %#v", remaining)
	}
	sinceStats, _ := since["stats"].(map[string]any)
	if sinceStats["tasks_considered"].(float64) != 2 ||
		sinceStats["tasks_without_current_schema_records"].(float64) != 1 {
		t.Fatalf("期間filter後のstats = %#v", sinceStats)
	}

	var taskOut bytes.Buffer
	taskCmd, err := ParseCommand([]string{"--stats", "history", "--compact",
		"--task", "99999999-9999-4999-8999-999999999999"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(taskCmd, fixture.cfg, nil, &taskOut, nil); err != nil {
		t.Fatal(err)
	}
	task := decodeSingleLineJSON(t, taskOut.String())
	taskScan, _ := task["scan"].(map[string]any)
	if taskScan["status"] != "none" {
		t.Fatalf("未知task filterのscan = %#v", taskScan)
	}
	taskCohorts, _ := task["cohorts"].([]any)
	if len(taskCohorts) != 0 {
		t.Fatalf("未知task filterのcohorts = %#v", taskCohorts)
	}
	taskStats, _ := task["stats"].(map[string]any)
	if taskStats["tasks_considered"].(float64) != 0 {
		t.Fatalf("未知task filterのstats = %#v", taskStats)
	}
	taskUsage, _ := task["parent_usage"].(map[string]any)
	if taskUsage["tasks"].(float64) != 0 {
		t.Fatalf("未知task filterのparent usage = %#v", taskUsage)
	}
}

func TestTelemetryCompactSummaryCurrentScope(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--call-outliers", "--compact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, fixture.cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())

	query, _ := decoded["query"].(map[string]any)
	if query["scope"] != state.TelemetryScopeCurrent {
		t.Fatalf("query = %#v", query)
	}
	scan, _ := decoded["scan"].(map[string]any)
	if scan["status"] != statusPartial || scan["files_considered"].(float64) != 3 ||
		scan["unreadable_files"].(float64) != 1 || scan["malformed_records"].(float64) != 0 {
		t.Fatalf("scan = %#v", scan)
	}
	cohorts, _ := decoded["cohorts"].([]any)
	if len(cohorts) != 1 {
		t.Fatalf("current scopeのcohorts = %#v", cohorts)
	}
	cohort, _ := cohorts[0].(map[string]any)
	if cohort["current_schema"] != true || cohort["excluded_reason"] != nil ||
		cohort["version"].(float64) != float64(state.ModelCallLogVersion) {
		t.Fatalf("current cohort = %#v", cohort)
	}
	outliers, _ := cohort["outliers"].(map[string]any)
	if outliers["status"] != telemetryCompactOutliersEvaluated {
		t.Fatalf("current scope outliers = %#v", outliers)
	}
	stats, _ := decoded["stats"].(map[string]any)
	if stats["tasks_considered"].(float64) != 3 || stats["current_schema_tasks"].(float64) != 1 {
		t.Fatalf("current scope stats = %#v", stats)
	}
}

func TestTelemetryCompactSummaryCurrentScopeWithoutCurrentSchemaRecords(t *testing.T) {
	assertEvaluatedEmptyCohort := func(t *testing.T, cfg config.AppConfig, wantScanStatus string, wantFilesConsidered float64) {
		t.Helper()
		var statsOut, outliersOut bytes.Buffer
		statsCmd, err := ParseCommand([]string{"--stats", "--compact"})
		if err != nil {
			t.Fatal(err)
		}
		if err := Execute(statsCmd, cfg, nil, &statsOut, nil); err != nil {
			t.Fatal(err)
		}
		outliersCmd, err := ParseCommand([]string{"--call-outliers", "--compact"})
		if err != nil {
			t.Fatal(err)
		}
		if err := Execute(outliersCmd, cfg, nil, &outliersOut, nil); err != nil {
			t.Fatal(err)
		}
		if statsOut.String() != outliersOut.String() {
			t.Fatalf("current-schema record 0件でも両commandの出力が一致しません: %q vs %q", statsOut.String(), outliersOut.String())
		}
		decoded := decodeSingleLineJSON(t, statsOut.String())
		scan, _ := decoded["scan"].(map[string]any)
		if scan["status"] != wantScanStatus || scan["files_considered"].(float64) != wantFilesConsidered {
			t.Fatalf("scan = %#v", scan)
		}
		cohorts, _ := decoded["cohorts"].([]any)
		if len(cohorts) != 1 {
			t.Fatalf("cohorts = %#v", cohorts)
		}
		cohort, _ := cohorts[0].(map[string]any)
		if cohort["version"].(float64) != float64(state.ModelCallLogVersion) ||
			cohort["schema_revision"].(float64) != float64(state.ModelCallLogSchemaRevision) ||
			cohort["current_schema"] != true || cohort["files"].(float64) != 0 ||
			cohort["tasks"].(float64) != 0 {
			t.Fatalf("空current-schema cohort = %#v", cohort)
		}
		records, _ := cohort["records"].(map[string]any)
		if records["read"].(float64) != 0 || records["task_calls"].(float64) != 0 {
			t.Fatalf("空cohort records = %#v", records)
		}
		outliers, _ := cohort["outliers"].(map[string]any)
		if outliers["status"] != telemetryCompactOutliersEvaluated ||
			outliers["eligible_groups"].(float64) != 0 ||
			outliers["outlier_calls"].(float64) != 0 || outliers["outlier_tasks"].(float64) != 0 ||
			outliers["truncated_calls"].(float64) != 0 || outliers["truncated_tasks"].(float64) != 0 {
			t.Fatalf("空cohort outliers = %#v", outliers)
		}
		stats, _ := decoded["stats"].(map[string]any)
		if stats["tasks_considered"].(float64) != 0 || stats["current_schema_tasks"].(float64) != 0 {
			t.Fatalf("stats = %#v", stats)
		}
		parentUsage, _ := decoded["parent_usage"].(map[string]any)
		if parentUsage["tasks"].(float64) != 0 {
			t.Fatalf("parent usage = %#v", parentUsage)
		}
	}

	t.Run("empty store", func(t *testing.T) {
		cfg := newAppConfig(t)
		assertEvaluatedEmptyCohort(t, cfg, "none", 0)
	})

	t.Run("old-schema records only", func(t *testing.T) {
		cfg := newAppConfig(t)
		st := state.AttachStateStore(cfg)
		if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
			t.Fatal(err)
		}
		taskID := "22222222-2222-4222-8222-222222222222"
		line := `{"version":3,"call_id":"legacy-only","call_type":"task","task_id":"` + taskID + `","started_at":"2026-09-01T09:00:00Z","phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":50,"wall_duration_ms":1000}`
		if err := os.WriteFile(st.Path("telemetry/"+taskID+".jsonl"), []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		assertEvaluatedEmptyCohort(t, cfg, "ok", 1)
	})
}

func TestTelemetryCompactSummaryPartialScan(t *testing.T) {
	fixture := newTelemetryCompactFixture(t)
	if err := os.Mkdir(fixture.st.Path("telemetry/77777777-7777-4777-8777-777777777777.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats", "history", "--compact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, fixture.cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	scan, _ := decoded["scan"].(map[string]any)
	if scan["status"] != statusPartial || scan["unreadable_files"].(float64) != 1 {
		t.Fatalf("partial scan = %#v", scan)
	}
}

func TestTelemetryCompactSummaryParentUsageBuckets(t *testing.T) {
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
	writeTelemetryCompactStatsArchive(t, terminal.st, ambiguousTaskID, ambiguousThreadID, terminal.start)
	writeTelemetryCompactStatsArchive(t, terminal.st, missingTaskID, "", terminal.start)

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats", "history", "--compact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, terminal.cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	parentUsage, _ := decoded["parent_usage"].(map[string]any)
	if parentUsage["tasks"].(float64) != 3 || parentUsage["available"].(float64) != 1 ||
		parentUsage["ambiguous"].(float64) != 1 || parentUsage["unknown"].(float64) != 1 {
		t.Fatalf("parent usage buckets = %#v", parentUsage)
	}
	byStatus, _ := parentUsage["by_status"].(map[string]any)
	if byStatus["included/"+analysisStatusAvailable].(float64) != 1 ||
		byStatus[codexStatusAmbiguous+"/"+codexStatusAmbiguous].(float64) != 1 ||
		byStatus[codexStatusMissing+"/"+codexStatusMissing].(float64) != 1 || len(byStatus) != 3 {
		t.Fatalf("parent usage by_status = %#v", byStatus)
	}
}

func TestTelemetryCompactSummaryBoundedOnLargeFixture(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	taskID := "88888888-8888-4888-8888-888888888888"
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lines := make([]string, 0, 300)
	for index := 0; index < 285; index++ {
		lines = append(lines, fmt.Sprintf(
			`{"version":3,"call_id":"bulk-normal-%d","call_type":"task","task_id":%q,"started_at":%q,"phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":10,"wall_duration_ms":1000,"prompt":"raw-prompt-must-not-leak"}`,
			index, taskID, base.Add(time.Duration(index)*time.Second).Format(time.RFC3339),
		))
	}
	for index := 0; index < 15; index++ {
		lines = append(lines, fmt.Sprintf(
			`{"version":3,"call_id":"bulk-spike-%d","call_type":"task","task_id":%q,"started_at":%q,"phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":100,"wall_duration_ms":1000}`,
			index, taskID, base.Add(time.Duration(1000+index)*time.Second).Format(time.RFC3339),
		))
	}
	path := st.Path("telemetry/" + taskID + ".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 64*1024 {
		t.Fatalf("fixtureが64KB級ではありません: %d bytes", info.Size())
	}

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--call-outliers", "history", "--compact"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "raw-prompt-must-not-leak") {
		t.Fatalf("compact summaryへraw promptが漏れています: %s", rendered)
	}
	if len(rendered) >= 16*1024 {
		t.Fatalf("compact summaryが上限を超えています: %d bytes", len(rendered))
	}
	var single map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(rendered)), &single); err != nil {
		t.Fatalf("single JSON objectとして解析できません: %v", err)
	}
	decoded := decodeSingleLineJSON(t, rendered)
	cohorts, _ := decoded["cohorts"].([]any)
	cohort, _ := cohorts[0].(map[string]any)
	outliers, _ := cohort["outliers"].(map[string]any)
	if outliers["outlier_calls"].(float64) != 15 {
		t.Fatalf("outlier_calls = %#v", outliers)
	}
	topCalls, _ := outliers["top_calls"].([]any)
	if len(topCalls) != telemetryCompactTopOutlierCalls || outliers["truncated_calls"].(float64) != 5 {
		t.Fatalf("top_calls/truncated = %#v", outliers)
	}
}

func newTelemetryCompactFixture(t *testing.T) telemetryCompactFixture {
	t.Helper()
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	periodStart := base.Add(12 * time.Hour)

	oldTaskID := "22222222-2222-4222-8222-222222222222"
	oldLines := make([]string, 0, 22)
	for index := 0; index < 20; index++ {
		turns := 10
		callID := "old-normal"
		if index == 19 {
			turns = 100
			callID = "old-spike"
		}
		oldLines = append(oldLines, fmt.Sprintf(
			`{"version":3,"call_id":%q,"call_type":"task","task_id":%q,"started_at":%q,"phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":%d,"wall_duration_ms":1000,"prompt":"raw-prompt-must-not-leak"}`,
			callID, oldTaskID, base.Add(time.Duration(index)*time.Minute).Format(time.RFC3339), turns,
		))
	}
	oldLines = append(oldLines, "{\"version\":3,\"broken\"")
	if err := os.WriteFile(st.Path("telemetry/"+oldTaskID+".jsonl"), []byte(strings.Join(oldLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldStatsTaskID := "44444444-4444-4444-8444-444444444444"
	oldStatsLines := []string{
		`{"version":3,"call_id":"legacy-1","call_type":"task","task_id":"` + oldStatsTaskID + `","started_at":"` + base.Add(time.Hour).Format(time.RFC3339) + `","phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":50,"wall_duration_ms":1000,"tree_usage":{"input_tokens":100000,"output_tokens":5}}`,
	}
	if err := os.WriteFile(st.Path("telemetry/"+oldStatsTaskID+".jsonl"), []byte(strings.Join(oldStatsLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeTelemetryCompactStatsArchiveWith(t, st, oldStatsTaskID, base.Add(time.Hour), func(stats *state.TaskStats) {
		stats.WorkerCalls = 7
		stats.InputTokensByAlias = map[string]int64{"opus": 100000}
		stats.SolPacketBytes = 999
		stats.ParentOutcomes = map[string]int{"no-go": 5}
	})

	archiveOnlyTaskID := "12345678-1234-4123-8123-123456789123"
	writeTelemetryCompactStatsArchiveWith(t, st, archiveOnlyTaskID, base.Add(23*time.Hour), nil)

	currentTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	currentStart := base.Add(13 * time.Hour)
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.StartedAt = currentStart
		stats.FixCommands = 1
		stats.ResumeCommands = 1
		stats.RateLimits = 1
		stats.SolPacketBytes = 12345
		stats.ParentOutcomes = map[string]int{"implemented": 1}
		stats.ParentFixOrigins = map[string]int{"codex-review": 2}
	})
	st.RecordModelCall(state.WorkerRole, "haiku")
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: currentTaskID, SessionID: "sess-compact",
		Role: state.WorkerRole, ModelAlias: "haiku", Phase: "worker-new",
		StartedAt: currentStart, CompletedAt: currentStart.Add(time.Minute),
		Outcome: "success", WallDurationMS: 60000, TopLevelTurns: 30,
		TreeUsage: state.TokenUsage{InputTokens: 400, OutputTokens: 100},
	})

	return telemetryCompactFixture{
		cfg:                cfg,
		st:                 st,
		oldTelemetryTaskID: oldTaskID,
		currentTaskID:      currentTaskID,
		periodStart:        periodStart,
	}
}

func writeTelemetryCompactStatsArchive(t *testing.T, st *state.StateStore, taskID string, parentThreadID string, startedAt time.Time) {
	t.Helper()
	writeTelemetryCompactStatsArchiveWith(t, st, taskID, startedAt, func(stats *state.TaskStats) {
		stats.ParentCodexThreadID = parentThreadID
	})
}

func writeTelemetryCompactStatsArchiveWith(t *testing.T, st *state.StateStore, taskID string, startedAt time.Time, update func(*state.TaskStats)) {
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
	if update != nil {
		update(&stats)
	}
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatal(err)
	}
	path := st.TaskStatsArchivePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
