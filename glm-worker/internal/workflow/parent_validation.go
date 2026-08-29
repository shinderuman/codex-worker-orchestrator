package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

var parentValidationGateRunner = func(w *Workflow, form string) (parentValidationGateRecord, error) {
	return w.runParentValidationGate(form)
}

func (w *Workflow) convergeParentValidation(checkpoint state.ResumeCheckpoint, result packet.Result) (packet.Result, error) {
	if result.Status != packet.StatusImplemented || result.ParentValidation == "" || result.ParentValidationEvidence != "" {
		return result, nil
	}
	form := result.ParentValidation

	qualityReport, err := w.qualityGate(w.config.RepoRoot)
	if err != nil {
		return packet.Result{}, &WorkerError{Phase: "harnesslint", Message: fmt.Sprintf("harnesslint failed before parent validation: %v", err)}
	}
	if harnesslint.IsViolation(qualityReport) {
		return w.fixBeforeParentValidation(checkpoint, qualityGateFixResult(qualityReport), form)
	}

	record, err := parentValidationGateRunner(w, form)
	if err != nil {
		return packet.Result{}, err
	}
	if record.Status != "pass" {
		return w.fixBeforeParentValidation(checkpoint, parentValidationFailureResult(record), form)
	}
	result.ParentValidationEvidence = parentValidationEvidence(record)
	return result, nil
}

func (w *Workflow) fixBeforeParentValidation(
	checkpoint state.ResumeCheckpoint,
	failure packet.Result,
	form string,
) (packet.Result, error) {
	if checkpoint.AutoFixes >= w.config.MaxAutoFixRounds {
		return packet.Result{}, &WorkerError{
			Phase:   "parent-validation",
			Message: fmt.Sprintf("parent validation %s remains failing after the worker fix budget", form),
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
	fixCheckpoint.ParentValidation = form
	w.state.RecordAutoFix()
	fixed, stopped, err := w.runAutoFixCheckpoint(fixCheckpoint)
	if err != nil || stopped {
		return packet.Result{}, err
	}
	return fixed, nil
}

func applyCheckpointParentValidation(checkpoint state.ResumeCheckpoint, result packet.Result) (packet.Result, error) {
	if checkpoint.ParentValidation == "" || result.Status != packet.StatusImplemented {
		return result, nil
	}
	if result.ParentValidation != "" && result.ParentValidation != checkpoint.ParentValidation {
		return packet.Result{}, &WorkerError{
			Phase: "parent-validation",
			Message: fmt.Sprintf(
				"worker changed the parent validation obligation from %s to %s",
				checkpoint.ParentValidation,
				result.ParentValidation,
			),
		}
	}
	result.ParentValidation = checkpoint.ParentValidation
	result.ParentValidationEvidence = ""
	result.Risk = packet.RiskHigh
	return result, nil
}

func (w *Workflow) runParentValidationGate(form string) (parentValidationGateRecord, error) {
	executable, err := os.Executable()
	if err != nil {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation executableを解決できません: %w", err)
	}
	stdout, stderr, runErr := runParentValidationCommand(executable, w.config.RepoRoot, "--quality-gate", form)
	runID, err := parentValidationRunID(stdout, stderr, runErr)
	if err != nil {
		return parentValidationGateRecord{}, err
	}
	recordOut, recordErrOut, recordErr := runParentValidationCommand(
		executable,
		w.config.RepoRoot,
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
	if record.ValidationRunID != runID || record.Form != form {
		return parentValidationGateRecord{}, fmt.Errorf("parent validation run identityが要求と一致しません")
	}
	return record, nil
}

func runParentValidationCommand(executable, repoRoot string, args ...string) (string, string, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = repoRoot
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
		"status=pass;form=%s;validation_run_id=%s;head=%s;index=%s;worktree=%s;log=%s",
		record.Form,
		record.ValidationRunID,
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
			"form=%s;validation_run_id=%s;exit_code=%d;head=%s;index=%s;worktree=%s;log=%s",
			record.Form,
			record.ValidationRunID,
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
