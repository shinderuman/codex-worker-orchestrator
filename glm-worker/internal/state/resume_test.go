package state

import (
	"os"
	"strings"
	"testing"
	"time"
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
		StopKind:       ResumeStopRateLimited,
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
	if got.Phase != checkpoint.Phase || got.ResetAtRFC3339 != checkpoint.ResetAtRFC3339 || got.Effort != "high" || got.Model != "sonnet" || got.StopKind != ResumeStopRateLimited {
		t.Fatalf("unexpected checkpoint: %#v", got)
	}
	if got.ReportOnly {
		t.Fatalf("report_only既定はfalse: %#v", got)
	}

	data, err := os.ReadFile(st.Path(resumeStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"report_only\": false") || !strings.Contains(string(data), "\"stop_kind\": \"rate-limited\"") {
		t.Fatalf("v6 required keysが明示保存されていません: %s", data)
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

func TestResumeStopKindsRoundTripAndMappings(t *testing.T) {
	cases := []struct {
		kind   ResumeStopKind
		status TaskStatus
		source string
	}{
		{kind: ResumeStopRateLimited, status: TaskStatusRateLimited, source: "rate-limit"},
		{kind: ResumeStopProviderUnavailable, status: TaskStatusProviderUnavailable, source: "provider-unavailable"},
		{kind: ResumeStopInterrupted, status: TaskStatusInterrupted, source: "user-interrupt"},
		{kind: ResumeStopGuardRecoverable, status: TaskStatusGuardRecoverable, source: "guard-recovery"},
	}
	for _, tc := range cases {
		t.Run(string(tc.kind), func(t *testing.T) {
			st := &StateStore{dir: t.TempDir()}
			checkpoint := ResumeCheckpoint{Stage: ResumeStageWorker, Model: "opus", StopKind: tc.kind}
			switch tc.kind {
			case ResumeStopRateLimited:
				checkpoint.ResetAtCST = "2026-07-22 14:06:34"
			case ResumeStopProviderUnavailable:
				checkpoint.ProviderUnavailableClassification = "transient"
				checkpoint.ProviderUnavailableProbes = 2
				checkpoint.ProviderUnavailableStartedAt = time.Date(2026, 7, 22, 6, 0, 0, 0, time.UTC)
			case ResumeStopGuardRecoverable:
				checkpoint.GuardFailure = "blocked"
			}
			if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
				t.Fatal(err)
			}
			got, err := st.LoadResumeCheckpoint()
			if err != nil {
				t.Fatal(err)
			}
			if got.StopKind != tc.kind || !got.IsStopped() || got.StopKind.TaskStatus() != tc.status || got.StopKind.ResumeSource() != tc.source {
				t.Fatalf("stop mapping round-trip = %#v", got)
			}
		})
	}
}

func TestResumeStopKindRejectsMalformedState(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker, Model: "opus", StopKind: ResumeStopKind("future-stop")}); err == nil || !strings.Contains(err.Error(), "unknown resume stop kind") {
		t.Fatalf("unknown kind save error = %v", err)
	}
	if err := st.SaveResumeCheckpoint(ResumeCheckpoint{Stage: ResumeStageWorker, Model: "opus", StopKind: ResumeStopInterrupted, ResetAtCST: "unexpected"}); err == nil || !strings.Contains(err.Error(), "rate-limit reset metadata") {
		t.Fatalf("mismatched payload save error = %v", err)
	}

	docs := []struct {
		name string
		doc  string
		want string
	}{
		{name: "unknown kind", doc: `{"version":6,"stage":"worker","model":"opus","report_only":false,"stop_kind":"future-stop"}`, want: "unknown resume stop kind"},
		{name: "provider payload mismatch", doc: `{"version":6,"stage":"worker","model":"opus","report_only":false,"stop_kind":"interrupted","provider_unavailable_probes":1}`, want: "provider metadata is present"},
	}
	for _, tc := range docs {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(st.Path(resumeStateFile), []byte(tc.doc), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("load error = %v", err)
			}
		})
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

	if err := os.WriteFile(st.Path(resumeStateFile), []byte("{\"version\":6,\"stage\":\"worker\",\"report_only\":false,\"stop_kind\":\"\"}"), 0o600); err != nil {
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
		"v4 previous runtime":      `{"version":4,"stage":"reviewer","phase":"reviewer-1","role":"reviewer","model":"sonnet","request":"req","rate_limited":true,"report_only":false}`,
		"v5 boolean stops":         `{"version":5,"stage":"reviewer","phase":"reviewer-1","role":"reviewer","model":"sonnet","request":"req","rate_limited":true,"report_only":false}`,
	} {
		if err := os.WriteFile(st.Path(resumeStateFile), []byte(legacy), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := st.LoadResumeCheckpoint()
		if err == nil || !strings.Contains(err.Error(), "unsupported resume state version: ") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestLoadResumeCheckpointV6RequiresExplicitKeys(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	rejected := map[string]struct{ name, want string }{
		`{"version":6,"stage":"worker","model":"opus","stop_kind":"rate-limited"}`:                       {"report_only missing", "report_only keyがありません"},
		`{"version":6,"stage":"worker","model":"opus","report_only":false}`:                              {"stop_kind missing", "stop_kind keyがありません"},
		`{"version":6,"stage":"worker","model":"opus","report_only":"false","stop_kind":"rate-limited"}`: {"report_only non-bool", "resume stateを読めません"},
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
		`{"version":6,"stage":"worker","model":"opus","stop_kind":"rate-limited","report_only":false}`: false,
		`{"version":6,"stage":"worker","model":"opus","stop_kind":"rate-limited","report_only":true}`:  true,
	}
	for doc, wantReportOnly := range accepted {
		if err := os.WriteFile(st.Path(resumeStateFile), []byte(doc), 0o600); err != nil {
			t.Fatal(err)
		}
		checkpoint, err := st.LoadResumeCheckpoint()
		if err != nil {
			t.Fatalf("explicit report_only=%v acceptance required: %v", wantReportOnly, err)
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
		StopKind:        ResumeStopRateLimited,
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
		Stage:    ResumeStageReview,
		Phase:    "reviewer-1",
		Model:    "sonnet",
		StopKind: ResumeStopRateLimited,
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
	malformed := `{"version":6,"stage":"reviewer","phase":"reviewer-1","role":"reviewer","model":"sonnet","prompt":"p","request":"r","stop_kind":"rate-limited","report_only":false,"stop_parent_files":{"plan":{"sha256":"a"},"history":{"sha256":"b"}}}`
	if err := os.WriteFile(st.Path(resumeStateFile), []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "resume stateを読めません") {
		t.Fatalf("2file形式stop_parent_filesは読込失敗が必要: %v", err)
	}
}
