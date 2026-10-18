package workflow

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newUnsealedReentryWorkflow(t *testing.T) (*Workflow, config.AppConfig, *state.StateStore, controller.Admission) {
	t.Helper()
	cfg, st := newWorkflowGuardFixture(t, t.TempDir())
	admission, err := controller.Activate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/root.md", []byte("# root\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := st.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	if err := st.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
		t.Fatal(err)
	}
	return NewWorkflow(cfg, st, nil, io.Discard), cfg, st, admission
}

func unsealedReentryArtifacts(t *testing.T, st *state.StateStore) []string {
	t.Helper()
	dir, err := st.PrepareArtifactDir()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "unsealed-completion-reentry-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

func TestReopenUnsealedParentCompletionRetainsSourceAndReentersFix(t *testing.T) {
	t.Parallel()
	w, cfg, st, admission := newUnsealedReentryWorkflow(t)
	const reason = "candidate accept failed: attempt seal archive roots omitted the advanced base"
	ref, err := w.ReopenUnsealedParentCompletion(admission.Head.ControllerGeneration, reason)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Digest == "" || ref.Kind != "runtime-artifact" || !ref.Required {
		t.Fatalf("retained evidence ref = %#v", ref)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("reopened status = %s", st.TaskStatus())
	}
	binding, err := st.LoadControllerRuntimeBinding()
	if err != nil || binding.AttemptID != admission.Attempt.AttemptID || binding.TaskPath != admission.Attempt.SemanticTaskRef.TaskPath {
		t.Fatalf("reopened runtime binding = %#v err=%v", binding, err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.ControllerGeneration != admission.Head.ControllerGeneration {
		t.Fatalf("reopen changed controller generation: %d -> %d", admission.Head.ControllerGeneration, head.ControllerGeneration)
	}
	retained, err := store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(retained, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["reason"] != reason || payload["controller_generation"] != float64(admission.Head.ControllerGeneration) || payload["lease_id"] != admission.Lease.LeaseID {
		t.Fatalf("retained reentry payload = %#v", payload)
	}
	artifacts := unsealedReentryArtifacts(t, st)
	if len(artifacts) != 1 || !strings.HasSuffix(filepath.Base(artifacts[0]), ref.Digest+".json") {
		t.Fatalf("reentry artifact files = %v ref=%s", artifacts, ref.Digest)
	}
	if data, err := os.ReadFile(artifacts[0]); err != nil || string(data) != string(retained) {
		t.Fatalf("reentry artifact differs from retained evidence: %v", err)
	}
}

func TestReopenUnsealedParentCompletionRejectsUnsafeAdmissionWithoutChangingSource(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		message string
		arrange func(w *Workflow, admission controller.Admission)
	}{
		{name: "stale generation", message: "requires exact live authority"},
		{name: "blank reason", message: "requires exact live authority"},
		{
			name:    "different attempt binding",
			message: "runtime binding differs",
			arrange: func(w *Workflow, admission controller.Admission) {
				other := admission
				other.Attempt.AttemptID = "other-attempt"
				w.admitCanonicalMutation = func() error {
					w.canonicalAdmission = &other
					return nil
				}
			},
		},
		{
			name:    "different task binding",
			message: "runtime binding differs",
			arrange: func(w *Workflow, admission controller.Admission) {
				other := admission
				other.Attempt.SemanticTaskRef.TaskPath = "IMPLEMENTATION_TASKS/other.md"
				w.admitCanonicalMutation = func() error {
					w.canonicalAdmission = &other
					return nil
				}
			},
		},
		{
			name:    "sealed attempt",
			message: "exact idle live attempt",
			arrange: func(w *Workflow, admission controller.Admission) {
				sealed := admission
				sealed.Attempt.AttemptSealID = "seal-1"
				w.admitCanonicalMutation = func() error {
					w.canonicalAdmission = &sealed
					return nil
				}
			},
		},
		{
			name:    "in-flight lease",
			message: "exact idle live attempt",
			arrange: func(w *Workflow, admission controller.Admission) {
				busy := admission
				busy.Lease.InFlightCallID = "call-live"
				w.admitCanonicalMutation = func() error {
					w.canonicalAdmission = &busy
					return nil
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, cfg, st, admission := newUnsealedReentryWorkflow(t)
			if tc.arrange != nil {
				tc.arrange(w, admission)
			}
			generation := admission.Head.ControllerGeneration
			reason := "accept failed"
			switch tc.name {
			case "stale generation":
				generation++
			case "blank reason":
				reason = "  "
			}
			if _, err := w.ReopenUnsealedParentCompletion(generation, reason); err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unsafe reentry = %v", err)
			}
			if st.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
				t.Fatalf("rejected reentry changed status: %s", st.TaskStatus())
			}
			if open, err := st.CurrentParentReview(); err != nil || open != nil {
				t.Fatalf("rejected reentry opened a review: %#v err=%v", open, err)
			}
			if artifacts := unsealedReentryArtifacts(t, st); len(artifacts) != 0 {
				t.Fatalf("rejected reentry retained artifacts: %v", artifacts)
			}
			store, err := controller.Open(cfg)
			if err != nil {
				t.Fatal(err)
			}
			head, err := store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if head.ControllerGeneration != admission.Head.ControllerGeneration {
				t.Fatalf("rejected reentry changed controller generation: %d", head.ControllerGeneration)
			}
		})
	}

	t.Run("without live controller", func(t *testing.T) {
		cfg, st := newWorkflowGuardFixture(t, t.TempDir())
		w := NewWorkflow(cfg, st, nil, io.Discard)
		if _, err := w.ReopenUnsealedParentCompletion(1, "accept failed"); err == nil || !strings.Contains(err.Error(), "requires exact live authority") {
			t.Fatalf("reentry without live controller = %v", err)
		}
	})
}
