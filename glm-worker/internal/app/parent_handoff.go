package app

import (
	"errors"
	"os"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentHandoffOutput struct {
	Version          int                      `json:"version"`
	Consistent       bool                     `json:"consistent"`
	Inconsistency    *string                  `json:"inconsistency"`
	TaskID           *string                  `json:"task_id"`
	TaskStatus       *string                  `json:"task_status"`
	RequiredAction   *string                  `json:"required_action"`
	AllowedActions   []string                 `json:"allowed_actions"`
	ResumeKind       *string                  `json:"resume_kind"`
	PendingDecision  bool                     `json:"pending_decision"`
	ParentReviewOpen *string                  `json:"parent_review_open"`
	BaselineHead     *string                  `json:"baseline_head"`
	Snapshot         *state.SnapshotDigest    `json:"snapshot"`
	ArtifactDir      *string                  `json:"artifact_dir"`
	LastMaterial     *parentHandoffMaterial   `json:"last_material"`
	Validation       *parentHandoffValidation `json:"validation"`
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

type parentHandoffValidation struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Status          string `json:"status"`
	Log             string `json:"log,omitempty"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
}

const parentHandoffVersion = 1

func printParentHandoff(st *state.StateStore, stdout interface{ Write([]byte) (int, error) }) error {
	output := buildParentHandoff(st)
	return writeJSON(stdout, output)
}

func buildParentHandoff(st *state.StateStore) parentHandoffOutput {
	taskID := st.ReadOr("task.id", "")
	taskStatus := st.TaskStatus()
	output := parentHandoffOutput{
		Version:          parentHandoffVersion,
		Consistent:       true,
		TaskID:           stringPtr(taskID),
		TaskStatus:       taskStatusPtr(taskStatus),
		AllowedActions:   []string{},
		PendingDecision:  st.Exists("pending-decision"),
		ParentReviewOpen: parentReviewPtr(st.OpenParentReviewLabel()),
		BaselineHead:     stringPtr(st.ReadOr("baseline-head", "")),
	}
	if taskID != "" {
		output.ArtifactDir = stringPtr(st.ArtifactDir(taskID))
	}
	applyParentActionPlan(st, &output)
	applyParentSnapshot(st, &output)
	applyParentLastMaterial(st, taskID, &output)
	output.Validation = latestParentValidation(st)
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

func applyParentSnapshot(st *state.StateStore, output *parentHandoffOutput) {
	repoRoot := st.ReadOr("repo-root", "")
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

func latestParentValidation(st *state.StateStore) *parentHandoffValidation {
	entries, err := os.ReadDir(st.Path(qualityGateRunDirectory))
	if err != nil {
		return nil
	}
	records := make([]qualityGateRunRecord, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !validValidationRunID(entry.Name()) {
			continue
		}
		record, err := readQualityGateRun(st, entry.Name())
		if err == nil {
			records = append(records, record)
		}
	}
	if len(records) == 0 {
		return nil
	}
	sort.Slice(records, func(i, j int) bool { return records[i].StartedAt.Before(records[j].StartedAt) })
	record := records[len(records)-1]
	return &parentHandoffValidation{
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Status:          record.Status,
		Log:             record.Log,
		Head:            record.Head,
		IndexDigest:     record.IndexDigest,
		WorktreeDigest:  record.WorktreeDigest,
	}
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

func isMissingHandoffEvidence(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
