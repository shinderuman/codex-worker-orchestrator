package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) ReopenUnsealedParentCompletion(generation uint64, reason string) (controller.EvidenceObjectRef, error) {
	store, admission, err := w.unsealedCompletionAdmission(generation, reason)
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	binding := state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}
	source, err := w.state.CaptureUnsealedParentCompletion(binding)
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	ref, err := w.retainUnsealedCompletionSource(store, admission, source, reason)
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	if err := w.state.ReopenUnsealedParentCompletion(source); err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	return ref, nil
}

func (w *Workflow) unsealedCompletionAdmission(generation uint64, reason string) (*controller.Store, controller.Admission, error) {
	store, head, active, err := w.liveControllerStore()
	if err != nil {
		return nil, controller.Admission{}, err
	}
	if !active || head.ControllerGeneration != generation || head.AcceptedCandidateRef != nil || head.PendingTransitionID != "" || strings.TrimSpace(reason) == "" {
		return nil, controller.Admission{}, fmt.Errorf("unsealed completion reentry requires exact live authority, no candidate or pending transition, and a reason")
	}
	if err := w.admitCanonicalMutation(); err != nil {
		return nil, controller.Admission{}, err
	}
	a := *w.canonicalAdmission
	if a.Head.ControllerGeneration != generation || a.Attempt.AttemptState != controller.AttemptStateLive || a.Attempt.AttemptSealID != "" || a.Lease.InFlightCallID != "" {
		return nil, controller.Admission{}, fmt.Errorf("unsealed completion reentry requires the exact idle live attempt")
	}
	return store, a, nil
}

func (w *Workflow) retainUnsealedCompletionSource(store *controller.Store, a controller.Admission, source json.RawMessage, reason string) (controller.EvidenceObjectRef, error) {
	data, err := json.Marshal(struct {
		Source     json.RawMessage              `json:"source"`
		Generation uint64                       `json:"controller_generation"`
		LeaseID    string                       `json:"lease_id"`
		Snapshot   controller.WorkspaceSnapshot `json:"snapshot"`
		Reason     string                       `json:"reason"`
	}{source, a.Head.ControllerGeneration, a.Lease.LeaseID, a.Snapshot, reason})
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	ref, err := store.PutEvidenceObject("runtime-artifact", "application/json", a.Attempt.AttemptID+":unsealed-completion-reentry", true, data)
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	dir, err := w.state.PrepareArtifactDir()
	if err != nil {
		return controller.EvidenceObjectRef{}, err
	}
	err = os.WriteFile(filepath.Join(dir, "unsealed-completion-reentry-"+ref.Digest+".json"), data, 0o600)
	return ref, err
}
