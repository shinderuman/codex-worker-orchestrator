package autoresume

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEvaluateWakeCandidateRejectsSchedulerRruleMismatch(t *testing.T) {
	wakeThread := "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	wakeID := codexWakeKeyPrefix + wakeThread
	wakeAt, err := time.Parse(dtStartLayout, "20260826T152059")
	if err != nil {
		t.Fatal(err)
	}
	toml := AutomationTOML{
		ID:             wakeID,
		Name:           wakeID,
		Status:         "ACTIVE",
		Rrule:          "DTSTART:20260826T152059\nRRULE:FREQ=DAILY;COUNT=1",
		TargetThreadID: wakeThread,
	}
	reader := func(string, string) (DBRow, error) {
		return DBRow{
			ID:         wakeID,
			Status:     "ACTIVE",
			Rrule:      "DTSTART:20260826T152059\nRRULE:FREQ=HOURLY;COUNT=1",
			NextRunAt:  wakeAt.UnixMilli(),
			HasNextRun: true,
		}, nil
	}
	result, err := evaluateWakeCandidate(
		toml,
		CoalesceParams{DBPath: "unused"},
		time.Date(2026, 8, 26, 15, 17, 55, 0, time.UTC),
		reader,
		CoalesceResult{Decision: DecisionCreateGLMWake},
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCreateGLMWake || result.Reason != "wake scheduler rrule does not match automation TOML" {
		t.Fatalf("result = %+v", result)
	}
}

func TestReadDBRowSqlite3RejectsUnsafeAutomationKey(t *testing.T) {
	_, err := ReadDBRowSqlite3("unused", "codex-5h-wake-x';SELECT 1;--")
	if err == nil {
		t.Fatal("unsafe automation key was accepted")
	}
	if !errors.Is(err, ErrDBUnreadable) || !strings.Contains(err.Error(), "invalid automation key format") {
		t.Fatalf("error = %v", err)
	}
}
