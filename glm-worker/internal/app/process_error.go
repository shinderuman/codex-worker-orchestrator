package app

import (
	"errors"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

const (
	errorKindUsage                   = "usage"
	errorKindStdinPayload            = "stdin_payload"
	errorKindNotFound                = "not_found"
	errorKindRepoLockHeld            = "repo_lock_held"
	errorKindWorkerError             = "worker_error"
	errorKindRateLimited             = "rate_limited"
	errorKindProviderUnavailable     = "provider_unavailable"
	errorKindInterrupted             = "interrupted"
	errorKindStopEndpointAbsent      = "stop_endpoint_absent"
	errorKindStopEndpointStale       = "stop_endpoint_stale"
	errorKindVerificationFailed      = "verification_failed"
	errorKindVerificationUnavailable = "verification_unavailable"
	errorKindInternal                = "internal"
)

type processErrorBody struct {
	Kind    string         `json:"kind"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

type processErrorEnvelope struct {
	Error processErrorBody `json:"error"`
}

func WriteProcessError(w io.Writer, err error) error {
	return writeJSON(w, processErrorEnvelope{Error: buildProcessError(err)})
}

func buildProcessError(err error) processErrorBody {
	var usage *UsageError
	var notFound *NotFoundError
	var stdinPayload *StdinPayloadError
	var verification *VerificationError
	var workerErr *workflow.WorkerError
	var rateLimit runner.ZaiRateLimitError
	var providerUnavailable *runner.ProviderUnavailableError
	var interrupted *runner.InterruptedCallError
	var stopEndpoint *StopEndpointError

	switch {
	case errors.As(err, &usage):
		return processErrorBody{Kind: errorKindUsage, Message: usage.Message}
	case errors.As(err, &stdinPayload):
		return processErrorBody{Kind: errorKindStdinPayload, Message: stdinPayload.Message}
	case errors.As(err, &notFound):
		return processErrorBody{Kind: errorKindNotFound, Message: notFound.Message}
	case errors.Is(err, ErrRepoLockHeld):
		return processErrorBody{Kind: errorKindRepoLockHeld, Message: ErrRepoLockHeld.Error()}
	case errors.Is(err, state.ErrNoResumeCheckpoint):
		return processErrorBody{Kind: errorKindNotFound, Message: state.ErrNoResumeCheckpoint.Error()}
	case errors.As(err, &workerErr):
		return processErrorBody{
			Kind:    errorKindWorkerError,
			Message: workerErr.Message,
			Detail:  workerErrorDetail(workerErr),
		}
	case errors.As(err, &rateLimit):
		return processErrorBody{
			Kind:    errorKindRateLimited,
			Message: "Z.ai Coding Plan 5h limit reached; task is stopped and resumable",
			Detail:  rateLimitDetail(rateLimit),
		}
	case errors.As(err, &providerUnavailable):
		return processErrorBody{
			Kind:    errorKindProviderUnavailable,
			Message: "provider stayed unavailable after probe budget; task is stopped and resumable",
			Detail:  providerUnavailableDetail(providerUnavailable),
		}
	case errors.As(err, &interrupted):
		return processErrorBody{
			Kind:    errorKindInterrupted,
			Message: "task interrupted by glm-worker --stop; task is stopped and resumable",
			Detail:  interruptedDetail(interrupted),
		}
	case errors.As(err, &stopEndpoint):
		if stopEndpoint.Absent {
			return processErrorBody{
				Kind:    errorKindStopEndpointAbsent,
				Message: stopEndpoint.Error(),
			}
		}
		return processErrorBody{
			Kind:    errorKindStopEndpointStale,
			Message: stopEndpoint.Error(),
		}
	case errors.As(err, &verification):
		return processErrorBody{
			Kind:    verificationKind(verification.Outcome),
			Message: verification.Reason,
		}
	default:
		return processErrorBody{Kind: errorKindInternal, Message: err.Error()}
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
