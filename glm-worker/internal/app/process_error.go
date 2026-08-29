package app

import (
	"errors"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type processErrorBody struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type processErrorEnvelope struct {
	Error processErrorBody `json:"error"`
}

const (
	errorKindUsage                   = "usage"
	errorKindStdinPayload            = "stdin_payload"
	errorKindNotFound                = "not_found"
	errorKindRepoLockHeld            = "repo_lock_held"
	errorKindWorkerError             = "worker_error"
	errorKindRateLimited             = "rate_limited"
	errorKindProviderUnavailable     = "provider_unavailable"
	errorKindGuardRecoverable        = "guard_recoverable"
	errorKindInterrupted             = "interrupted"
	errorKindStopEndpointAbsent      = "stop_endpoint_absent"
	errorKindStopEndpointStale       = "stop_endpoint_stale"
	errorKindVerificationFailed      = "verification_failed"
	errorKindVerificationUnavailable = "verification_unavailable"
	errorKindCodexLimitUnavailable   = "codex_limit_unavailable"
	errorKindInstallSmokeFailed      = "install_smoke_failed"
	errorKindQualityGateFailed       = "quality_gate_failed"
	errorKindMachineOutputViolation  = "machine_output_violation"
	errorKindInternal                = "internal"
)

func WriteProcessError(w io.Writer, err error) error {
	return writeJSON(w, processErrorEnvelope{Error: buildProcessError(err)})
}

func buildProcessError(err error) processErrorBody {
	if body, ok := buildInputProcessError(err); ok {
		return body
	}
	if body, ok := buildRuntimeProcessError(err); ok {
		return body
	}
	if body, ok := buildPostRunProcessError(err); ok {
		return body
	}
	return processErrorBody{Kind: errorKindInternal, Message: err.Error()}
}

func buildInputProcessError(err error) (processErrorBody, bool) {
	var usage *UsageError
	var notFound *NotFoundError
	var stdinPayload *StdinPayloadError
	var outputViolation *MachineOutputViolationError

	switch {
	case errors.As(err, &usage):
		return processErrorBody{Kind: errorKindUsage, Message: usage.Message}, true
	case errors.As(err, &stdinPayload):
		return processErrorBody{Kind: errorKindStdinPayload, Message: stdinPayload.Message}, true
	case errors.As(err, &notFound):
		return processErrorBody{Kind: errorKindNotFound, Message: notFound.Message}, true
	case errors.Is(err, ErrRepoLockHeld):
		return processErrorBody{Kind: errorKindRepoLockHeld, Message: ErrRepoLockHeld.Error()}, true
	case errors.Is(err, state.ErrNoResumeCheckpoint):
		return processErrorBody{Kind: errorKindNotFound, Message: state.ErrNoResumeCheckpoint.Error()}, true
	case errors.As(err, &outputViolation):
		return processErrorBody{
			Kind:    errorKindMachineOutputViolation,
			Message: outputViolation.Error(),
			Detail:  map[string]any{"held_bytes": outputViolation.HeldBytes},
		}, true
	default:
		return processErrorBody{}, false
	}
}

func buildRuntimeProcessError(err error) (processErrorBody, bool) {
	var workerErr *workflow.WorkerError
	var rateLimit runner.ZaiRateLimitError
	var providerUnavailable *runner.ProviderUnavailableError
	var guardRecoverable *workflow.GuardRecoverableError
	var interrupted *runner.InterruptedCallError
	var stopEndpoint *StopEndpointError

	switch {
	case errors.As(err, &workerErr):
		return processErrorBody{
			Kind:    errorKindWorkerError,
			Message: workerErr.Message,
			Detail:  workerErrorDetail(workerErr),
		}, true
	case errors.As(err, &rateLimit):
		return processErrorBody{
			Kind:    errorKindRateLimited,
			Message: "Z.ai Coding Plan 5h limit reached; task is stopped and resumable",
			Detail:  rateLimitDetail(rateLimit),
		}, true
	case errors.As(err, &providerUnavailable):
		return processErrorBody{
			Kind:    errorKindProviderUnavailable,
			Message: "provider stayed unavailable after probe budget; task is stopped and resumable",
			Detail:  providerUnavailableDetail(providerUnavailable),
		}, true
	case errors.As(err, &guardRecoverable):
		return processErrorBody{
			Kind:    errorKindGuardRecoverable,
			Message: "guard rejected the model call; inspect or repair the guard condition before resuming the same task",
			Detail: map[string]any{
				"phase":                  stringPtr(guardRecoverable.Phase),
				"task_id":                stringPtr(guardRecoverable.TaskID),
				"repo_root":              stringPtr(guardRecoverable.RepoRoot),
				"resume_available":       true,
				"completed_result_saved": guardRecoverable.ResultSaved,
			},
		}, true
	case errors.As(err, &interrupted):
		return processErrorBody{
			Kind:    errorKindInterrupted,
			Message: "task interrupted by glm-worker --stop; task is stopped and resumable",
			Detail:  interruptedDetail(interrupted),
		}, true
	case errors.As(err, &stopEndpoint):
		kind := errorKindStopEndpointStale
		if stopEndpoint.Absent {
			kind = errorKindStopEndpointAbsent
		}
		return processErrorBody{Kind: kind, Message: stopEndpoint.Error()}, true
	default:
		return processErrorBody{}, false
	}
}

func buildPostRunProcessError(err error) (processErrorBody, bool) {
	var verification *VerificationError
	var codexLimit *CodexLimitError
	var smokeFail *InstallSmokeError
	var qualityGateFail *QualityGateError

	switch {
	case errors.As(err, &verification):
		return processErrorBody{Kind: verificationKind(verification.Outcome), Message: verification.Reason}, true
	case errors.As(err, &codexLimit):
		return processErrorBody{
			Kind:    errorKindCodexLimitUnavailable,
			Message: "codex rate limits unreadable; use the manual recovery path",
			Detail:  map[string]any{"phase": codexLimit.Phase},
		}, true
	case errors.As(err, &smokeFail):
		return processErrorBody{
			Kind:    errorKindInstallSmokeFailed,
			Message: smokeFail.Error(),
			Detail:  installSmokeFailDetail(smokeFail),
		}, true
	case errors.As(err, &qualityGateFail):
		return processErrorBody{
			Kind:    errorKindQualityGateFailed,
			Message: qualityGateFail.Error(),
			Detail:  qualityGateFailDetail(qualityGateFail),
		}, true
	default:
		return processErrorBody{}, false
	}
}

func installSmokeFailDetail(err *InstallSmokeError) map[string]any {
	return map[string]any{
		"exit_code":   err.ExitCode,
		"result":      "fail",
		"role":        stringPtr(err.Role),
		"duration_ms": err.DurationMS,
	}
}

func qualityGateFailDetail(err *QualityGateError) map[string]any {
	return map[string]any{
		"validation_run_id": err.ValidationRunID,
		"exit_code":         err.ExitCode,
		"form":              err.Form,
		"command":           err.Command,
		"working_dir":       err.WorkingDir,
		"duration_ms":       err.DurationMS,
		"log":               stringPtr(err.LogPath),
	}
}

func workerErrorDetail(err *workflow.WorkerError) map[string]any {
	detail := map[string]any{}
	if err.Phase != "" {
		detail["phase"] = err.Phase
	}
	if err.ExitCode != 0 {
		detail["exit_code"] = err.ExitCode
	}
	if err.Tail != "" {
		detail["output_tail"] = err.Tail
	}
	return detail
}

func rateLimitDetail(err runner.ZaiRateLimitError) map[string]any {
	detail := map[string]any{
		"limit":            "ZAI_GLM_CODING_PLAN_5H",
		"phase":            err.Phase,
		"task_id":          stringPtr(err.TaskID),
		"repo_root":        stringPtr(err.RepoRoot),
		"reset_at_cst":     stringPtr(err.Limit.ResetAtCST),
		"reset_at_rfc3339": stringPtr(err.Limit.ResetAtRFC3339),
		"resume_available": true,
	}
	if available, at := err.AutoResumeSchedule(); available {
		detail["auto_resume_available"] = true
		detail["auto_resume_at_rfc3339"] = at
		detail["auto_resume_key"] = err.AutoResumeKey()
	} else {
		detail["auto_resume_available"] = false
	}
	if err.ArtifactWarning != "" {
		detail["artifact_warning"] = err.ArtifactWarning
	}
	return detail
}

func providerUnavailableDetail(err *runner.ProviderUnavailableError) map[string]any {
	return map[string]any{
		"phase":            stringPtr(err.Phase),
		"classification":   stringPtr(err.Classification),
		"probes":           err.Probes,
		"elapsed_ms":       err.Elapsed.Milliseconds(),
		"task_id":          stringPtr(err.TaskID),
		"repo_root":        stringPtr(err.RepoRoot),
		"resume_available": true,
	}
}

func interruptedDetail(err *runner.InterruptedCallError) map[string]any {
	detail := map[string]any{
		"phase":            stringPtr(err.Phase),
		"task_id":          stringPtr(err.TaskID),
		"repo_root":        stringPtr(err.RepoRoot),
		"resume_available": true,
	}
	if err.CleanupWarning != "" {
		detail["cleanup_warning"] = err.CleanupWarning
	}
	return detail
}

func verificationKind(outcome autoresume.Outcome) string {
	if outcome == autoresume.Unavailable {
		return errorKindVerificationUnavailable
	}
	return errorKindVerificationFailed
}
