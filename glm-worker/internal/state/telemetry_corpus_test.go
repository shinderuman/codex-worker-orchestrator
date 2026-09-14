package state

import (
	"testing"
	"time"
)

func TestTelemetryCorpusPreservesCurrentAndHistoryValidityBoundaries(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	valid := currentTelemetryHistoryRecord(telemetryHistoryTaskA, "valid", base, 1, true)
	unsupported := telemetryHistoryRecordWithRevision(t, valid, ModelCallLogSchemaRevision+1)
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		valid,
		unsupported,
		`{"version":3,"broken"`,
	)

	current, err := st.ScanTelemetryCurrent(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if current.FilesConsidered != 1 || current.Files != 0 || len(current.Logs) != 0 {
		t.Fatalf("current scan accepted partially malformed task: %#v", current)
	}
	if len(current.UnreadableTasks) != 1 || current.UnreadableTasks[0].TaskID != telemetryHistoryTaskA || current.UnreadableTasks[0].Error == "" {
		t.Fatalf("current unreadable evidence = %#v", current.UnreadableTasks)
	}

	history, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if history.Status != telemetryHistoryStatusOK || history.FilesConsidered != 1 || len(history.Cohorts) != 1 {
		t.Fatalf("history scan = %#v", history)
	}
	if history.Cohorts[0].Records.Read != 1 || history.Cohorts[0].Records.Task != 1 {
		t.Fatalf("history valid records = %#v", history.Cohorts[0].Records)
	}
	if history.Malformed.Count != 2 ||
		history.Malformed.ByReason[telemetryMalformedReasonUnsupportedSchema] != 1 ||
		history.Malformed.ByReason[telemetryMalformedReasonDecode] != 1 {
		t.Fatalf("history malformed evidence = %#v", history.Malformed)
	}
}

func TestTelemetryCorpusClassifiesMalformedRecordsForCurrentAndHistory(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		reason string
	}{
		{name: "blank", line: "", reason: telemetryMalformedReasonDecode},
		{name: "decode malformed", line: `{"version":3,"broken"`, reason: telemetryMalformedReasonDecode},
		{name: "missing version", line: `{"call_type":"task"}`, reason: telemetryMalformedReasonHeader},
		{name: "unsupported schema", line: `{"version":999,"schema_revision":0}`, reason: telemetryMalformedReasonUnsupportedSchema},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &StateStore{dir: t.TempDir()}
			writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA, tt.line)

			current, err := st.ScanTelemetryCurrent(TelemetryQueryFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if current.FilesConsidered != 1 || current.Files != 0 || len(current.Logs) != 0 {
				t.Fatalf("current scan accepted malformed task: %#v", current)
			}
			if len(current.UnreadableTasks) != 1 || current.UnreadableTasks[0].TaskID != telemetryHistoryTaskA || current.UnreadableTasks[0].Error == "" {
				t.Fatalf("current unreadable evidence = %#v", current.UnreadableTasks)
			}

			history, err := st.ScanTelemetryHistory(TelemetryQueryFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if history.FilesConsidered != 1 || history.Malformed.Count != 1 || history.Malformed.ByReason[tt.reason] != 1 {
				t.Fatalf("history malformed evidence = %#v", history)
			}
		})
	}
}

func TestTelemetryCorpusAppliesPeriodFilterOnceForCurrentAndHistory(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "before", base, 1, false),
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "inside", base.Add(2*time.Hour), 1, false),
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "undated", time.Time{}, 1, false),
	)
	filter := TelemetryQueryFilter{Since: base.Add(time.Hour), Until: base.Add(3 * time.Hour)}

	current, err := st.ScanTelemetryCurrent(filter)
	if err != nil {
		t.Fatal(err)
	}
	history, err := st.ScanTelemetryHistory(filter)
	if err != nil {
		t.Fatal(err)
	}
	if current.RecordsOutsidePeriod != 1 || current.RecordsUndatedExcluded != 1 ||
		history.RecordsOutsidePeriod != 1 || history.RecordsUndatedExcluded != 1 {
		t.Fatalf("period counters diverged: current=%#v history=%#v", current, history)
	}
	if len(current.Logs) != 1 || len(current.Logs[0].Logs) != 1 || current.Logs[0].Logs[0].CallID != "inside" {
		t.Fatalf("current filtered logs = %#v", current.Logs)
	}
	if len(history.Cohorts) != 1 || history.Cohorts[0].Records.Read != 1 {
		t.Fatalf("history filtered cohort = %#v", history.Cohorts)
	}
}

func TestTelemetryCorpusCurrentCountersExcludeUnreadableFile(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	base := telemetryHistoryBase()
	writeTelemetryHistoryFile(t, st, telemetryHistoryTaskA,
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "before", base, 1, false),
		currentTelemetryHistoryRecord(telemetryHistoryTaskA, "inside", base.Add(2*time.Hour), 1, false),
		`{"version":3,"broken"`,
	)
	filter := TelemetryQueryFilter{Since: base.Add(time.Hour), Until: base.Add(3 * time.Hour)}

	current, err := st.ScanTelemetryCurrent(filter)
	if err != nil {
		t.Fatal(err)
	}
	if current.Files != 0 || current.RecordsOutsidePeriod != 0 || current.RecordsUndatedExcluded != 0 || len(current.UnreadableTasks) != 1 {
		t.Fatalf("current unreadable task leaked period accounting: %#v", current)
	}

	history, err := st.ScanTelemetryHistory(filter)
	if err != nil {
		t.Fatal(err)
	}
	if history.RecordsOutsidePeriod != 1 || history.RecordsUndatedExcluded != 0 || len(history.Cohorts) != 1 || history.Cohorts[0].Records.Read != 1 {
		t.Fatalf("history line-level period evidence changed: %#v", history)
	}
	if history.Malformed.Count != 1 || history.Malformed.ByReason[telemetryMalformedReasonDecode] != 1 {
		t.Fatalf("history malformed evidence = %#v", history.Malformed)
	}
}
