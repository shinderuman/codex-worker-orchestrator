package state

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

const (
	telemetryHistoryTaskA = "22222222-2222-4222-8222-222222222222"
	telemetryHistoryTaskB = "33333333-3333-4333-8333-333333333333"
)

func telemetryHistoryBase() time.Time {
	return time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)
}

func currentTelemetryHistoryRecord(taskID, callID string, startedAt time.Time, turns int, withUsage bool) string {
	record := ModelCallLog{
		Version:        ModelCallLogVersion,
		CallID:         callID,
		CallType:       CallTypeTask,
		TaskID:         taskID,
		SessionID:      "session-current",
		StartedAt:      startedAt,
		CompletedAt:    startedAt.Add(time.Second),
		Phase:          "worker-new",
		Role:           WorkerRole,
		ModelAlias:     "opus",
		Outcome:        "success",
		TopLevelTurns:  turns,
		WallDurationMS: 60000,
	}
	if withUsage {
		record.TreeUsage = TokenUsage{InputTokens: 100, CacheReadInputTokens: 20, OutputTokens: 50}
		record.ResolvedModelUsage = map[string]ResolvedModelUsage{
			"glm-5.3": {InputTokens: 100, OutputTokens: 50},
		}
	}
	data, err := json.Marshal(record)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func telemetryHistoryRecordWithRevision(t *testing.T, line string, revision int) string {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		t.Fatal(err)
	}
	record["schema_revision"] = revision
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
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

func TestScanTelemetryHistoryReadsCurrentSchemaOnly(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	currentA := currentTelemetryHistoryRecord(telemetryHistoryTaskA, "current-a", base, 12, true)
	currentB := currentTelemetryHistoryRecord(telemetryHistoryTaskB, "current-b", base.Add(time.Hour), 8, false)
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		currentA,
		telemetryHistoryRecordWithRevision(t, currentA, 0),
		telemetryHistoryRecordWithRevision(t, currentA, ModelCallLogSchemaRevision+1),
		`{"version":3,"broken"`,
		`{"call_type":"task"}`,
	)
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskB, currentB)

	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != telemetryHistoryStatusOK || scan.FilesConsidered != 2 {
		t.Fatalf("scan status/files = %s / %d", scan.Status, scan.FilesConsidered)
	}
	if len(scan.Cohorts) != 1 {
		t.Fatalf("cohorts = %#v", scan.Cohorts)
	}
	cohort := scan.Cohorts[0]
	if !cohort.CurrentSchema || cohort.Version != ModelCallLogVersion || cohort.SchemaRevision != ModelCallLogSchemaRevision || cohort.ExcludedReason != "" {
		t.Fatalf("current cohort = %#v", cohort)
	}
	if cohort.Files != 2 || cohort.Tasks != 2 || cohort.Records.Read != 2 || cohort.Records.Task != 2 {
		t.Fatalf("current cohort counts = %#v", cohort)
	}
	if cohort.Aggregates == nil || cohort.Aggregates.ModelCalls != 2 || cohort.Aggregates.ModelCallsByAlias["opus"] != 2 {
		t.Fatalf("current cohort aggregates = %#v", cohort.Aggregates)
	}
	if cohort.Aggregates.InputTokensByAlias["opus"] != 100 || cohort.Aggregates.OutputTokensByAlias["opus"] != 50 {
		t.Fatalf("current cohort usage = %#v", cohort.Aggregates)
	}
	if scan.Malformed.Count != 4 || scan.Malformed.ByReason[telemetryMalformedReasonUnsupportedSchema] != 2 || scan.Malformed.ByReason[telemetryMalformedReasonDecode] != 1 || scan.Malformed.ByReason[telemetryMalformedReasonHeader] != 1 {
		t.Fatalf("malformed = %#v", scan.Malformed)
	}
	logs := scan.HistoryCohortLogs()
	if len(logs) != 1 || logs[0].Version != ModelCallLogVersion || logs[0].SchemaRevision != ModelCallLogSchemaRevision || len(logs[0].Logs) != 2 {
		t.Fatalf("history cohort logs = %#v", logs)
	}
}

func TestScanTelemetryHistoryTaskAndPeriodFilterCurrentSchema(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "a-1", base, 1, false),
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "a-2", base.Add(2*time.Hour), 1, false),
	)
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskB,
		currentTelemetryHistoryRecord(telemetryHistoryTaskB, "b-1", base.Add(3*time.Hour), 1, false),
	)

	taskFiltered, err := st.ScanTelemetryHistory(TelemetryQueryFilter{TaskID: telemetryHistoryTaskA})
	if err != nil {
		t.Fatal(err)
	}
	if taskFiltered.FilesConsidered != 1 || len(taskFiltered.Cohorts) != 1 || taskFiltered.Cohorts[0].Records.Read != 2 {
		t.Fatalf("task filtered scan = %#v", taskFiltered)
	}

	periodFiltered, err := st.ScanTelemetryHistory(TelemetryQueryFilter{
		Since: base.Add(time.Hour),
		Until: base.Add(4 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if periodFiltered.RecordsOutsidePeriod != 1 || len(periodFiltered.Cohorts) != 1 || periodFiltered.Cohorts[0].Records.Read != 2 {
		t.Fatalf("period filtered scan = %#v", periodFiltered)
	}
}

func TestScanTelemetryHistoryExcludesUndatedCurrentRecordWhenPeriodBounded(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "dated", base, 1, false),
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "undated", time.Time{}, 1, false),
	)

	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{Since: base.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if scan.RecordsUndatedExcluded != 1 || len(scan.Cohorts) != 1 || scan.Cohorts[0].Records.Read != 1 {
		t.Fatalf("bounded undated scan = %#v", scan)
	}
}

func TestScanTelemetryHistoryNoFiles(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	scan, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if scan.Status != telemetryHistoryStatusNone || scan.FilesConsidered != 0 || len(scan.Cohorts) != 0 || len(scan.HistoryCohortLogs()) != 0 {
		t.Fatalf("empty history scan = %#v", scan)
	}
}
