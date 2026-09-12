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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandTelemetryQueryArgs(t *testing.T) {
	taskID := "22222222-2222-4222-8222-222222222222"
	since := "2026-09-01T00:00:00Z"
	until := "2026-09-02T00:00:00Z"
	sinceAt, err := time.Parse(time.RFC3339, since)
	if err != nil {
		t.Fatal(err)
	}
	untilAt, err := time.Parse(time.RFC3339, until)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		args    []string
		mode    CommandMode
		scope   string
		taskID  string
		since   time.Time
		until   time.Time
		compact bool
	}{
		{name: "stats default", args: []string{"--stats"}, mode: ModeStats, scope: state.TelemetryScopeCurrent},
		{name: "stats history", args: []string{"--stats", "history"}, mode: ModeStats, scope: state.TelemetryScopeHistory},
		{
			name:  "stats history full query",
			args:  []string{"--stats", "history", "--task", taskID, "--since", since, "--until", until},
			mode:  ModeStats,
			scope: state.TelemetryScopeHistory, taskID: taskID, since: sinceAt, until: untilAt,
		},
		{
			name:  "call-outliers current with filters",
			args:  []string{"--call-outliers", "current", "--task", taskID, "--since", since},
			mode:  ModeCallOutliers,
			scope: state.TelemetryScopeCurrent, taskID: taskID, since: sinceAt,
		},
		{name: "stats compact", args: []string{"--stats", "--compact"}, mode: ModeStats, scope: state.TelemetryScopeCurrent, compact: true},
		{
			name:  "stats history compact with filters",
			args:  []string{"--stats", "history", "--task", taskID, "--since", since, "--compact"},
			mode:  ModeStats,
			scope: state.TelemetryScopeHistory, taskID: taskID, since: sinceAt, compact: true,
		},
		{
			name:  "call-outliers history compact before options",
			args:  []string{"--call-outliers", "history", "--compact", "--until", until},
			mode:  ModeCallOutliers,
			scope: state.TelemetryScopeHistory, until: untilAt, compact: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command, err := ParseCommand(test.args)
			if err != nil {
				t.Fatal(err)
			}
			if command.Mode != test.mode {
				t.Fatalf("mode = %d", command.Mode)
			}
			if command.Query.Scope != test.scope {
				t.Fatalf("scope = %q", command.Query.Scope)
			}
			if command.Query.Filter.TaskID != test.taskID {
				t.Fatalf("task = %q", command.Query.Filter.TaskID)
			}
			if !command.Query.Filter.Since.Equal(test.since) || !command.Query.Filter.Until.Equal(test.until) {
				t.Fatalf("period = %v / %v", command.Query.Filter.Since, command.Query.Filter.Until)
			}
			if command.Query.Compact != test.compact {
				t.Fatalf("compact = %v", command.Query.Compact)
			}
		})
	}
}

func TestParseCommandRejectsInvalidTelemetryQuery(t *testing.T) {
	taskID := "22222222-2222-4222-8222-222222222222"
	since := "2026-09-01T00:00:00Z"
	for _, args := range [][]string{
		{"--stats", "bogus"},
		{"--stats", "--task"},
		{"--stats", "--task", "not-a-uuid"},
		{"--stats", "--since", "2026-09-01"},
		{"--stats", "--since", since, "--until", since},
		{"--stats", "--since", since, "--until", "2026-08-31T00:00:00Z"},
		{"--stats", "--unknown", "x"},
		{"--stats", "--task", taskID, "--task", taskID},
		{"--stats", "--since", since, "--since", since},
		{"--stats", "history", "extra"},
		{"--stats", "--compact", "--compact"},
		{"--stats", "--compact", "extra"},
		{"--call-outliers", "history", "--until"},
		{"--call-outliers", "CURRENT"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid queryを受理しました: %#v", args)
		}
	}
}

func TestStatsExplicitCurrentScopeMatchesDefaultOutput(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")

	var defaultOut, explicitOut bytes.Buffer
	if err := printStats(cfg, st, TelemetryQueryArgs{}, &defaultOut); err != nil {
		t.Fatal(err)
	}
	if err := printStats(cfg, st, TelemetryQueryArgs{Scope: state.TelemetryScopeCurrent}, &explicitOut); err != nil {
		t.Fatal(err)
	}
	if defaultOut.String() != explicitOut.String() {
		t.Fatalf("明示current出力がdefaultと異なります: %q vs %q", defaultOut.String(), explicitOut.String())
	}
	decoded := decodeSingleLineJSON(t, defaultOut.String())
	query, _ := decoded["query"].(map[string]any)
	if query["scope"] != state.TelemetryScopeCurrent {
		t.Fatalf("query.scope = %#v", query)
	}
}

func writeStatsHistoryArchive(t *testing.T, st *state.StateStore, taskID string, startedAt time.Time, modelCalls int) {
	t.Helper()
	archive := map[string]any{
		"version":              3,
		"schema_revision":      1,
		"task_id":              taskID,
		"started_at":           startedAt.Format(time.RFC3339Nano),
		"status":               "complete",
		"model_calls":          modelCalls,
		"model_calls_by_alias": map[string]any{"opus": modelCalls},
	}
	data, err := json.Marshal(archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(st.Path("stats/"+taskID+".json")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("stats/"+taskID+".json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStatsCurrentScopeTaskAndPeriodFilter(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	taskEarly := "11111111-1111-4111-8111-111111111111"
	taskLate := "22222222-2222-4222-8222-222222222222"
	orphanTask := "33333333-3333-4333-8333-333333333333"
	writeStatsHistoryArchive(t, st, taskEarly, base, 2)
	writeStatsHistoryArchive(t, st, taskLate, base.Add(48 * time.Hour), 3)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("telemetry/"+taskEarly+".jsonl"), []byte(
		"{\"version\":3,\"schema_revision\":1,\"call_id\":\"early-1\",\"call_type\":\"task\",\"task_id\":\""+taskEarly+"\",\"started_at\":\""+base.Format(time.RFC3339)+"\"}\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("telemetry/"+orphanTask+".jsonl"), []byte("{\"version\":3}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var taskOut bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats", "--task", taskLate})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &taskOut, nil); err != nil {
		t.Fatal(err)
	}
	taskDecoded := decodeSingleLineJSON(t, taskOut.String())
	if taskDecoded["tasks"].(float64) != 1 || taskDecoded["model_calls"].(float64) != 3 {
		t.Fatalf("task filter後のstats = %#v", taskDecoded)
	}
	coverage, _ := taskDecoded["telemetry_coverage"].(map[string]any)
	if coverage["orphan_files"].(float64) != 0 {
		t.Fatalf("task絞り込み時に無関係fileをorphan計上しています: %#v", coverage)
	}

	var periodOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--stats", "--since", base.Add(24 * time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &periodOut, nil); err != nil {
		t.Fatal(err)
	}
	periodDecoded := decodeSingleLineJSON(t, periodOut.String())
	if periodDecoded["tasks"].(float64) != 1 || periodDecoded["model_calls"].(float64) != 3 {
		t.Fatalf("期間filter後のstats = %#v", periodDecoded)
	}
	query, _ := periodDecoded["query"].(map[string]any)
	if query["period_basis"] != telemetryQueryPeriodBasisTask {
		t.Fatalf("period_basis = %#v", query)
	}
	periodCoverage, _ := periodDecoded["telemetry_coverage"].(map[string]any)
	if periodCoverage["orphan_files"].(float64) != 1 {
		t.Fatalf("期間外taskのtelemetry fileをorphan計上しています: %#v", periodCoverage)
	}
	periodTasks, _ := periodCoverage["tasks"].([]any)
	for _, entry := range periodTasks {
		if entry.(map[string]any)["task_id"] == taskEarly {
			t.Fatalf("期間外taskがcoverage行へ残っています: %#v", periodCoverage)
		}
	}

	var defaultOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--stats"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &defaultOut, nil); err != nil {
		t.Fatal(err)
	}
	defaultDecoded := decodeSingleLineJSON(t, defaultOut.String())
	if defaultDecoded["tasks"].(float64) != 2 || defaultDecoded["model_calls"].(float64) != 5 {
		t.Fatalf("無filterのstats = %#v", defaultDecoded)
	}
	defaultCoverage, _ := defaultDecoded["telemetry_coverage"].(map[string]any)
	if defaultCoverage["orphan_files"].(float64) != 1 {
		t.Fatalf("無filterのorphan = %#v", defaultCoverage)
	}
}

func TestStatsCurrentScopeUndatedArchiveAndNanoBounds(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	nanoBound := base.Add(123456789 * time.Nanosecond)
	taskDated := "11111111-1111-4111-8111-111111111111"
	taskUndated := "22222222-2222-4222-8222-222222222222"
	writeStatsHistoryArchive(t, st, taskDated, nanoBound, 2)
	undated := map[string]any{
		"version":              3,
		"schema_revision":      1,
		"task_id":              taskUndated,
		"status":               "complete",
		"model_calls":          5,
		"model_calls_by_alias": map[string]any{"opus": 5},
	}
	data, err := json.Marshal(undated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(st.Path("stats/"+taskUndated+".json")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("stats/"+taskUndated+".json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	var unboundedOut bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &unboundedOut, nil); err != nil {
		t.Fatal(err)
	}
	unbounded := decodeSingleLineJSON(t, unboundedOut.String())
	if unbounded["tasks"].(float64) != 2 || unbounded["model_calls"].(float64) != 7 {
		t.Fatalf("無filterのstats = %#v", unbounded)
	}

	var untilOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--stats", "--until", base.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &untilOut, nil); err != nil {
		t.Fatal(err)
	}
	until := decodeSingleLineJSON(t, untilOut.String())
	if until["tasks"].(float64) != 1 || until["model_calls"].(float64) != 2 {
		t.Fatalf("until-only filterへ日時不明taskが混入しています: %#v", until)
	}

	var nanoOut bytes.Buffer
	cmd, err = ParseCommand([]string{
		"--stats",
		"--since", nanoBound.Format(time.RFC3339Nano),
		"--until", base.Add(time.Hour + 987654321 * time.Nanosecond).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &nanoOut, nil); err != nil {
		t.Fatal(err)
	}
	nano := decodeSingleLineJSON(t, nanoOut.String())
	nanoQuery, _ := nano["query"].(map[string]any)
	if nanoQuery["since"] != "2026-09-01T09:00:00.123456789Z" || nanoQuery["until"] != "2026-09-01T10:00:00.987654321Z" {
		t.Fatalf("nanosecond境界がviewへlosslessに出ていません: %#v", nanoQuery)
	}
	if nano["tasks"].(float64) != 1 || nano["model_calls"].(float64) != 2 {
		t.Fatalf("nanosecond since境界のselection = %#v", nano)
	}

	var untilExactOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--stats", "--until", nanoBound.Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &untilExactOut, nil); err != nil {
		t.Fatal(err)
	}
	untilExact := decodeSingleLineJSON(t, untilExactOut.String())
	untilExactQuery, _ := untilExact["query"].(map[string]any)
	if untilExactQuery["until"] != "2026-09-01T09:00:00.123456789Z" {
		t.Fatalf("until-only nanosecond view = %#v", untilExactQuery)
	}
	if untilExact["tasks"].(float64) != 0 {
		t.Fatalf("nanosecond until境界のselection = %#v", untilExact)
	}
}

func TestStatsHistoryScopeUsesCurrentSchemaOnly(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lines := []string{
		"{\"version\":3,\"call_id\":\"old-1\",\"call_type\":\"task\",\"task_id\":\"22222222-2222-4222-8222-222222222222\",\"started_at\":\"" + base.Format(time.RFC3339) + "\",\"model_alias\":\"opus\",\"top_level_turns\":120,\"wall_duration_ms\":60000,\"tree_usage\":{\"input_tokens\":100,\"output_tokens\":50},\"prompt\":\"raw-old-prompt-must-not-leak\"}",
		"{\"version\":3,\"call_id\":\"old-2\",\"call_type\":\"task\",\"task_id\":\"22222222-2222-4222-8222-222222222222\",\"started_at\":\"" + base.Add(time.Hour).Format(time.RFC3339) + "\",\"model_alias\":\"opus\",\"top_level_turns\":0}",
		"{\"version\":3,\"schema_revision\":1,\"call_id\":\"cur-1\",\"call_type\":\"task\",\"task_id\":\"22222222-2222-4222-8222-222222222222\",\"started_at\":\"" + base.Add(2 * time.Hour).Format(time.RFC3339) + "\",\"model_alias\":\"haiku\",\"top_level_turns\":30,\"tree_usage\":{\"input_tokens\":10}}",
		"{\"version\":3,\"broken\"",
	}
	if err := os.WriteFile(st.Path("telemetry/22222222-2222-4222-8222-222222222222.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--stats", "history"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "raw-old-prompt-must-not-leak") {
		t.Fatalf("history出力へunsupported old promptが漏れています: %s", rendered)
	}
	decoded := decodeSingleLineJSON(t, rendered)
	query, _ := decoded["query"].(map[string]any)
	if query["scope"] != state.TelemetryScopeHistory {
		t.Fatalf("query = %#v", query)
	}
	telemetry, _ := decoded["telemetry"].(map[string]any)
	cohorts, _ := telemetry["cohorts"].([]any)
	if len(cohorts) != 1 {
		t.Fatalf("cohorts = %#v", cohorts)
	}
	currentCohort, _ := cohorts[0].(map[string]any)
	if currentCohort["current_schema"] != true || currentCohort["schema_revision"].(float64) != float64(state.ModelCallLogSchemaRevision) {
		t.Fatalf("current cohort = %#v", currentCohort)
	}
	if currentCohort["records"].(map[string]any)["task_calls"].(float64) != 1 {
		t.Fatalf("current cohort records = %#v", currentCohort)
	}
	aggregates, _ := currentCohort["aggregates"].(map[string]any)
	if aggregates["model_calls"].(float64) != 1 {
		t.Fatalf("current cohort aggregates = %#v", aggregates)
	}
	malformed, _ := telemetry["malformed_records"].(map[string]any)
	if malformed["count"].(float64) != 3 {
		t.Fatalf("unsupported/malformed records = %#v", malformed)
	}
}

func TestCallOutliersHistoryUsesCurrentSchemaPopulation(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	taskID := "44444444-4444-4444-8444-444444444444"
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lines := make([]string, 0, 41)
	for index := 0; index < 20; index++ {
		lines = append(lines, fmt.Sprintf(
			`{"version":3,"call_id":%q,"call_type":"task","task_id":%q,"started_at":%q,"phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":100,"wall_duration_ms":1000,"prompt":"raw-old-prompt-must-not-leak"}`,
			"old-v3", taskID, base.Add(time.Duration(index) * time.Minute).Format(time.RFC3339),
		))
	}
	for index := 0; index < 20; index++ {
		lines = append(lines, fmt.Sprintf(
			`{"version":2,"call_id":%q,"call_type":"task","task_id":%q,"started_at":%q,"phase":"worker-new","role":"worker","model_alias":"opus","top_level_turns":200,"wall_duration_ms":1000}`,
			"old-v2", taskID, base.Add(time.Duration(index) * time.Second).Format(time.RFC3339),
		))
	}
	lines = append(lines, "{\"version\":3,\"schema_revision\":1,\"call_id\":\"cur-only\",\"call_type\":\"task\",\"task_id\":\""+taskID+"\",\"started_at\":\""+base.Add(time.Hour).Format(time.RFC3339)+"\",\"phase\":\"worker-new\",\"role\":\"worker\",\"model_alias\":\"haiku\",\"top_level_turns\":9,\"wall_duration_ms\":1000}")
	if err := os.WriteFile(st.Path("telemetry/"+taskID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{"--call-outliers", "history"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	rendered := out.String()
	if strings.Contains(rendered, "raw-old-prompt-must-not-leak") {
		t.Fatalf("history出力へunsupported old promptが漏れています: %s", rendered)
	}
	decoded := decodeSingleLineJSON(t, rendered)
	query, _ := decoded["query"].(map[string]any)
	if query["scope"] != state.TelemetryScopeHistory {
		t.Fatalf("query = %#v", query)
	}
	reports, _ := decoded["reports"].([]any)
	if len(reports) != 1 {
		t.Fatalf("current-schema report数 = %d: %#v", len(reports), reports)
	}
	report, _ := reports[0].(map[string]any)
	if report["version"].(float64) != float64(state.ModelCallLogVersion) || report["schema_revision"].(float64) != float64(state.ModelCallLogSchemaRevision) {
		t.Fatalf("report cohort key = %#v", report)
	}
	records, _ := report["report"].(map[string]any)["records"].(map[string]any)
	if records["task_calls"].(float64) != 1 {
		t.Fatalf("current schema population = %#v", records)
	}
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["malformed_records"].(map[string]any)["count"].(float64) != 40 {
		t.Fatalf("unsupported old records = %#v", telemetry)
	}
	cohorts, _ := telemetry["cohorts"].([]any)
	if len(cohorts) != 1 || cohorts[0].(map[string]any)["records"].(map[string]any)["task_calls"].(float64) != 1 {
		t.Fatalf("current cohort = %#v", cohorts)
	}
}

func TestCallOutliersCurrentScopePeriodFilter(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-a",
		Role: state.WorkerRole, ModelAlias: "opus", Phase: "worker-new",
		StartedAt: base, CompletedAt: base.Add(time.Minute),
		Outcome: "success", WallDurationMS: 60000, TopLevelTurns: 10,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		Version: 3, CallType: state.CallTypeTask, TaskID: taskID, SessionID: "sess-a",
		Role: state.WorkerRole, ModelAlias: "opus", Phase: "worker-new",
		StartedAt: base.Add(48 * time.Hour), CompletedAt: base.Add(48 * time.Hour).Add(time.Minute),
		Outcome: "success", WallDurationMS: 60000, TopLevelTurns: 20,
	})

	var out bytes.Buffer
	cmd, err := ParseCommand([]string{
		"--call-outliers", "--since", base.Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &out, nil); err != nil {
		t.Fatal(err)
	}
	decoded := decodeSingleLineJSON(t, out.String())
	telemetry, _ := decoded["telemetry"].(map[string]any)
	if telemetry["records_outside_period"].(float64) != 1 {
		t.Fatalf("期間外record = %#v", telemetry)
	}
	if telemetry["files"].(float64) != 1 {
		t.Fatalf("files = %#v", telemetry)
	}
	report, _ := decoded["report"].(map[string]any)
	records, _ := report["records"].(map[string]any)
	if records["task_calls"].(float64) != 1 {
		t.Fatalf("期間filter後の母集団 = %#v", records)
	}
}

func TestCallOutliersUndatedRecordPeriodFilter(t *testing.T) {
	cfg := newAppConfig(t)
	st := state.AttachStateStore(cfg)
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	taskID := "55555555-5555-4555-8555-555555555555"
	base := time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
	lines := []string{
		`{"version":3,"schema_revision":1,"call_id":"dated-in","call_type":"task","task_id":"` + taskID + `","started_at":"` + base.Format(time.RFC3339) + `","model_alias":"opus","top_level_turns":5}`,
		`{"version":3,"schema_revision":1,"call_id":"undated","call_type":"task","task_id":"` + taskID + `","model_alias":"opus","top_level_turns":5}`,
		`{"version":3,"schema_revision":1,"call_id":"dated-out","call_type":"task","task_id":"` + taskID + `","started_at":"` + base.Add(48 * time.Hour).Format(time.RFC3339) + `","model_alias":"opus","top_level_turns":5}`,
	}
	if err := os.WriteFile(st.Path("telemetry/"+taskID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var boundedOut bytes.Buffer
	cmd, err := ParseCommand([]string{"--call-outliers", "--until", base.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &boundedOut, nil); err != nil {
		t.Fatal(err)
	}
	bounded := decodeSingleLineJSON(t, boundedOut.String())
	boundedTelemetry, _ := bounded["telemetry"].(map[string]any)
	if boundedTelemetry["records_outside_period"].(float64) != 1 {
		t.Fatalf("期間外record = %#v", boundedTelemetry)
	}
	if boundedTelemetry["records_undated_excluded"].(float64) != 1 {
		t.Fatalf("日時不明除外record = %#v", boundedTelemetry)
	}
	boundedReport, _ := bounded["report"].(map[string]any)
	boundedRecords, _ := boundedReport["records"].(map[string]any)
	if boundedRecords["task_calls"].(float64) != 1 {
		t.Fatalf("until-only filter後の母集団へ日時不明recordが混入しています: %#v", boundedRecords)
	}

	var historyOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--call-outliers", "history", "--until", base.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &historyOut, nil); err != nil {
		t.Fatal(err)
	}
	history := decodeSingleLineJSON(t, historyOut.String())
	historyTelemetry, _ := history["telemetry"].(map[string]any)
	if historyTelemetry["records_outside_period"].(float64) != 1 {
		t.Fatalf("history期間外record = %#v", historyTelemetry)
	}
	if historyTelemetry["records_undated_excluded"].(float64) != 1 {
		t.Fatalf("history日時不明除外record = %#v", historyTelemetry)
	}

	var unboundedOut bytes.Buffer
	cmd, err = ParseCommand([]string{"--call-outliers"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(cmd, cfg, nil, &unboundedOut, nil); err != nil {
		t.Fatal(err)
	}
	unbounded := decodeSingleLineJSON(t, unboundedOut.String())
	unboundedTelemetry, _ := unbounded["telemetry"].(map[string]any)
	if _, ok := unboundedTelemetry["records_outside_period"]; ok {
		t.Fatalf("無filterで期間外recordがあります: %#v", unboundedTelemetry)
	}
	if _, ok := unboundedTelemetry["records_undated_excluded"]; ok {
		t.Fatalf("無filterで日時不明除外recordがあります: %#v", unboundedTelemetry)
	}
	unboundedReport, _ := unbounded["report"].(map[string]any)
	unboundedRecords, _ := unboundedReport["records"].(map[string]any)
	if unboundedRecords["task_calls"].(float64) != 3 {
		t.Fatalf("無filterの母集団 = %#v", unboundedRecords)
	}
}
