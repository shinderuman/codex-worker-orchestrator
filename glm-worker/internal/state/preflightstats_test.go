package state

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordPreflightAttemptAccumulatesBoundedAggregate(t *testing.T) {
	dir := t.TempDir()
	st := &StateStore{dir: dir}
	firstAt := time.Date(2026, 9, 5, 1, 0, 0, 0, time.UTC)
	secondAt := time.Date(2026, 9, 5, 2, 0, 0, 0, time.UTC)
	thirdAt := time.Date(2026, 9, 5, 3, 0, 0, 0, time.UTC)

	st.RecordPreflightAttempt(PreflightOutcomePass, firstAt, 2*time.Millisecond)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordPreflightAttempt(PreflightOutcomeFail, secondAt, 5*time.Millisecond)
	st.RecordPreflightAttempt(PreflightOutcomePass, thirdAt, time.Millisecond)

	stats, err := (&StateStore{dir: dir}).LoadPreflightStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.PassCount != 2 || stats.FailureCount != 1 || stats.AvoidedModelCalls != 1 {
		t.Fatalf("counts = %+v", stats)
	}
	if stats.TotalDurationMS != 8 || stats.MaxDurationMS != 5 || stats.LastDurationMS != 1 {
		t.Fatalf("durations = %+v", stats)
	}
	if stats.LastOutcome != PreflightOutcomePass || !stats.LastAt.Equal(thirdAt) {
		t.Fatalf("last = %+v", stats)
	}
	if _, err := os.Stat(filepath.Join(dir, "telemetry")); !os.IsNotExist(err) {
		t.Fatalf("preflight集計がtelemetry dirへ書かれています: %v", err)
	}
}

func TestRecordPreflightAttemptPreservesUnreadableAggregate(t *testing.T) {
	restore := RedirectStatsWarnings(io.Discard)
	t.Cleanup(restore)
	dir := t.TempDir()
	st := &StateStore{dir: dir}
	future := `{"version":2,"schema_revision":1,"pass_count":9}`
	if err := os.WriteFile(st.Path(preflightStatsFile), []byte(future), 0o600); err != nil {
		t.Fatal(err)
	}

	st.RecordPreflightAttempt(PreflightOutcomeFail, time.Now().UTC(), time.Millisecond)

	data, err := os.ReadFile(st.Path(preflightStatsFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != future {
		t.Fatalf("読めない集計が書き換えられました: %s", data)
	}
	if _, err := st.LoadPreflightStats(); !errors.Is(err, errUnsupportedPreflightStatsVersion) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadPreflightStatsFailsClosedOnMalformedAggregate(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	cases := map[string]string{
		"malformed":       "{not json",
		"future schema":   `{"version":1,"schema_revision":2}`,
		"unknown outcome": `{"version":1,"schema_revision":1,"last_outcome":"warning"}`,
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(st.Path(preflightStatsFile), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := st.LoadPreflightStats(); err == nil {
				t.Fatalf("%sの集計が読めてしまいました", name)
			}
		})
	}
}

func TestRecordPreflightAttemptIgnoresUnknownOutcome(t *testing.T) {
	restore := RedirectStatsWarnings(io.Discard)
	t.Cleanup(restore)
	st := &StateStore{dir: t.TempDir()}

	st.RecordPreflightAttempt("warning", time.Now().UTC(), time.Millisecond)

	if _, err := os.Stat(st.Path(preflightStatsFile)); !os.IsNotExist(err) {
		t.Fatalf("未知のoutcomeで集計が書かれました: %v", err)
	}
}
