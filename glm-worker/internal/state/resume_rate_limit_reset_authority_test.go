package state

import (
	"os"
	"strings"
	"testing"
)

func TestResumeCheckpointPersistsOnlyCanonicalRateLimitResetTime(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{
		Stage:           ResumeStageWorker,
		Model:           "opus",
		StopKind:        ResumeStopRateLimited,
		ResetAtCST:      "2099-01-01 00:00:00",
		ResetAtRFC3339:  "2026-09-12T14:06:34+08:00",
	}

	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.Path(resumeStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\"reset_at_cst\"") {
		t.Fatalf("derived reset_at_cst was persisted: %s", data)
	}
	if !strings.Contains(string(data), "\"reset_at_rfc3339\": \"2026-09-12T14:06:34+08:00\"") {
		t.Fatalf("canonical reset_at_rfc3339 missing: %s", data)
	}

	loaded, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResetAtRFC3339 != "2026-09-12T14:06:34+08:00" {
		t.Fatalf("canonical reset = %q", loaded.ResetAtRFC3339)
	}
	if loaded.ResetAtCST != "2026-09-12 14:06:34" {
		t.Fatalf("derived CST reset = %q", loaded.ResetAtCST)
	}
}

func TestLoadResumeCheckpointRejectsLegacyDivergentResetTimes(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	doc := `{
  "version": 6,
  "stage": "worker",
  "model": "opus",
  "report_only": false,
  "stop_kind": "rate-limited",
  "reset_at_cst": "2026-09-12 14:06:34",
  "reset_at_rfc3339": "2026-09-12T15:06:34+08:00"
}`
	if err := os.WriteFile(st.Path(resumeStateFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "legacy reset_at_cst key") {
		t.Fatalf("legacy divergent reset load error = %v", err)
	}
}

func TestResumeCheckpointRejectsInvalidCanonicalRateLimitResetTime(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{
		Stage:          ResumeStageWorker,
		Model:          "opus",
		StopKind:       ResumeStopRateLimited,
		ResetAtRFC3339: "not-a-timestamp",
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "reset_at_rfc3339 is invalid") {
		t.Fatalf("invalid canonical reset save error = %v", err)
	}
}

func TestResumeCheckpointAllowsUnknownRateLimitResetTime(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{
		Stage:    ResumeStageWorker,
		Model:    "opus",
		StopKind: ResumeStopRateLimited,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResetAtRFC3339 != "" || loaded.ResetAtCST != "" {
		t.Fatalf("unknown reset time must remain absent: %#v", loaded)
	}
}
