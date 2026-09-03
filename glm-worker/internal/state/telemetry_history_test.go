package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

const (
	telemetryHistoryTaskMixed   = "22222222-2222-4222-8222-222222222222"
	telemetryHistoryTaskOld     = "11111111-1111-4111-8111-111111111111"
	telemetryHistoryTaskCurrent = "33333333-3333-4333-8333-333333333333"
)

func telemetryHistoryBase() time.Time {
	return time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
}

func oldCohortTaskRecord(taskID string, callID string, startedAt time.Time, turns int, withUsage bool) string {
	record := map[string]any{
		"version":          ModelCallLogVersion,
		"call_id":          callID,
		"call_type":        CallTypeTask,
		"task_id":          taskID,
		"session_id":       "sess-old",
		"started_at":       startedAt.Format(time.RFC3339),
		"phase":            "worker-new",
		"role":             string(WorkerRole),
		"model_alias":      "opus",
		"outcome":          "success",
		"top_level_turns":  turns,
		"wall_duration_ms": 60000,
		"prompt":           "raw-old-prompt-must-not-leak",
	}
	if withUsage {
		record["tree_usage"] = map[string]any{"input_tokens": 100, "cache_read_input_tokens": 20, "output_tokens": 50}
		record["resolved_model_usage"] = map[string]any{
			"glm-5.3": map[string]any{"input_tokens": 100, "output_tokens": 50},
		}
	}
	return marshalTelemetryHistoryLine(record)
}

func currentCohortTaskRecord(callID string, startedAt time.Time) string {
	return marshalTelemetryHistoryLine(map[string]any{
		"version":          ModelCallLogVersion,
		"schema_revision":  modelCallLogSchemaRevision,
		"call_id":          callID,
		"call_type":        CallTypeTask,
		"task_id":          telemetryHistoryTaskCurrent,
		"session_id":       "sess-current",
		"started_at":       startedAt.Format(time.RFC3339),
		"phase":            "worker-new",
		"role":             string(WorkerRole),
		"model_alias":      "haiku",
		"outcome":          "success",
		"top_level_turns":  30,
		"wall_duration_ms": 30000,
		"tree_usage":       map[string]any{"input_tokens": 10, "output_tokens": 5},
		"prompt":           "raw-current-prompt-must-not-leak",
	})
}

func marshalTelemetryHistoryLine(record map[string]any) string {
	data, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func writeTelemetryHistoryFile(t *testing.T, st *StateStore, taskID string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(st.Path("telemetry"), 0o700); err != nil {
		t.Fatal(err)
	}
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	if err := os.WriteFile(st.ModelCallLogPath(taskID), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeMixedTelemetryHistoryFixture(t *testing.T, st *StateStore) {
	t.Helper()
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskMixed,
		oldCohortTaskRecord(telemetryHistoryTaskMixed, "old-call-1", base, 120, true),
		marshalTelemetryHistoryLine(map[string]any{
			"version": ModelCallLogVersion, "call_id": "old-probe-1", "call_type": CallTypeProbe,
			"task_id": telemetryHistoryTaskMixed, "started_at": base.Add(time.Minute).Format(time.RFC3339),
			"model_alias": "opus", "outcome": "probe_success",
		}),
		currentCohortTaskRecord("current-call-1", base.Add(10*time.Minute)),
		`{"version":3,"broken"`,
		`{"call_type":"task"}`,
	)
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskOld,
		oldCohortTaskRecord(telemetryHistoryTaskOld, "old-call-2", base.Add(2*time.Hour), 0, false))
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskCurrent,
		currentCohortTaskRecord("current-call-2", base.Add(3*time.Hour)))
	if err := os.WriteFile(st.Path("telemetry/notes.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findTelemetryHistoryCohort(t *testing.T, scan *TelemetryHistoryScan, version, schemaRevision int) TelemetryCohortScan {
	t.Helper()
	for _, cohort := range scan.Cohorts {
		if cohort.Version == version && cohort.SchemaRevision == schemaRevision {
			return cohort
		}
	}
	t.Fatalf("cohort version=%d schema_revision=%dがありません: %+v", version, schemaRevision, scan.Cohorts)
	return TelemetryCohortScan{}
}

func TestScanTelemetryHistorySeparatesCohorts(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeMixedTelemetryHistoryFixture(t, st)

	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != "ok" || scan.Dir != st.Path("telemetry") {
		t.Fatalf("scan status/dir = %s / %s", scan.Status, scan.Dir)
	}
	if scan.FilesConsidered != 4 || len(scan.IgnoredFiles) != 1 {
		t.Fatalf("considered=%d ignored=%#v", scan.FilesConsidered, scan.IgnoredFiles)
	}
	if len(scan.Cohorts) != 2 {
		t.Fatalf("cohorts = %#v", scan.Cohorts)
	}

	oldCohort := findTelemetryHistoryCohort(t, scan, ModelCallLogVersion, 0)
	if oldCohort.ExcludedReason != "" || oldCohort.Aggregates == nil {
		t.Fatalf("旧cohortが集計対象ではありません: %+v", oldCohort)
	}
	if oldCohort.Files != 2 || oldCohort.Tasks != 2 {
		t.Fatalf("旧cohort files/tasks = %d / %d: %+v", oldCohort.Files, oldCohort.Tasks, oldCohort)
	}
	if fmt.Sprint(oldCohort.FileNames) != fmt.Sprint([]string{telemetryHistoryTaskOld + ".jsonl", telemetryHistoryTaskMixed + ".jsonl"}) {
		t.Fatalf("旧cohortのsource locator = %#v", oldCohort.FileNames)
	}
	if oldCohort.Records.Read != 3 || oldCohort.Records.Task != 2 || oldCohort.Records.Probe != 1 {
		t.Fatalf("旧cohort records = %+v", oldCohort.Records)
	}
	if oldCohort.FirstStartedAt == nil || !oldCohort.FirstStartedAt.Equal(telemetryHistoryBase()) {
		t.Fatalf("旧cohort first_started_at = %v", oldCohort.FirstStartedAt)
	}
	if oldCohort.LastStartedAt == nil || !oldCohort.LastStartedAt.Equal(telemetryHistoryBase().Add(2*time.Hour)) {
		t.Fatalf("旧cohort last_started_at = %v", oldCohort.LastStartedAt)
	}
	coverage := oldCohort.Coverage
	if coverage.TaskCallsWithTurns != 1 || coverage.TaskCallsWithDuration != 2 ||
		coverage.TaskCallsWithUsage != 1 || coverage.TaskCallsMissingUsage != 1 || coverage.UsageTotalsKnown {
		t.Fatalf("旧cohort coverage = %+v", coverage)
	}
	aggregates := oldCohort.Aggregates
	if aggregates.ModelCalls != 2 || aggregates.ModelCallsByAlias["opus"] != 2 {
		t.Fatalf("旧cohort model calls = %d %+v", aggregates.ModelCalls, aggregates.ModelCallsByAlias)
	}
	if aggregates.InputTokensByAlias["opus"] != 100 || aggregates.CacheReadInputTokensByAlias["opus"] != 20 ||
		aggregates.OutputTokensByAlias["opus"] != 50 {
		t.Fatalf("usage不明recordをゼロ補間していなかった: %+v", aggregates)
	}
	if aggregates.TopLevelTurnsByAlias["opus"] != 120 || aggregates.ModelDurationMSByAlias["opus"] != 120000 {
		t.Fatalf("旧cohort turns/duration = %+v / %+v", aggregates.TopLevelTurnsByAlias, aggregates.ModelDurationMSByAlias)
	}
	if aggregates.CallTreesByResolvedModel["glm-5.3"] != 1 || aggregates.InputTokensByResolvedModel["glm-5.3"] != 100 {
		t.Fatalf("旧cohort resolved model = %+v", aggregates.CallTreesByResolvedModel)
	}

	currentCohort := findTelemetryHistoryCohort(t, scan, ModelCallLogVersion, modelCallLogSchemaRevision)
	if !currentCohort.CurrentSchema || currentCohort.ExcludedReason != TelemetryExclusionCurrentSchema {
		t.Fatalf("current cohortの除外分類 = %+v", currentCohort)
	}
	if currentCohort.Aggregates != nil {
		t.Fatalf("current cohortをhistory集計へ混ぜています: %+v", currentCohort.Aggregates)
	}
	if currentCohort.Records.Read != 2 || currentCohort.Files != 2 {
		t.Fatalf("current cohort records/files = %+v / %d", currentCohort.Records, currentCohort.Files)
	}

	if scan.Malformed.Count != 2 ||
		scan.Malformed.ByReason[telemetryMalformedReasonDecode] != 1 ||
		scan.Malformed.ByReason[telemetryMalformedReasonHeader] != 1 {
		t.Fatalf("malformed = %+v", scan.Malformed)
	}

	cohortLogs := scan.HistoryCohortLogs()
	if len(cohortLogs) != 1 {
		t.Fatalf("history cohort logs = %#v", cohortLogs)
	}
	if cohortLogs[0].Version != ModelCallLogVersion || cohortLogs[0].SchemaRevision != 0 {
		t.Fatalf("history cohort logsのcohort key = %+v", cohortLogs[0])
	}
	readTotal := 0
	for _, task := range cohortLogs[0].Logs {
		if task.TaskID == telemetryHistoryTaskCurrent {
			t.Fatalf("current cohort taskがhistory logsへ混ざっています: %#v", task)
		}
		readTotal += len(task.Logs)
	}
	if readTotal != 3 {
		t.Fatalf("history logsのrecord数 = %d", readTotal)
	}
}

func TestScanTelemetryHistoryTaskAndPeriodFilter(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeMixedTelemetryHistoryFixture(t, st)

	taskFiltered, err := st.ScanTelemetryHistory(TelemetryQueryFilter{TaskID: telemetryHistoryTaskOld})
	if err != nil {
		t.Fatal(err)
	}
	if taskFiltered.FilesConsidered != 1 || len(taskFiltered.Cohorts) != 1 {
		t.Fatalf("task filter後 = considered %d cohorts %#v", taskFiltered.FilesConsidered, taskFiltered.Cohorts)
	}
	if taskFiltered.Cohorts[0].Records.Task != 1 || taskFiltered.Cohorts[0].Files != 1 {
		t.Fatalf("task filter後のcohort = %+v", taskFiltered.Cohorts[0])
	}
	if taskFiltered.RecordsOutsidePeriod != 0 {
		t.Fatalf("periodなしqueryで除外recordがあります: %d", taskFiltered.RecordsOutsidePeriod)
	}

	period := TelemetryQueryFilter{
		Since: telemetryHistoryBase().Add(30 * time.Minute),
		Until: telemetryHistoryBase().Add(4 * time.Hour),
	}
	periodFiltered, err := st.ScanTelemetryHistory(period)
	if err != nil {
		t.Fatal(err)
	}
	if periodFiltered.RecordsOutsidePeriod != 3 {
		t.Fatalf("期間外record = %d", periodFiltered.RecordsOutsidePeriod)
	}
	oldCohort := findTelemetryHistoryCohort(t, periodFiltered, ModelCallLogVersion, 0)
	if oldCohort.Records.Read != 1 || oldCohort.Files != 1 || oldCohort.Tasks != 1 {
		t.Fatalf("期間filter後の旧cohort = %+v", oldCohort)
	}
	if oldCohort.Coverage.TaskCallsWithUsage != 0 || oldCohort.Coverage.TaskCallsMissingUsage != 1 {
		t.Fatalf("期間filter後のcoverage = %+v", oldCohort.Coverage)
	}
	currentCohort := findTelemetryHistoryCohort(t, periodFiltered, ModelCallLogVersion, modelCallLogSchemaRevision)
	if currentCohort.Records.Read != 1 {
		t.Fatalf("期間filter後のcurrent cohort = %+v", currentCohort)
	}
}

func TestScanTelemetryHistoryExcludesNewerSchemaCohort(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskCurrent,
		marshalTelemetryHistoryLine(map[string]any{
			"version": ModelCallLogVersion + 1, "call_id": "future-call", "call_type": CallTypeTask,
			"task_id": telemetryHistoryTaskCurrent, "started_at": base.Format(time.RFC3339),
			"model_alias": "opus", "top_level_turns": 5, "tree_usage": map[string]any{"input_tokens": 9},
		}),
		marshalTelemetryHistoryLine(map[string]any{
			"version": ModelCallLogVersion, "schema_revision": modelCallLogSchemaRevision + 1,
			"call_id": "future-revision", "call_type": CallTypeTask,
			"task_id": telemetryHistoryTaskCurrent, "started_at": base.Format(time.RFC3339),
			"model_alias": "opus", "top_level_turns": 5,
		}),
	)

	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Cohorts) != 2 {
		t.Fatalf("cohorts = %#v", scan.Cohorts)
	}
	for _, cohort := range scan.Cohorts {
		if cohort.ExcludedReason != TelemetryExclusionNewerSchema || cohort.Aggregates != nil {
			t.Fatalf("新規schema cohortを集計しました: %+v", cohort)
		}
	}
	if logs := scan.HistoryCohortLogs(); len(logs) != 0 {
		t.Fatalf("新規schema recordがhistory logsへ混ざっています: %#v", logs)
	}
}

func TestScanTelemetryHistoryIsReadOnlyAndPartialOnUnreadableFile(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	writeMixedTelemetryHistoryFixture(t, st)
	if err := os.Remove(st.ModelCallLogPath(telemetryHistoryTaskMixed)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(st.ModelCallLogPath(telemetryHistoryTaskMixed), 0o700); err != nil {
		t.Fatal(err)
	}

	before := telemetryHistoryContentDigests(t, st)
	if err := os.Chmod(st.Path("telemetry"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(st.Path("telemetry"), 0o700) })

	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != "partial" || len(scan.UnreadableFiles) != 1 {
		t.Fatalf("unreadable file後 = %s %#v", scan.Status, scan.UnreadableFiles)
	}
	if scan.UnreadableFiles[0].File != telemetryHistoryTaskMixed+".jsonl" || scan.UnreadableFiles[0].Error == "" {
		t.Fatalf("unreadable entry = %#v", scan.UnreadableFiles[0])
	}
	oldCohort := findTelemetryHistoryCohort(t, scan, ModelCallLogVersion, 0)
	if oldCohort.Records.Read != 1 {
		t.Fatalf("他fileの集計が落ちています: %+v", oldCohort.Records)
	}
	if digests := telemetryHistoryContentDigests(t, st); fmt.Sprint(digests) != fmt.Sprint(before) {
		t.Fatalf("history走査がtelemetryを書き換えました: %v -> %v", before, digests)
	}
}

func telemetryHistoryContentDigests(t *testing.T, st *StateStore) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(st.Path("telemetry"))
	if err != nil {
		t.Fatal(err)
	}
	digests := make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, readErr := os.ReadFile(st.Path("telemetry/" + entry.Name()))
		if readErr != nil {
			t.Fatal(readErr)
		}
		sum := sha256.Sum256(data)
		digests[entry.Name()] = hex.EncodeToString(sum[:])
	}
	return digests
}
