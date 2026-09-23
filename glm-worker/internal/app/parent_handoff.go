package app

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentcontinuation"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationsequence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type parentHandoffOutput struct {
	Version                  int                                      `json:"version"`
	Consistent               bool                                     `json:"consistent"`
	Inconsistency            *string                                  `json:"inconsistency"`
	TaskID                   *string                                  `json:"task_id"`
	TaskStatus               *string                                  `json:"task_status"`
	RequiredAction           *string                                  `json:"required_action"`
	AllowedActions           []string                                 `json:"allowed_actions"`
	RequiredActionParameters map[string]string                        `json:"required_action_parameters,omitempty"`
	ResumeKind               *string                                  `json:"resume_kind"`
	PendingDecision          bool                                     `json:"pending_decision"`
	ParentReviewOpen         *string                                  `json:"parent_review_open"`
	Baseline                 *state.GitBaselineEvidence               `json:"baseline"`
	Snapshot                 *state.SnapshotDigest                    `json:"snapshot"`
	ArtifactDir              *string                                  `json:"artifact_dir"`
	LastMaterial             *parentHandoffMaterial                   `json:"last_material"`
	Validations              []parentHandoffValidation                `json:"validations"`
	RoutingEvidence          []parentHandoffRoutingEvidence           `json:"routing_evidence"`
	SessionRotation          *state.SessionRotationProjection         `json:"session_rotation"`
	ParentRequest            *ParentRequestCompletionProjection       `json:"parent_request"`
	Publication              *publicationsequence.PublicationSequence `json:"publication,omitempty"`
}

type parentHandoffRecoveryOutput struct {
	Version                  int                                `json:"version"`
	Projection               string                             `json:"projection"`
	Consistent               bool                               `json:"consistent"`
	TaskID                   *string                            `json:"task_id"`
	TaskStatus               *string                            `json:"task_status"`
	RequiredAction           *string                            `json:"required_action"`
	AllowedActions           []string                           `json:"allowed_actions"`
	RequiredActionParameters map[string]string                  `json:"required_action_parameters,omitempty"`
	PendingDecision          bool                               `json:"pending_decision"`
	ParentReviewOpen         *string                            `json:"parent_review_open"`
	LastMaterial             *parentHandoffRecoveryMaterial     `json:"last_material"`
	SessionRotation          *state.SessionRotationProjection   `json:"session_rotation"`
	ParentRequest            *ParentRequestCompletionProjection `json:"parent_request"`
	GuardFailure             string                             `json:"guard_failure,omitempty"`
	GuardRefChanges          []state.GuardRefChange             `json:"guard_ref_changes,omitempty"`
	GuardRefChangesTruncated bool                               `json:"guard_ref_changes_truncated,omitempty"`
	QualityGateFailure       string                             `json:"quality_gate_failure,omitempty"`
}

type parentHandoffMaterial struct {
	CallID             *string `json:"call_id"`
	CallType           string  `json:"call_type"`
	Phase              string  `json:"phase"`
	Outcome            string  `json:"outcome,omitempty"`
	PacketStatus       string  `json:"packet_status,omitempty"`
	PacketRejectReason string  `json:"-"`
	PacketError        string  `json:"-"`
	Role               string  `json:"role,omitempty"`
	Model              string  `json:"model,omitempty"`
}

type parentHandoffRecoveryMaterial struct {
	CallID             *string `json:"call_id"`
	CallType           string  `json:"call_type"`
	Phase              string  `json:"phase"`
	Outcome            string  `json:"outcome,omitempty"`
	PacketStatus       string  `json:"packet_status,omitempty"`
	PacketRejectReason string  `json:"packet_reject_reason,omitempty"`
	PacketError        string  `json:"packet_error,omitempty"`
}

type parentHandoffValidation struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Status          string `json:"status"`
	WorkingDir      string `json:"working_dir"`
	Log             string `json:"log,omitempty"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
}

type parentHandoffRoutingEvidence struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	WorkingDir      string `json:"working_dir"`
	SnapshotMatch   string `json:"snapshot_match"`
}

const parentHandoffVersion = 3

const (
	routingSnapshotMatchExact              = "exact"
	routingSnapshotMatchParentMetadataOnly = "parent_metadata_only"
)

func projectParentHandoffRecovery(output parentHandoffOutput) parentHandoffRecoveryOutput {
	recovery := parentHandoffRecoveryOutput{
		Version:                  output.Version,
		Projection:               "recovery",
		Consistent:               output.Consistent,
		TaskID:                   output.TaskID,
		TaskStatus:               output.TaskStatus,
		RequiredAction:           output.RequiredAction,
		AllowedActions:           append([]string(nil), output.AllowedActions...),
		RequiredActionParameters: output.RequiredActionParameters,
		PendingDecision:          output.PendingDecision,
		ParentReviewOpen:         output.ParentReviewOpen,
		SessionRotation:          output.SessionRotation,
		ParentRequest:            output.ParentRequest,
	}
	if output.LastMaterial != nil {
		recovery.LastMaterial = recoveryMaterialFromHandoff(output.LastMaterial)
	}
	return recovery
}

func recoveryMaterialFromHandoff(material *parentHandoffMaterial) *parentHandoffRecoveryMaterial {
	if material == nil {
		return nil
	}
	return &parentHandoffRecoveryMaterial{
		CallID:             material.CallID,
		CallType:           material.CallType,
		Phase:              material.Phase,
		Outcome:            material.Outcome,
		PacketStatus:       material.PacketStatus,
		PacketRejectReason: material.PacketRejectReason,
		PacketError:        material.PacketError,
	}
}

func applyParentGuardRecovery(st *state.StateStore, output *parentHandoffRecoveryOutput) {
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusGuardRecoverable) {
		return
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopGuardRecoverable {
		return
	}
	output.GuardFailure = checkpoint.GuardFailure
	if len(checkpoint.GuardRefChanges) != 0 {
		output.GuardRefChanges = append([]state.GuardRefChange(nil), checkpoint.GuardRefChanges...)
	}
	output.GuardRefChangesTruncated = checkpoint.GuardRefChangesTruncated
}

func applyParentQualityGateRecovery(st *state.StateStore, output *parentHandoffRecoveryOutput) {
	if output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusQualityGateRecoverable) {
		return
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopQualityGate {
		return
	}
	output.QualityGateFailure = checkpoint.QualityGateFailure
}

func buildParentHandoffFromContinuation(st *state.StateStore, continuation parentcontinuation.Projection) parentHandoffOutput {
	taskID := st.ReadOr("task.id", "")
	taskStatus := st.TaskStatus()
	repoRoot := st.ReadOr("repo-root", "")
	output := parentHandoffOutput{
		Version:          parentHandoffVersion,
		Consistent:       continuation.Consistent,
		Inconsistency:    continuation.Inconsistency,
		TaskID:           machinecli.StringPtr(taskID),
		TaskStatus:       machinecli.TaskStatusPtr(taskStatus),
		AllowedActions:   []string{},
		PendingDecision:  st.Exists("pending-decision"),
		ParentReviewOpen: parentReviewPtr(st.OpenParentReviewLabel()),
		Baseline:         st.BaselineEvidence(),
		Snapshot:         continuation.Snapshot,
		Validations:      []parentHandoffValidation{},
		RoutingEvidence:  []parentHandoffRoutingEvidence{},
		SessionRotation:  continuation.SessionRotation,
	}
	if taskID != "" {
		output.ArtifactDir = machinecli.StringPtr(st.ArtifactDir(taskID))
	}
	if continuation.ParentRequest != nil {
		request := parentRequestProjectionFromFocused(*continuation.ParentRequest)
		output.ParentRequest = &request
	}
	if continuation.Consistent && continuation.ActionPlan != nil {
		required := string(continuation.ActionPlan.RequiredAction)
		output.RequiredAction = &required
		for _, action := range continuation.ActionPlan.AllowedActions {
			output.AllowedActions = append(output.AllowedActions, string(action))
		}
		if len(continuation.ActionPlan.RequiredActionParameters) != 0 {
			output.RequiredActionParameters = continuation.ActionPlan.RequiredActionParameters
		}
		output.ResumeKind = machinecli.StringPtr(continuation.ActionPlan.ResumeKind)
	}
	applyParentLastMaterial(st, taskID, &output)
	output.Validations = currentParentValidations(st, repoRoot, output.Snapshot)
	output.RoutingEvidence = currentParentRoutingEvidence(st, repoRoot, taskID, output.Snapshot)
	if output.Consistent && (taskStatus == state.TaskStatusAwaitingParentCompletion || taskStatus == state.TaskStatusComplete) {
		sequence := publicationsequence.ProjectPublicationSequence(repoRoot, st)
		output.Publication = &sequence
	}
	return output
}

func applyParentLastMaterial(st *state.StateStore, taskID string, output *parentHandoffOutput) {
	logs, err := taskview.ReadStatusTelemetry(st, taskID)
	if err != nil || len(logs) == 0 {
		return
	}
	for index := len(logs) - 1; index >= 0; index-- {
		if logs[index].CallType == state.CallTypeProbe {
			continue
		}
		output.LastMaterial = parentHandoffMaterialFromLog(logs[index])
		return
	}
}

func parentHandoffMaterialFromLog(log state.ModelCallLog) *parentHandoffMaterial {
	material := &parentHandoffMaterial{
		CallID:       machinecli.StringPtr(log.CallID),
		CallType:     string(log.CallType),
		Phase:        log.Phase,
		Outcome:      log.Outcome,
		PacketStatus: log.PacketStatus,
		Role:         string(log.Role),
		Model:        log.ModelAlias,
	}
	if log.Outcome == "invalid_packet" {
		material.PacketRejectReason = log.PacketRejectReason
		material.PacketError = log.Error
	}
	return material
}

func currentParentValidations(st *state.StateStore, repoRoot string, snapshot *state.SnapshotDigest) []parentHandoffValidation {
	if repoRoot == "" || snapshot == nil {
		return []parentHandoffValidation{}
	}
	latestByForm := latestCurrentValidationRuns(st, repoRoot, snapshot)
	forms := make([]string, 0, len(latestByForm))
	for form := range latestByForm {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	validations := make([]parentHandoffValidation, 0, len(forms))
	for _, form := range forms {
		validations = append(validations, parentHandoffValidationFromRun(latestByForm[form]))
	}
	return validations
}

func latestCurrentValidationRuns(st *state.StateStore, repoRoot string, snapshot *state.SnapshotDigest) map[string]qualitygate.RunRecord {
	latestByForm := make(map[string]qualitygate.RunRecord, len(qualityGateForms))
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		return latestByForm
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := readQualityGateRun(st, entry.Name())
		if err != nil || !qualityGateMatchesHandoff(record, repoRoot, snapshot) {
			continue
		}
		previous, found := latestByForm[record.Form]
		if !found || previous.StartedAt.Before(record.StartedAt) {
			latestByForm[record.Form] = record
		}
	}
	return latestByForm
}

func parentHandoffValidationFromRun(record qualitygate.RunRecord) parentHandoffValidation {
	return parentHandoffValidation{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Status:          record.Status,
		WorkingDir:      record.WorkingDir,
		Log:             record.Log,
		Head:            record.Head,
		IndexDigest:     record.IndexDigest,
		WorktreeDigest:  record.WorktreeDigest,
	}
}

func qualityGateMatchesHandoff(record qualitygate.RunRecord, repoRoot string, snapshot *state.SnapshotDigest) bool {
	return filepath.Clean(record.Repository) == filepath.Clean(repoRoot) &&
		record.Head == snapshot.Head &&
		record.IndexDigest == snapshot.IndexDigest &&
		record.WorktreeDigest == snapshot.WorktreeDigest
}

func currentParentRoutingEvidence(st *state.StateStore, repoRoot, taskID string, snapshot *state.SnapshotDigest) []parentHandoffRoutingEvidence {
	if repoRoot == "" || taskID == "" || snapshot == nil {
		return []parentHandoffRoutingEvidence{}
	}
	latestByForm := latestRoutingEvidenceRuns(st, repoRoot, taskID, snapshot)
	forms := make([]string, 0, len(latestByForm))
	for form := range latestByForm {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	evidence := make([]parentHandoffRoutingEvidence, 0, len(forms))
	for _, form := range forms {
		record := latestByForm[form]
		evidence = append(evidence, parentHandoffRoutingEvidence{
			ValidationRunID: record.ValidationRunID,
			Form:            record.Form,
			WorkingDir:      record.WorkingDir,
			SnapshotMatch:   routingSnapshotMatch(record, repoRoot, snapshot),
		})
	}
	return evidence
}

func latestRoutingEvidenceRuns(st *state.StateStore, repoRoot, taskID string, snapshot *state.SnapshotDigest) map[string]qualitygate.RunRecord {
	latestByForm := make(map[string]qualitygate.RunRecord, len(qualityGateForms))
	entries, err := os.ReadDir(st.Path(qualitygate.RunDirectory))
	if err != nil {
		return latestByForm
	}
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := readQualityGateRun(st, entry.Name())
		if err != nil || record.Status != qualitygate.StatusPass || record.TaskID != taskID {
			continue
		}
		if routingSnapshotMatch(record, repoRoot, snapshot) == "" {
			continue
		}
		previous, found := latestByForm[record.Form]
		if !found || previous.StartedAt.Before(record.StartedAt) {
			latestByForm[record.Form] = record
		}
	}
	return latestByForm
}

func routingSnapshotMatch(record qualitygate.RunRecord, repoRoot string, snapshot *state.SnapshotDigest) string {
	if filepath.Clean(record.Repository) != filepath.Clean(repoRoot) ||
		record.Head != snapshot.Head ||
		record.IndexDigest != snapshot.IndexDigest {
		return ""
	}
	if record.WorktreeDigest == snapshot.WorktreeDigest {
		return routingSnapshotMatchExact
	}
	if record.WorktreeDigestExcludingParent != "" &&
		record.WorktreeDigestExcludingParent == snapshot.WorktreeDigestExcludingParent {
		return routingSnapshotMatchParentMetadataOnly
	}
	return ""
}

func parentReviewPtr(label string) *string {
	if label == "" || label == taskview.StatusNone {
		return nil
	}
	return &label
}
