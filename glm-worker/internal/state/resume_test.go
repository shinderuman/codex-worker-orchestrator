package state

import (
	"os"
	"strings"
	"testing"
)

func TestResumeCheckpointPersists(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	checkpoint := ResumeCheckpoint{
		Stage:          ResumeStageReview,
		Phase:          "reviewer-2",
		Role:           ReviewerRole,
		Model:          "sonnet",
		ReadOnly:       true,
		Effort:         "high",
		Prompt:         "original",
		OriginalPrompt: "original",
		Request:        "request",
		ReviewNumber:   2,
		AutoFixes:      1,
		RateLimited:    true,
		ResetAtCST:     "2026-07-22 14:06:34",
		ResetAtRFC3339: "2026-07-22T14:06:34+08:00",
	}

	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}

	got, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if got.Phase != checkpoint.Phase || got.ResetAtRFC3339 != checkpoint.ResetAtRFC3339 || got.Effort != "high" || got.Model != "sonnet" {
		t.Fatalf("unexpected checkpoint: %#v", got)
	}
	if got.ReportOnly {
		t.Fatalf("report_only既定はfalse: %#v", got)
	}

	data, err := os.ReadFile(st.Path(resumeStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"report_only\": false") {
		t.Fatalf("report_only=falseが明示保存されていません: %s", data)
	}

	checkpoint.Stage = ResumeStageAutoFix
	checkpoint.Phase = "worker-report-only-1"
	checkpoint.ReadOnly = true
	checkpoint.ReportOnly = true
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	got, err = st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReportOnly || !got.ReadOnly {
		t.Fatalf("report-only checkpoint field round-trip = %#v", got)
	}
}

func TestClearResumeCheckpoint(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker, Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ClearResumeCheckpoint(); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "resumable task is not available") {
		t.Fatalf("clear後のload error = %v", err)
	}
}

func TestLoadResumeCheckpointRejectsCorruptionAndVersion(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := os.WriteFile(st.Path(resumeStateFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "resume stateを読めません") {
		t.Fatalf("corruption error = %v", err)
	}

	if err := os.WriteFile(st.Path(resumeStateFile), []byte("{\"version\":1}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "unsupported resume state version") {
		t.Fatalf("version error = %v", err)
	}
}

func TestResumeCheckpointRequiresModel(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker}); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("save error = %v", err)
	}

	if err := os.WriteFile(st.Path(resumeStateFile), []byte("{\"version\":4,\"stage\":\"worker\",\"report_only\":false}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("load error = %v", err)
	}
}

func TestLoadResumeCheckpointRejectsLegacyVersions(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	for name, legacy := range map[string]string{
		"v2 convertible":           `{"version":2,"stage":"reviewer","phase":"reviewer-1","model":"sonnet","request":"req","worker_packet":["STATUS: IMPLEMENTED","RISK: LOW","SUMMARY: s","TESTS: t"],"packet_compacted":true}`,
		"v2 broken":                `{"version":2,"stage":"worker","model":"opus","worker_packet":["plain text"]}`,
		"v3 report-only no field":  `{"version":3,"stage":"auto-fix","phase":"worker-report-only-1","role":"worker","model":"opus","request":"req","rate_limited":true}`,
		"v3 packet-compact suffix": `{"version":3,"stage":"auto-fix","phase":"worker-report-only-1-packet-compact","role":"worker","model":"opus","request":"req","rate_limited":true}`,
		"v3 ordinary auto-fix":     `{"version":3,"stage":"auto-fix","phase":"worker-auto-fix-1","role":"worker","model":"opus","request":"req","rate_limited":true}`,
	} {
		if err := os.WriteFile(st.Path(resumeStateFile), []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := st.LoadResumeCheckpoint()
		want := "unsupported resume state version: "
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestLoadResumeCheckpointV4RequiresExplicitReportOnly(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	rejected := map[string]struct{ name, want string }{
		`{"version":4,"stage":"auto-fix","phase":"worker-report-only-1","role":"worker","model":"opus","request":"req","rate_limited":true}`:                    {"v4 report-only風phase key欠落", "report_only keyがありません"},
		`{"version":4,"stage":"auto-fix","phase":"worker-auto-fix-1","role":"worker","model":"opus","request":"req","rate_limited":true}`:                       {"v4 通常auto-fix key欠落", "report_only keyがありません"},
		`{"version":4,"stage":"auto-fix","phase":"worker-auto-fix-1","role":"worker","model":"opus","request":"req","rate_limited":true,"report_only":"false"}`: {"v4 report_only非bool", "resume stateを読めません"},
	}
	for doc, tc := range rejected {
		if err := os.WriteFile(st.Path(resumeStateFile), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := st.LoadResumeCheckpoint()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s error = %v", tc.name, err)
		}
	}
	accepted := map[string]bool{
		`{"version":4,"stage":"auto-fix","phase":"worker-auto-fix-1","role":"worker","model":"opus","request":"req","rate_limited":true,"report_only":false}`:   false,
		`{"version":4,"stage":"auto-fix","phase":"worker-report-only-1","role":"worker","model":"opus","request":"req","rate_limited":true,"report_only":true}`: true,
	}
	for doc, wantReportOnly := range accepted {
		if err := os.WriteFile(st.Path(resumeStateFile), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Fatalf("明示report_only=%vの受理が必要: %v", wantReportOnly, err)
		}
		if checkpoint.ReportOnly != wantReportOnly {
			t.Fatalf("report_only = %v, want %v", checkpoint.ReportOnly, wantReportOnly)
		}
	}
}

func TestResumeCheckpointStopParentFilesRoundTrip(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	stop := &ParentFileStates{
		{Path: ParentRulesFile, Exists: true, SHA256: "rules-sha"},
		{Path: ParentPlanFile, Exists: true, SHA256: "plan-sha"},
		{Path: ParentTasksDir + "/001-active.md", Exists: true, SHA256: "task-sha"},
		{Path: ParentHistoryFile},
	}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{
		Stage:           ResumeStageReview,
		Phase:           "reviewer-1",
		Model:           "sonnet",
		RateLimited:     true,
		StopParentFiles: stop,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if got.StopParentFiles == nil || !SameParentFileStates(*got.StopParentFiles, *stop) {
		t.Fatalf("stop parent files round-trip = %#v", got.StopParentFiles)
	}
}

func TestResumeCheckpointWithoutStopParentFiles(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{
		Stage:       ResumeStageReview,
		Phase:       "reviewer-1",
		Model:       "sonnet",
		RateLimited: true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if got.StopParentFiles != nil {
		t.Fatalf("stop_parent_files未設定時はnil: %#v", got.StopParentFiles)
	}
}

func TestResumeCheckpointTwoFileStopParentFilesFailsClosed(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	legacy := `{"version":4,"stage":"reviewer","phase":"reviewer-1","role":"reviewer","model":"sonnet","prompt":"p","request":"r","rate_limited":true,"report_only":false,"stop_parent_files":{"plan":{"sha256":"a"},"history":{"sha256":"b"}}}`
	if err := os.WriteFile(st.Path(resumeStateFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "resume stateを読めません") {
		t.Fatalf("2file形式stop_parent_filesは読込失敗が必要: %v", err)
	}
}
