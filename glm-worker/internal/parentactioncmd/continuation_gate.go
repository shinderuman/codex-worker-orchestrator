package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

type continuationGateHandoff struct {
	Consistent    bool                                   `json:"consistent"`
	Inconsistency *string                                `json:"inconsistency"`
	ParentRequest *app.ParentRequestCompletionProjection `json:"parent_request"`
}

type continuationStopHookOutput struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

const (
	actionContinuationStopHook      = "continuation-stop-hook"
	actionContinuationMetadataGuard = "continuation-metadata-guard"
)

func executeContinuationGate(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action <%s|%s>", actionContinuationStopHook, actionContinuationMetadataGuard)
	}
	handoff, err := loadContinuationGateHandoff(cfg)
	switch args[0] {
	case actionContinuationStopHook:
		reason := continuationStopBlockReason(handoff, err)
		if reason == "" {
			return nil
		}
		return json.NewEncoder(stdout).Encode(continuationStopHookOutput{Decision: "block", Reason: reason})
	case actionContinuationMetadataGuard:
		return continuationMetadataGuardFailure(handoff, err)
	default:
		return fmt.Errorf("usage: glm-parent-action <%s|%s>", actionContinuationStopHook, actionContinuationMetadataGuard)
	}
}

func loadContinuationGateHandoff(cfg config.AppConfig) (continuationGateHandoff, error) {
	var raw bytes.Buffer
	if err := app.Execute(app.Command{Mode: app.ModeHandoff}, cfg, nil, &raw, io.Discard); err != nil {
		return continuationGateHandoff{}, fmt.Errorf("canonical handoff unavailable: %w", err)
	}
	var handoff continuationGateHandoff
	if err := json.Unmarshal(raw.Bytes(), &handoff); err != nil {
		return continuationGateHandoff{}, fmt.Errorf("canonical handoff is not valid JSON: %w", err)
	}
	return handoff, nil
}

func continuationStopBlockReason(handoff continuationGateHandoff, loadErr error) string {
	if loadErr != nil {
		return "canonical parent continuation gate unavailable; do not end USER_REQUEST: " + loadErr.Error()
	}
	if !handoff.Consistent {
		reason := "canonical handoff is inconsistent; do not end USER_REQUEST"
		if handoff.Inconsistency != nil && strings.TrimSpace(*handoff.Inconsistency) != "" {
			reason += ": " + strings.TrimSpace(*handoff.Inconsistency)
		}
		return reason
	}
	if handoff.ParentRequest == nil || handoff.ParentRequest.StopAdmitted {
		return ""
	}
	continuation := handoff.ParentRequest.Continuation
	parts := []string{
		"canonical parent continuation does not admit USER_REQUEST stop",
		"state=" + continuation.State,
		"reason=" + continuation.Reason,
	}
	if continuation.Task != "" {
		parts = append(parts, "task="+continuation.Task)
	}
	if continuation.RequiredAction != "" {
		parts = append(parts, "required_action="+continuation.RequiredAction)
	}
	return strings.Join(parts, "; ")
}

func continuationMetadataGuardFailure(handoff continuationGateHandoff, loadErr error) error {
	if loadErr != nil {
		return fmt.Errorf("parent continuation metadata guard unavailable: %w", loadErr)
	}
	if !handoff.Consistent {
		if handoff.Inconsistency != nil && strings.TrimSpace(*handoff.Inconsistency) != "" {
			return fmt.Errorf("parent continuation metadata guard rejected inconsistent handoff: %s", strings.TrimSpace(*handoff.Inconsistency))
		}
		return fmt.Errorf("parent continuation metadata guard rejected inconsistent handoff")
	}
	if handoff.ParentRequest == nil {
		return nil
	}
	projection := handoff.ParentRequest
	switch projection.Continuation.State {
	case repositoryproject.ContinuationContinueNow:
		return nil
	case repositoryproject.ContinuationBlocked,
		repositoryproject.ContinuationExplicitStop,
		repositoryproject.ContinuationDeferredByVerifiedAutomation:
		if projection.StopAdmitted {
			return nil
		}
	case repositoryproject.ContinuationTerminal:
		if projection.CompletionAdmitted && projection.StopAdmitted {
			return nil
		}
	}
	continuation := projection.Continuation
	return fmt.Errorf(
		"parent continuation metadata guard rejected transition: state=%s reason=%s task=%s required_action=%s completion_admitted=%t stop_admitted=%t",
		continuation.State,
		continuation.Reason,
		continuation.Task,
		continuation.RequiredAction,
		projection.CompletionAdmitted,
		projection.StopAdmitted,
	)
}
