package autoresume

import (
	"strings"
	"testing"
)

func TestReadDBRowSqlite3RejectsInvalidIDBeforeLookup(t *testing.T) {
	if _, err := ReadDBRowSqlite3("unused", "bad'id"); err == nil || !strings.Contains(err.Error(), "invalid automation ID") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckCoalesceRejectsSchedulerRruleMismatch(t *testing.T) {
	parent := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	wakeID := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	dir := t.TempDir()
	writeCoalesceTOML(t, dir+"/"+wakeID+"/automation.toml", wakeID, wakeID, "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", coalescePrompt(parent), "20260826T152059")
	row := dbRowAt("20260826T152059")
	row.Rrule = "DTSTART:20260826T152100\nRRULE:FREQ=DAILY;COUNT=1"
	result, err := CheckCoalesce(CoalesceParams{
		ParentThreadID:  parent,
		ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		AutomationsDir:  dir,
		DBPath:          "unused",
	}, fixedDBReader(map[string]DBRow{wakeID: row}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCreateGLMWake || result.Reason != "wake scheduler rrule does not match automation.toml" {
		t.Fatalf("result = %+v", result)
	}
}
