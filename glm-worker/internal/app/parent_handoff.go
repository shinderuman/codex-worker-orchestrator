package app

import (
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffOutput struct {
	Version          int                        `json:"version"`
	Consistent       bool                       `json:"consistent"`
	Inconsistency    *string                    `json:"inconsistency"`
	TaskID           *string                    `json:"task_id"`
	TaskStatus       *string                    `json:"task_status"`
	RequiredAction   *string                    `json:"required_action"`
	AllowedActions   []string                   `json:"allowed_actions"`
	ResumeKind       *string                    `json:"resume_kind"`
	PendingDecision  bool                       `json:"pending_decision"`
	ParentReviewOpen *string                    `json:"parent_review_open"`
	Baseline         *state.GitBaselineEvidence `json:"baseline"`
	Snapshot         *state.SnapshotDigest      `json:"snapshot"`
	ArtifactDir      *string                    `json:"artifact_dir"`
	LastMaterial     *parentHandoffMaterial     `json:"last_material"`
	Validations      []parentHandoffValidation  `json:"validations"`
}

type parentHandoffRecoveryOutput struct {
	Version          int                            `json:"version"`
	Projection       string                         `json:"projection"`
	Consistent       bool                           `json:"consistent"`
	TaskID           *string                        `json:"task_id"`
	TaskStatus       *string                        `json:"task_status"`
	RequiredAction   *string                        `json:"required_action"`
	AllowedActions   []string                       `json:"allowed_actions"`
	PendingDecision  bool                           `json:"pending_decision"`
	ParentReviewOpen *string                        `json:"parent_review_open"`
	LastMaterial     *parentHandoffRecoveryMaterial `json:"last_material"`
}

type parentHandoffMaterial struct {
	CallID       *string `json:"call_id"`
	CallType     string  `json:"call_type"`
	Phase        string  `json:"phase"`
	Outcome      string  `json:"outcome,omitempty"`
	PacketStatus string  `json:"packet_status,omitempty"`
	Role         string  `json:"role,omitempty"`
	Model        string  `json:"model,omitempty"`
}

type parentHandoffRecoveryMaterial struct {
	CallID       *string `json:"call_id"`
	CallType     string  `json:"call_type"`
	Phase        string  `json:"phase"`
	Outcome      string  `json:"outcome,omitempty"`
	PacketStatus string  `json:"packet_status,omitempty"`
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

const parentHandoffVersion = 1

func printParentHandoff(st *state.StateStore, stdout io.Writer) error {
	return writeJSON(stdout, buildParentHandoff(st))
}

func printParentHandoffRecovery(st *state.StateStore, stdout io.Writer) error {
	return writeJSON(stdout, projectParentHandoffRecovery(buildParentHandoff(st)))
}

func projectParentHandoffRecovery(output parentHandoffOutput) parentHandoffRecoveryOutput {
	recovery := parentHandoffRecoveryOutput{
		Version:          output.Version,
		Projection:       "recovery",
		Consistent:       output.Consistent,
		TaskID:           output.TaskID,
		TaskStatus:       output.TaskStatus,
		RequiredAction:   output.RequiredAction,
		AllowedActions:   append([]string(nil), output.AllowedActions...),
		PendingDecision:  output.PendingDecision,
		ParentReviewOpen: output.ParentReviewOpen,
	}
	if output.LastMaterial != nil {
		recovery.LastMaterial = &parentHandoffRecoveryMaterial{
			CallID:       output.LastMaterial.CallID,
			CallType:     output.LastMaterial.CallType,
			Phase:        output.LastMaterial.Phase,
			Outcome:      output.LastMaterial.Outcome,
			PacketStatus: output.LastMaterial.PacketStatus,
		}
	}
	return recovery
}

func buildParentHandoff(st *state.StateStore) parentHandoffOutput {
	taskID := st.ReadOr("task.id", "")
	taskStatus := st.TaskStatus()
	repoRoot := st.ReadOr("repo-root", "")
	output := parentHandoffOutput{
		Version:          parentHandoffVersion,
		Consistent:       true,
		TaskID:           stringPtr(taskID),
		TaskStatus:       taskStatusPtr(taskStatus),
		AllowedActions:   []string{},
		PendingDecision:  st.Exists("pending-decision"),
		ParentReviewOpen: parentReviewPtr(st.OpenParentReviewLabel()),
		Baseline:         st.BaselineEvidence(),
		Validations:      []parentHandoffValidation{},
	}
	if taskID != "" {
		output.ArtifactDir = stringPtr(st.ArtifactDir(taskID))
	}
	applyParentActionPlan(st, &output)
	applyParentSnapshot(repoRoot, &output)
	applyParentLastMaterial(st, taskID, &output)
	output.Validations = currentParentValidations(st, repoRoot, output.Snapshot)
	return output
}

func applyParentActionPlan(st *state.StateStore, output *parentHandoffOutput) {
	plan, err := st.ParentActionPlan()
	if err != nil {
		markHandoffInconsistent(output, err.Error())
		return
	}
	required := string(plan.RequiredAction)
	output.RequiredAction = &required
	for _, action := range plan.AllowedActions {
		output.AllowedActions = append(output.AllowedActions, string(action))
	}
	output.ResumeKind = stringPtr(plan.ResumeKind)
}

func applyParentSnapshot(repoRoot string, output *parentHandoffOutput) {
	if repoRoot == "" {
		markHandoffInconsistent(output, "repository root is unavailable")
		return
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		markHandoffInconsistent(output, "current repository snapshot is unavailable: "+err.Error())
		return
	}
	output.Snapshot = &state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
}

func applyParentLastMaterial(st *state.StateStore, taskID string, output *parentHandoffOutput) {
	logs, err := readStatusTelemetry(st, taskID)
	if err != nil || len(logs) == 0 {
		return
	}
	for index := len(logs) - 1; index >= 0; index-- {
		log := logs[index]
		if log.CallType == state.CallTypeProbe {
			continue
		}
		output.LastMaterial = &parentHandoffMaterial{
			CallID:       stringPtr(log.CallID),
			CallType:     string(log.CallType),
			Phase:        log.Phase,
			Outcome:      log.Outcome,
			PacketStatus: log.PacketStatus,
			Role:         string(log.Role),
			Model:        log.ModelAlias,
		}
		return
	}
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

func latestCurrentValidationRuns(st *state.StateStore, repoRoot string, snapshot *state.SnapshotDigest) map[string]qualityGateRunRecord {
	latestByForm := make(map[string]qualityGateRunRecord, len(qualityGateForms))
	entries, err := os.ReadDir(st.Path(qualityGateRunDirectory))
	if err != nil {
		return latestByForm
	}
	for _, entry := range entries {
		if !entry.IsDir() || !validValidationRunID(entry.Name()) {
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

func parentHandoffValidationFromRun(record qualityGateRunRecord) parentHandoffValidation {
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

func qualityGateMatchesHandoff(record qualityGateRunRecord, repoRoot string, snapshot *state.SnapshotDigest) bool {
	return filepath.Clean(record.Repository) == filepath.Clean(repoRoot) &&
		record.Head == snapshot.Head &&
		record.IndexDigest == snapshot.IndexDigest &&
		record.WorktreeDigest == snapshot.WorktreeDigest
}

func parentReviewPtr(label string) *string {
	if label == "" || label == statusNone {
		return nil
	}
	return &label
}

func markHandoffInconsistent(output *parentHandoffOutput, detail string) {
	output.Consistent = false
	if output.Inconsistency == nil {
		output.Inconsistency = stringPtr(detail)
	}
	output.RequiredAction = nil
	output.AllowedActions = []string{}
	output.ResumeKind = nil
}
