package workflow

import (
	"bytes"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCaptureRepositoryBoundarySelectsActivationScopedSnapshot(t *testing.T) {
	tests := []struct {
		name         string
		inactive     bool
		wantHead     string
		wantGeneric  int
		wantBoundary int
	}{
		{
			name:         "active repository harness uses parent authority boundary",
			wantHead:     "boundary",
			wantBoundary: 1,
		},
		{
			name:        "inactive repository harness uses generic git boundary",
			inactive:    true,
			wantHead:    "generic",
			wantGeneric: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStateStoreT(t)
			w := newWorkflowT(t, st, &scriptedRunner{})
			if tt.inactive {
				pinRepositoryHarnessInactiveT(t, st)
			}

			genericCalls := 0
			boundaryCalls := 0
			w.captureSnapshot = func(string) (state.GitSnapshot, error) {
				genericCalls++
				return state.GitSnapshot{Head: "generic", WorktreeDigest: "generic-worktree"}, nil
			}
			w.captureBoundarySnapshot = func(string) (state.GitSnapshot, error) {
				boundaryCalls++
				parents := state.ParentFileStates{}
				return state.GitSnapshot{Head: "boundary", WorktreeDigest: "boundary-worktree", ParentFiles: &parents}, nil
			}

			got, err := w.captureRepositoryBoundary()
			if err != nil {
				t.Fatal(err)
			}
			if got.Head != tt.wantHead {
				t.Fatalf("head = %q want %q", got.Head, tt.wantHead)
			}
			if genericCalls != tt.wantGeneric || boundaryCalls != tt.wantBoundary {
				t.Fatalf("capture calls: generic=%d boundary=%d want generic=%d boundary=%d", genericCalls, boundaryCalls, tt.wantGeneric, tt.wantBoundary)
			}
			if tt.inactive && got.ParentFiles != nil {
				t.Fatalf("inactive repository boundary unexpectedly carried parent authority: %#v", got.ParentFiles)
			}
		})
	}
}

func TestReviewResumeInactiveHarnessRejectsCoincidentalParentPathChange(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	var out bytes.Buffer
	w := newReviewResumeWorkflow(t, st, r, &out)
	pinRepositoryHarnessInactiveT(t, st)

	writeRepoParentPlan(t, w.config.RepoRoot, "foreign-plan-at-review-start\n")
	saved := reviewResumeSnapshot("worktree-0", "excluding-1", nil)
	checkpoint := reviewResumeCheckpoint(nil)
	seedReviewResumeStop(t, st, saved, checkpoint)

	writeRepoParentPlan(t, w.config.RepoRoot, "foreign-plan-changed-during-stop\n")
	current := reviewResumeSnapshot("worktree-1", "excluding-1", nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return current, nil }
	w.captureBoundarySnapshot = func(string) (state.GitSnapshot, error) {
		t.Fatal("inactive repository harness must not capture repository-specific parent authority")
		return state.GitSnapshot{}, nil
	}

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	assertReviewResumeStopped(t, st, r, &out)
}
