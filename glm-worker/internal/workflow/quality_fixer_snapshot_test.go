package workflow

import (
	"go/format"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityFixSnapshotFeedsReviewer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("initial")},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	path := filepath.Join(w.config.RepoRoot, "fixture.go")
	original := []byte("package fixture\n\nfunc f( ){ }\n")
	formatted, err := format.Source(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(root string) (harnesslint.Report, error) {
		input, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		if err := os.WriteFile(path, formatted, 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		output, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		return qualityFixReportForSnapshots(input, output), nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 2 || r.phases[1] != "reviewer-1" {
		t.Fatalf("machine fix後にreviewerへ進んでいません: %v", r.phases)
	}
	workerEnd, err := st.LoadWorkerEndSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	reviewStart, err := st.LoadReviewStartSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !state.EqualGitSnapshot(workerEnd, reviewStart) {
		t.Fatalf("reviewer基準がmachine fix後snapshotへ揃っていません: worker=%+v review=%+v", workerEnd, reviewStart)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(formatted) {
		t.Fatalf("review対象がformatter適用後内容ではありません: %q", got)
	}
}

func TestQualityFixRejectsUnprovenFixedReport(t *testing.T) {
	for _, status := range []string{"pass", "fail"} {
		t.Run(status, func(t *testing.T) {
			st := newStateStoreT(t)
			r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
			w := newWorkflowT(t, st, r)
			w.collectChangedPaths = func(string, string) ([]string, error) {
				return []string{"fixture.go"}, nil
			}
			path := filepath.Join(w.config.RepoRoot, "fixture.go")
			if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			w.captureSnapshot = state.CaptureGitSnapshot
			w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
			w.qualityGate = func(string) (harnesslint.Report, error) {
				if err := os.WriteFile(path, []byte("package fixture\n\nvar changed = true\n"), 0o644); err != nil {
					return harnesslint.Report{}, err
				}
				report := harnesslint.Report{Status: status, Fixed: 1, Violations: []harnesslint.Violation{}}
				if status == "fail" {
					report.Violations = []harnesslint.Violation{{Rule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid"}}
				}
				return report, nil
			}
			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.phases) != 1 {
				t.Fatalf("provenance確認前に次phaseへ進んでいます: %v", r.phases)
			}
			if st.TaskStatus() != state.TaskStatusWaitingSolReview {
				t.Fatalf("provenanceのないmachine fixはfail closedすべきです: %s", st.TaskStatus())
			}
		})
	}
}

func TestQualityViolationWithoutFixRejectsExternalChangeBeforeAutoFix(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"fixture.go"}, nil
	}
	path := filepath.Join(w.config.RepoRoot, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(string) (harnesslint.Report, error) {
		if err := os.WriteFile(path, []byte("package fixture\n\nvar changed = true\n"), 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.Report{
			Status: "fail",
			Fixed:  0,
			Violations: []harnesslint.Violation{{
				Rule: "fixture", Path: "fixture.go", Line: 1, Column: 1, Message: "still invalid",
			}},
		}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 1 {
		t.Fatalf("external変更確認前にauto-fixへ進んでいます: %v", r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("fixer由来でないexternal変更はfail closedすべきです: %s", st.TaskStatus())
	}
}

func TestQualityPassWithoutFixDoesNotRebaseExternalChange(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("initial")}}}
	w := newWorkflowT(t, st, r)
	w.collectChangedPaths = func(string, string) ([]string, error) {
		return []string{"fixture.go"}, nil
	}
	path := filepath.Join(w.config.RepoRoot, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w.captureSnapshot = state.CaptureGitSnapshot
	w.captureBoundarySnapshot = state.CaptureRepositoryBoundarySnapshot
	w.qualityGate = func(string) (harnesslint.Report, error) {
		if err := os.WriteFile(path, []byte("package fixture\n\nvar changed = true\n"), 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		return harnesslint.Report{Status: "pass", Fixed: 0, Violations: []harnesslint.Violation{}}, nil
	}

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.phases) != 1 {
		t.Fatalf("fixer根拠のない外部変更後にreviewerを呼んでいます: %v", r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("外部変更はfail closedすべきです: %s", st.TaskStatus())
	}
}

func TestParentFileStatesRequireExactMatch(t *testing.T) {
	before := state.ParentFileStates{{Path: state.ParentPlanFile, Exists: true, SHA256: "before"}}
	after := state.ParentFileStates{{Path: state.ParentPlanFile, Exists: true, SHA256: "after"}}
	if state.SameParentFileStates(before, after) {
		t.Fatal("parent-managed metadataの変更を同一扱いしています")
	}
	if !state.SameParentFileStates(before, before) {
		t.Fatal("同一parent-managed metadataを不一致扱いしています")
	}
}

func qualityFixReportForSnapshots(input, output state.GitSnapshot) harnesslint.Report {
	return harnesslint.Report{
		Status:     "pass",
		Fixed:      1,
		Violations: []harnesslint.Violation{},
		FixEvidence: &harnesslint.FixEvidence{
			Method: harnesslint.FixProvenanceIsolatedPostimageV1,
			Input: &harnesslint.FixInputSnapshot{
				Head:           input.Head,
				IndexDigest:    input.IndexDigest,
				WorktreeDigest: input.WorktreeDigest,
			},
			Output: &harnesslint.FixInputSnapshot{
				Head:           output.Head,
				IndexDigest:    output.IndexDigest,
				WorktreeDigest: output.WorktreeDigest,
			},
		},
	}
}

func TestQualityFixSnapshotRejectsOutputMismatch(t *testing.T) {
	workerEnd := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "before"}
	reviewInput := state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "after-external"}
	report := qualityFixReportForSnapshots(workerEnd, state.GitSnapshot{Head: "head", IndexDigest: "index", WorktreeDigest: "fixer-after"})
	if reason := qualityFixSnapshotMismatchReason(workerEnd, reviewInput, report); reason == "" {
		t.Fatal("fixer return後のexternal worktree changeを受理しています")
	}
}
