package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestQualityReviewForeignMetadataNamesStayGeneric(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		dirs  bool
	}{
		{name: "ordinary files"},
		{name: "same-named directories", dirs: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initForeignRepository(t)
			for _, path := range []string{state.ParentRulesFile, state.ParentPlanFile, state.ParentHistoryFile} {
				name := filepath.Join(repoRoot, path)
				if tc.dirs {
					if err := os.Mkdir(name, 0o755); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(name, []byte("foreign project metadata\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Join(repoRoot, state.ParentTasksDir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repoRoot, state.ParentTasksDir, "foreign.md"), []byte("not an orchestrator task\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.dirs {
				if _, err := state.CaptureParentFileStates(repoRoot); err == nil {
					t.Fatal("directory fixture must fail the unscoped parent metadata capture")
				}
			}

			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)
			qualityCalls := 0
			w.qualityGate = func(string) (harnesslint.Report, error) {
				qualityCalls++
				return harnesslint.Report{Status: "pass"}, nil
			}

			if err := w.ExecuteNewTask("foreign request"); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != state.TaskStatusComplete || len(r.prompts) != 2 || qualityCalls != 1 {
				t.Fatalf("generic review failed: status=%s model calls=%v quality calls=%d output=%s", st.TaskStatus(), r.phases, qualityCalls, out.String())
			}
			requireNoRepositoryPolicyInjection(t, r.prompts)
		})
	}
}

func TestQualityReviewInactiveFixerSkipsNamedMetadataCapture(t *testing.T) {
	t.Parallel()
	repoRoot := initForeignRepository(t)
	if err := os.Mkdir(filepath.Join(repoRoot, state.ParentRulesFile), 0o755); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)
	w.qualityGate = func(root string) (harnesslint.Report, error) {
		before, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("fixed\n"), 0o644); err != nil {
			return harnesslint.Report{}, err
		}
		after, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		return qualityFixReportForSnapshots(before, after), nil
	}
	if err := w.ExecuteNewTask("foreign request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusComplete || len(r.phases) != 2 {
		t.Fatalf("inactive fixer should reach reviewer: status=%s phases=%v output=%s", st.TaskStatus(), r.phases, out.String())
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
		t.Fatalf("review did not use fixer postimage: worker=%+v review=%+v", workerEnd, reviewStart)
	}
}

func TestQualityReviewActiveFixerRejectsParentMetadataChange(t *testing.T) {
	t.Parallel()
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)
	w.qualityGate = func(root string) (harnesslint.Report, error) {
		before, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		if err := mutatePlanFile(root); err != nil {
			return harnesslint.Report{}, err
		}
		after, err := state.CaptureGitSnapshot(root)
		if err != nil {
			return harnesslint.Report{}, err
		}
		return qualityFixReportForSnapshots(before, after), nil
	}
	if err := w.ExecuteNewTask("activated request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview || len(r.phases) != 1 {
		t.Fatalf("active metadata mutation admitted: status=%s phases=%v output=%s", st.TaskStatus(), r.phases, out.String())
	}
	if !strings.Contains(out.String(), "machine quality fixer実行中にparent-managed metadataが変化") {
		t.Fatalf("missing fail-closed metadata mismatch: %s", out.String())
	}
}
