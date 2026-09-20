package parentactioncmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type publicationGateProjection struct {
	Gate            string `json:"gate"`
	Required        bool   `json:"required"`
	Status          string `json:"status"`
	ValidationRunID string `json:"validation_run_id,omitempty"`
	SnapshotID      string `json:"snapshot_id,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

type publicationReadinessOutput struct {
	Status       string                      `json:"status"`
	CandidateOID string                      `json:"candidate_oid,omitempty"`
	TreeOID      string                      `json:"tree_oid,omitempty"`
	SnapshotID   string                      `json:"snapshot_id,omitempty"`
	Gates        []publicationGateProjection `json:"gates"`
	Installation *publicationInstallOutput   `json:"installation,omitempty"`
	Failure      *finalizationFailure        `json:"failure,omitempty"`
}

type publicationQualityRunRecord struct {
	ValidationRunID               string     `json:"validation_run_id"`
	Form                          string     `json:"form"`
	Repository                    string     `json:"repository"`
	Head                          string     `json:"head"`
	IndexDigest                   string     `json:"index_digest"`
	WorktreeDigest                string     `json:"worktree_digest"`
	WorktreeDigestExcludingParent string     `json:"worktree_digest_excluding_parent,omitempty"`
	TaskID                        string     `json:"task_id,omitempty"`
	CompletedAt                   *time.Time `json:"completed_at,omitempty"`
	Status                        string     `json:"status"`
	ExitCode                      int        `json:"exit_code,omitempty"`
	ExitSource                    string     `json:"exit_source,omitempty"`
	Log                           string     `json:"log,omitempty"`
}

const (
	publicationReadinessReady   = "ready"
	publicationReadinessBlocked = "blocked"
	publicationGatePass         = "pass"
	publicationGateMissing      = "missing"
	publicationGateStale        = "stale"
	publicationGateFail         = "fail"

	publicationReadinessInstallCandidate = "--install-candidate"

	publicationFailureCandidateMissing = "publication_candidate_missing"
	publicationFailureCandidateStale   = "publication_candidate_stale"
	publicationFailureGateMissing      = "publication_required_gate_missing"
	publicationFailureGateFailed       = "publication_required_gate_failed"
)

func runPublicationReadiness(cfg config.AppConfig, args []string, stdout io.Writer) error {
	installCandidate, err := parsePublicationReadinessArgs(args)
	if err != nil {
		return err
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	var installation *publicationInstallOutput
	if installCandidate {
		result := installPublicationCandidate(cfg, st)
		installation = &result
	}
	output := projectPublicationReadiness(cfg, st)
	output.Installation = installation
	return json.NewEncoder(stdout).Encode(output)
}

func parsePublicationReadinessArgs(args []string) (bool, error) {
	if len(args) == 1 && args[0] == "readiness" {
		return false, nil
	}
	if len(args) == 2 && args[0] == "readiness" && args[1] == publicationReadinessInstallCandidate {
		return true, nil
	}
	return false, fmt.Errorf("usage: glm-parent-action push-binding readiness [--install-candidate]")
}

func projectPublicationReadiness(cfg config.AppConfig, st *state.StateStore) publicationReadinessOutput {
	output := publicationReadinessOutput{Status: publicationReadinessBlocked, Gates: []publicationGateProjection{}}
	candidate, err := st.LoadPublicationCandidate()
	if err != nil {
		output.Failure = publicationReadinessFailure(publicationFailureCandidateMissing, err.Error())
		return output
	}
	output.CandidateOID = candidate.CommitOID
	output.TreeOID = candidate.TreeOID
	output.SnapshotID = candidate.SnapshotID

	sourceGate := publicationSourceGate(cfg.RepoRoot, candidate)
	output.Gates = append(output.Gates, sourceGate)
	if sourceGate.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureCandidateStale, sourceGate.Reason)
		return output
	}

	reviewGate := publicationReviewGate(st, candidate)
	output.Gates = append(output.Gates, reviewGate)
	if reviewGate.Status != publicationGatePass {
		output.Failure = publicationReadinessFailure(publicationFailureGateMissing, reviewGate.Reason)
		return output
	}

	if validationGate, required := publicationParentValidationGate(st, cfg.RepoRoot, candidate); required {
		output.Gates = append(output.Gates, validationGate)
		if validationGate.Status != publicationGatePass {
			reason := publicationFailureGateMissing
			if validationGate.Status == publicationGateFail {
				reason = publicationFailureGateFailed
			}
			output.Failure = publicationReadinessFailure(reason, validationGate.Gate+": "+validationGate.Reason)
			return output
		}
	}

	installGates, failure := publicationRuntimeInstallGates(cfg, st, candidate)
	output.Gates = append(output.Gates, installGates...)
	if failure != nil {
		output.Failure = failure
		return output
	}
	output.Status = publicationReadinessReady
	return output
}

func publicationSourceGate(repoRoot string, candidate state.PublicationCandidate) publicationGateProjection {
	gate := publicationGateProjection{Gate: "candidate-source", Required: true, Status: publicationGateStale, SnapshotID: candidate.SnapshotID}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		gate.Reason = err.Error()
		return gate
	}
	current := state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
	if current == candidate.Snapshot {
		tree, err := gitFinalizationOutput(repoRoot, "write-tree")
		if err == nil && strings.TrimSpace(tree) == candidate.TreeOID {
			gate.Status = publicationGatePass
			return gate
		}
		gate.Reason = "current index tree no longer matches publication candidate"
		return gate
	}
	if snapshot.Head == candidate.CommitOID && pushBindingTreeClean(repoRoot) {
		tree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "HEAD^{tree}")
		if err == nil && strings.TrimSpace(tree) == candidate.TreeOID {
			gate.Status = publicationGatePass
			return gate
		}
	}
	gate.Reason = "repository snapshot no longer matches publication candidate"
	return gate
}

func publicationReviewGate(st *state.StateStore, candidate state.PublicationCandidate) publicationGateProjection {
	gate := publicationGateProjection{Gate: "parent-review", Required: true, Status: publicationGateMissing, SnapshotID: candidate.SnapshotID}
	plan, err := st.ParentActionPlan()
	if err != nil {
		gate.Reason = err.Error()
		return gate
	}
	if plan.Allows(state.ParentActionInstall) || plan.Allows(state.ParentActionComplete) {
		gate.Status = publicationGatePass
		return gate
	}
	gate.Reason = "required parent action is " + string(plan.RequiredAction)
	return gate
}

func publicationParentValidationGate(st *state.StateStore, repoRoot string, candidate state.PublicationCandidate) (publicationGateProjection, bool) {
	checkpoint, err := st.LoadResumeCheckpoint()
	if errors.Is(err, state.ErrNoResumeCheckpoint) || (err == nil && checkpoint.ParentValidation == nil) {
		return publicationGateProjection{}, false
	}
	gate := publicationGateProjection{Gate: "parent-validation", Required: true, Status: publicationGateMissing, SnapshotID: candidate.SnapshotID}
	if err != nil {
		gate.Reason = err.Error()
		return gate, true
	}
	gate.Gate = checkpoint.ParentValidation.Form
	snapshots, snapshotErr := publicationValidationSnapshots(repoRoot, candidate)
	if snapshotErr != nil {
		gate.Reason = snapshotErr.Error()
		return gate, true
	}
	event, snapshot, eventErr := publicationValidationEvent(st, candidate, checkpoint.ParentValidation.Form, snapshots)
	if eventErr != nil {
		gate.Reason = eventErr.Error()
		return gate, true
	}
	gate.ValidationRunID = event.ValidationRunID
	gate.SnapshotID = event.SnapshotID
	gate.Status = event.Result
	if gate.Status != publicationGatePass {
		gate.Reason = "candidate-bound quality gate result is " + event.Result
		return gate, true
	}
	if err := verifyPublicationQualityRunForSnapshot(st, repoRoot, candidate, checkpoint.ParentValidation.Form, event.ValidationRunID, event.SnapshotID, snapshot); err != nil {
		gate.Status = publicationGateStale
		gate.Reason = err.Error()
		return gate, true
	}
	return gate, true
}

func publicationValidationSnapshots(repoRoot string, candidate state.PublicationCandidate) (map[string]state.SnapshotDigest, error) {
	snapshots := map[string]state.SnapshotDigest{candidate.SnapshotID: candidate.Snapshot}
	currentRaw, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		return nil, err
	}
	current := state.SnapshotDigest{
		Head:                          currentRaw.Head,
		IndexDigest:                   currentRaw.IndexDigest,
		WorktreeDigest:                currentRaw.WorktreeDigest,
		WorktreeDigestExcludingParent: currentRaw.WorktreeDigestExcludingParent,
	}
	if current.Head != candidate.CommitOID || !pushBindingTreeClean(repoRoot) {
		return snapshots, nil
	}
	tree, err := gitFinalizationOutput(repoRoot, "rev-parse", "--verify", "HEAD^{tree}")
	if err != nil || strings.TrimSpace(tree) != candidate.TreeOID {
		return snapshots, nil
	}
	if snapshotID := state.ValidationSnapshotID(current.Head, current.IndexDigest, current.WorktreeDigest); snapshotID != "" {
		snapshots[snapshotID] = current
	}
	return snapshots, nil
}

func publicationValidationEvent(st *state.StateStore, candidate state.PublicationCandidate, form string, snapshots map[string]state.SnapshotDigest) (state.TaskValidationEvent, state.SnapshotDigest, error) {
	file, err := os.Open(st.TaskEventLogPath(candidate.TaskID))
	if err != nil {
		return state.TaskValidationEvent{}, state.SnapshotDigest{}, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var found *state.TaskValidationEvent
	var foundSnapshot state.SnapshotDigest
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			return state.TaskValidationEvent{}, state.SnapshotDigest{}, err
		}
		if record.Validation == nil || record.Validation.Form != form {
			continue
		}
		snapshot, ok := snapshots[record.Validation.SnapshotID]
		if !ok {
			continue
		}
		copy := *record.Validation
		found = &copy
		foundSnapshot = snapshot
	}
	if err := scanner.Err(); err != nil {
		return state.TaskValidationEvent{}, state.SnapshotDigest{}, err
	}
	if found == nil {
		return state.TaskValidationEvent{}, state.SnapshotDigest{}, fmt.Errorf("no %s validation matches an admitted publication snapshot", form)
	}
	if found.ValidationRunID == "" {
		return state.TaskValidationEvent{}, state.SnapshotDigest{}, fmt.Errorf("matching %s validation has no run identity", form)
	}
	return *found, foundSnapshot, nil
}

func verifyPublicationQualityRun(st *state.StateStore, repoRoot string, candidate state.PublicationCandidate, form, runID string) error {
	return verifyPublicationQualityRunForSnapshot(st, repoRoot, candidate, form, runID, candidate.SnapshotID, candidate.Snapshot)
}

func verifyPublicationQualityRunForSnapshot(st *state.StateStore, repoRoot string, candidate state.PublicationCandidate, form, runID, snapshotID string, snapshot state.SnapshotDigest) error {
	record, err := loadPublicationQualityRun(st, runID)
	if err != nil {
		return err
	}
	if err := verifyPublicationQualityRunSnapshotIdentity(record, repoRoot, candidate.TaskID, form, runID, snapshotID, snapshot); err != nil {
		return err
	}
	return verifyPublicationQualityRunResult(record)
}

func loadPublicationQualityRun(st *state.StateStore, runID string) (publicationQualityRunRecord, error) {
	if !validPublicationValidationRunID(runID) {
		return publicationQualityRunRecord{}, fmt.Errorf("quality gate run identity is invalid")
	}
	data, err := os.ReadFile(st.Path(filepath.Join("quality-gate-runs", runID, "run.json")))
	if err != nil {
		return publicationQualityRunRecord{}, err
	}
	var record publicationQualityRunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return publicationQualityRunRecord{}, err
	}
	return record, nil
}

func verifyPublicationQualityRunIdentity(record publicationQualityRunRecord, repoRoot string, candidate state.PublicationCandidate, form, runID string) error {
	return verifyPublicationQualityRunSnapshotIdentity(record, repoRoot, candidate.TaskID, form, runID, candidate.SnapshotID, candidate.Snapshot)
}

func verifyPublicationQualityRunSnapshotIdentity(record publicationQualityRunRecord, repoRoot, taskID, form, runID, snapshotID string, snapshot state.SnapshotDigest) error {
	if record.ValidationRunID != runID || record.Form != form || record.Repository != repoRoot || record.TaskID != taskID {
		return fmt.Errorf("quality gate run authority does not match publication candidate")
	}
	if record.Head != snapshot.Head || record.IndexDigest != snapshot.IndexDigest || record.WorktreeDigest != snapshot.WorktreeDigest ||
		record.WorktreeDigestExcludingParent != snapshot.WorktreeDigestExcludingParent {
		return fmt.Errorf("quality gate run snapshot does not match publication candidate")
	}
	if state.ValidationSnapshotID(record.Head, record.IndexDigest, record.WorktreeDigest) != snapshotID {
		return fmt.Errorf("quality gate run snapshot identity does not match validation event")
	}
	return nil
}

func verifyPublicationQualityRunResult(record publicationQualityRunRecord) error {
	if record.Status != publicationGatePass || record.CompletedAt == nil || record.ExitCode != 0 || record.ExitSource != state.ValidationExitSourceTarget {
		return fmt.Errorf("quality gate run did not complete with target-process PASS")
	}
	if record.Log == "" {
		return fmt.Errorf("quality gate run PASS has no log evidence")
	}
	if _, err := os.Stat(record.Log); err != nil {
		return fmt.Errorf("quality gate log evidence is unavailable: %w", err)
	}
	return nil
}

func publicationRuntimeInstallGates(cfg config.AppConfig, st *state.StateStore, candidate state.PublicationCandidate) ([]publicationGateProjection, *finalizationFailure) {
	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		gate := publicationGateProjection{Gate: "runtime-install", Required: true, Status: publicationGateMissing, SnapshotID: candidate.SnapshotID, Reason: err.Error()}
		return []publicationGateProjection{gate}, publicationReadinessFailure(publicationFailureGateMissing, err.Error())
	}
	if !requirement.Required {
		return []publicationGateProjection{{Gate: "runtime-install", Required: false, Status: publicationGatePass, SnapshotID: candidate.SnapshotID}}, nil
	}
	if requirement.Head != candidate.BaseHead && requirement.Head != candidate.CommitOID {
		gate := publicationGateProjection{Gate: "runtime-install", Required: true, Status: publicationGateStale, SnapshotID: candidate.SnapshotID, Reason: "runtime classification HEAD does not match candidate base or promoted candidate"}
		return []publicationGateProjection{gate}, publicationReadinessFailure(publicationFailureCandidateStale, gate.Reason)
	}
	requirement.Head = candidate.CommitOID
	evidence, err := st.LoadRuntimeInstallEvidence()
	installGate := publicationGateProjection{Gate: "runtime-install", Required: true, Status: publicationGateMissing, SnapshotID: candidate.SnapshotID}
	smokeGate := publicationGateProjection{Gate: "installed-state-smoke", Required: true, Status: publicationGateMissing, SnapshotID: candidate.SnapshotID}
	if err != nil {
		installGate.Reason = err.Error()
		smokeGate.Reason = "runtime install evidence is missing"
		return []publicationGateProjection{installGate, smokeGate}, publicationReadinessFailure(publicationFailureGateMissing, installGate.Reason)
	}
	if evidence.TaskID != candidate.TaskID || evidence.Head != candidate.CommitOID || evidence.InstalledRevision != candidate.CommitOID || evidence.SourceDigest != requirement.SourceDigest {
		installGate.Status = publicationGateStale
		installGate.Reason = "runtime install evidence does not match publication candidate"
		smokeGate.Status = publicationGateStale
		smokeGate.Reason = installGate.Reason
		return []publicationGateProjection{installGate, smokeGate}, publicationReadinessFailure(publicationFailureCandidateStale, installGate.Reason)
	}
	installGate.Status = publicationGatePass
	if evidence.SmokeResult != state.ValidationResultPass {
		smokeGate.Status = publicationGateFail
		smokeGate.Reason = "installed-state smoke did not pass"
		return []publicationGateProjection{installGate, smokeGate}, publicationReadinessFailure(publicationFailureGateFailed, smokeGate.Reason)
	}
	if _, _, failure := installedRuntimeStatus(cfg, candidate.CommitOID); failure != nil {
		smokeGate.Status = publicationGateStale
		smokeGate.Reason = failure.Detail
		return []publicationGateProjection{installGate, smokeGate}, publicationReadinessFailure(publicationFailureCandidateStale, smokeGate.Reason)
	}
	smokeGate.Status = publicationGatePass
	return []publicationGateProjection{installGate, smokeGate}, nil
}

func validPublicationValidationRunID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func publicationReadinessFailure(reason, detail string) *finalizationFailure {
	return &finalizationFailure{Stage: "publication", Reason: reason, Detail: compactFinalizationDiagnostic(detail)}
}
