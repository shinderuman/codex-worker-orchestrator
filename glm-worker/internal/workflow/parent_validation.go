package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentValidationGateRecord struct {
	ValidationRunID string `json:"validation_run_id"`
	Form            string `json:"form"`
	Repository      string `json:"repository"`
	WorkingDir      string `json:"working_dir"`
	Head            string `json:"head"`
	IndexDigest     string `json:"index_digest"`
	WorktreeDigest  string `json:"worktree_digest"`
	Status          string `json:"status"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	Log             string `json:"log"`
}

type parentValidationStartOutput struct {
	ValidationRunID string `json:"validation_run_id"`
}

type parentValidationProcessError struct {
	Error struct {
		Kind    string `json:"kind"`
		Message string `json:"message"`
		Detail  struct {
			ValidationRunID string `json:"validation_run_id"`
		} `json:"detail"`
	} `json:"error"`
}

const parentValidationGateFailureKind = "quality_gate_failed"

var parentValidationGateRunner = func(w *Workflow, request packet.ParentValidationRequest) (parentValidationGateRecord, error) {
	return w.runParentValidationGate(request)
}

func (w *Workflow) convergeParentValidation(checkpoint state.ResumeCheckpoint, result packet.Result) (packet.Result, error) {
	request := result.ParentValidationRequest()
	if result.Status != packet.StatusImplemented || request == nil || result.ParentValidationEvidence != "" {
		return result, nil
	}

	qualityReport, err := w.qualityGate(w.config.RepoRoot)
	if err != nil {
		return packet.Result{}, &WorkerError{Phase: "harnesslint", Message: fmt.Sprintf("harnesslint failed before parent validation: %v", err)}
	}
	if harnesslint.IsViolation(qualityReport) {
		return w.fixBeforeParentValidation(checkpoint, qualityGateFixResult(qualityReport), *request)
	}

	record, err := parentValidationGateRunner(w, *request)
	if err != nil {
		return packet.Result{}, err
	}
	if err := w.validateParentValidationRecord(*request, record); err != nil {
		return packet.Result{}, err
	}
	switch record.Status {
	case "pass":
		result.ParentValidationEvidence = parentValidationEvidence(record)
		return result, nil
	case "fail":
		return w.fixBeforeParentValidation(checkpoint, parentValidationFailureResult(record), *request)
	default:
		return packet.Result{}, &WorkerError{
			Phase:   "parent-validation",
			Message: fmt.Sprintf("parent validation run %s ended with non-reviewable status %s", record.ValidationRunID, record.Status),
		}
	}
}

func (w *Workflow) validateParentValidationRecord(request packet.ParentValidationRequest, record parentValidationGateRecord) error {
	resolvedRoot, err := filepath.EvalSymlinks(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("parent validation repository rootを解決できません: %w", err)
	}
	recordRepository, err := filepath.EvalSymlinks(record.Repository)
	if err != nil || filepath.Clean(recordRepository) != filepath.Clean(resolvedRoot) {
		return &WorkerError{Phase: "parent-validation", Message: "parent validation evidenceのrepository identityがactive repositoryと一致しません"}
	}
	expectedWorkingDir, err := resolveParentValidationWorkingDir(w.config.RepoRoot, request.WorkingDir)
	if err != nil {
		return err
	}
	recordWorkingDir, err := filepath.EvalSymlinks(record.WorkingDir)
	if err != nil || filepath.Clean(recordWorkingDir) != filepath.Clean(expectedWorkingDir) {
		return &WorkerError{Phase: "parent-validation", Message: "parent validation evidenceのworking directoryが要求と一致しません"}
	}
	current, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("parent validation後snapshotを取得できません: %w", err)
	}
	if record.Head != current.Head || record.IndexDigest != current.IndexDigest || record.WorktreeDigest != current.WorktreeDigest {
		return &WorkerError{
			Phase:   "parent-validation",
			Message: "parent validation完了後にrepository snapshotが変化したためvalidation evidenceを採用できません",
		}
	}
	return nil
}

func (w *Workflow) fixBeforeParentValidation(
	checkpoint state.ResumeCheckpoint,
	failure packet.Result,
	request packet.ParentValidationRequest,
) (packet.Result, error) {
	if checkpoint.AutoFixes >= w.config.MaxAutoFixRounds {
		return packet.Result{}, &WorkerError{
			Phase:   "parent-validation",
			Message: fmt.Sprintf("parent validation %s remains failing after the worker fix budget", request.Form),
		}
	}
	reviewNumber := checkpoint.ReviewNumber
	if reviewNumber < 1 {
		reviewNumber = 1
	}
	fixCheckpoint, err := w.prepareAutoFixCheckpoint(checkpoint.Request, failure, reviewNumber, checkpoint.AutoFixes+1)
	if err != nil {
		return packet.Result{}, err
	}
	fixCheckpoint.ParentValidation = cloneParentValidationRequest(&request)
	w.state.RecordAutoFix()
	fixed, stopped, err := w.runAutoFixCheckpoint(fixCheckpoint)
	if err != nil || stopped {
		return packet.Result{}, err
	}
	return fixed, nil
}

func applyCheckpointParentValidation(checkpoint state.ResumeCheckpoint, result packet.Result) (packet.Result, error) {
	if checkpoint.ParentValidation == nil || result.Status != packet.StatusImplemented {
		return result, nil
	}
	reported := result.ParentValidationRequest()
	if reported != nil && !sameParentValidationRequest(*reported, *checkpoint.ParentValidation) {
		return packet.Result{}, &WorkerError{
			Phase: "parent-validation",
			Message: fmt.Sprintf(
				"worker changed the parent validation obligation from %s@%s to %s@%s",
				checkpoint.ParentValidation.Form,
				checkpoint.ParentValidation.WorkingDir,
				reported.Form,
				reported.WorkingDir,
			),
		}
	}
	result.SetParentValidationRequest(checkpoint.ParentValidation)
	result.ParentValidationEvidence = ""
	result.Risk = packet.RiskHigh
	return result, nil
}

func cloneParentValidationRequest(request *packet.ParentValidationRequest) *packet.ParentValidationRequest {
	if request == nil {
		return nil
	}
	copy := *request
	return &copy
}

func sameParentValidationRequest(left, right packet.ParentValidationRequest) bool {
	return left.Form == right.Form && left.WorkingDir == right.WorkingDir
}

func (w *Workflow) runParentValidationGate(request packet.ParentValidationRequest) (parentValidationGateRecord, error) {
	workingDir, err := resolveParentValidationWorkingDir(w.config.RepoRoot, request.WorkingDir)
	if err != nil {
		return parentValidationGateRecord{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation executableを解決できません: %w", err)
	}
	stdout, stderr, runErr := runParentValidationCommand(executable, workingDir, "--quality-gate", request.Form)
	runID, err := parentValidationRunID(stdout, stderr, runErr)
	if err != nil {
		return parentValidationGateRecord{}, err
	}
	recordOut, recordErrOut, recordErr := runParentValidationCommand(
		executable,
		workingDir,
		"--quality-gate",
		"result",
		runID,
	)
	if recordErr != nil {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation run %sのexact snapshot evidenceを取得できません: %s: %w", runID, strings.TrimSpace(recordErrOut), recordErr)
	}
	var record parentValidationGateRecord
	if err := json.Unmarshal(bytes.TrimSpace([]byte(recordOut)), &record); err != nil {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation run evidenceを解析できません: %w", err)
	}
	if record.ValidationRunID != runID || record.Form != request.Form || filepath.Clean(record.WorkingDir) != filepath.Clean(workingDir) {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation run identityが要求と一致しません")
	}
	return record, nil
}

func resolveParentValidationWorkingDir(repoRoot, relative string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", fmt.Errorf("parent validation repository rootを解決できません: %w", err)
	}
	candidate := filepath.Join(repoRoot, filepath.FromSlash(relative))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("parent validation working directoryを解決できません: %w", err)
	}
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("parent validation working directoryがrepository外です: %s", relative)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("parent validation working directoryを確認できません: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("parent validation working directoryがdirectoryではありません: %s", relative)
	}
	return resolved, nil
}

func runParentValidationCommand(executable, workingDir string, args ...string) (string, string, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = workingDir
	cmd.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func parentValidationRunID(stdout, stderr string, runErr error) (string, error) {
	if runErr == nil {
		var output parentValidationStartOutput
		if err := json.Unmarshal(bytes.TrimSpace([]byte(stdout)), &output); err != nil {
			return "", fmt.Errorf("parent validation start resultを解析できません: %w", err)
		}
		if output.ValidationRunID == "" {
			return "", fmt.Errorf("parent validation start resultにvalidation_run_idがありません")
		}
		return output.ValidationRunID, nil
	}
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var envelope parentValidationProcessError
		if err := json.Unmarshal([]byte(line), &envelope); err != nil || envelope.Error.Kind == "" {
			continue
		}
		if envelope.Error.Kind != parentValidationGateFailureKind {
			return "", fmt.Errorf("parent validation command failed: %s", envelope.Error.Message)
		}
		if envelope.Error.Detail.ValidationRunID == "" {
			return "", fmt.Errorf("failed parent validation has no validation_run_id")
		}
		return envelope.Error.Detail.ValidationRunID, nil
	}
	return "", fmt.Errorf("parent validation command failed without structured quality-gate evidence: %w", runErr)
}

func parentValidationEvidence(record parentValidationGateRecord) string {
	return fmt.Sprintf(
		"status=pass;form=%s;validation_run_id=%s;working_dir=%s;head=%s;index=%s;worktree=%s;log=%s",
		record.Form,
		record.ValidationRunID,
		record.WorkingDir,
		record.Head,
		record.IndexDigest,
		record.WorktreeDigest,
		record.Log,
	)
}

func parentValidationFailureResult(record parentValidationGateRecord) packet.Result {
	return packet.Result{
		Status:              packet.StatusFixRequired,
		Risk:                packet.RiskLow,
		Summary:             "required parent-capability validation failed before independent review",
		RequirementCoverage: "the implementation snapshot is not reviewable until the required parent gate passes",
		Invariants:          "worker/reviewer capability boundaries remain unchanged; validation is bound to the exact repository snapshot",
		TestEvidence: fmt.Sprintf(
			"form=%s;validation_run_id=%s;working_dir=%s;exit_code=%d;head=%s;index=%s;worktree=%s;log=%s",
			record.Form,
			record.ValidationRunID,
			record.WorkingDir,
			record.ExitCode,
			record.Head,
			record.IndexDigest,
			record.WorktreeDigest,
			record.Log,
		),
		Issues:       "make the required parent-capability validation pass on the current implementation",
		ResidualRisk: "independent review is intentionally deferred while deterministic validation is failing",
		Targets:      []string{"none"},
	}
}
