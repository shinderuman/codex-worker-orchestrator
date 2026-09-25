package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) recordInterruptedEvent(checkpoint state.ResumeCheckpoint) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:      w.state.ReadOr("task.id", "unknown"),
		CallType:    state.CallTypeEvent,
		StartedAt:   now,
		CompletedAt: now,
		Phase:       checkpoint.Phase + "-user-interrupted",
		Role:        checkpoint.Role,
		ModelAlias:  checkpoint.Model,
		Outcome:     "user_interrupted",
	})
}

func (w *Workflow) recordProviderUnavailableEvent(checkpoint state.ResumeCheckpoint, classification string, probes int, elapsed time.Duration) {
	now := w.now().UTC()
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:                 w.state.ReadOr("task.id", "unknown"),
		CallType:               state.CallTypeEvent,
		StartedAt:              now,
		CompletedAt:            now,
		Phase:                  checkpoint.Phase + "-provider-unavailable",
		Role:                   checkpoint.Role,
		ModelAlias:             checkpoint.Model,
		Outcome:                "provider_unavailable",
		ProviderClassification: classification,
		ProbeAttempt:           probes,
		RetryElapsedMS:         elapsed.Milliseconds(),
	})
}

func (w *Workflow) recordProbeCall(
	checkpoint state.ResumeCheckpoint,
	probe runner.ProbeResult,
	attempt int,
	startedAt time.Time,
	completedAt time.Time,
	probeErr error,
) {
	outcome := "probe_success"
	errorText := ""
	if probeErr != nil {
		outcome = "probe_failure"
		errorText = boundedText(probeErr.Error(), packet.MaxDiagnosticBytes)
	}
	w.state.RecordProbeOutcome(outcome)
	promptHash := sha256.Sum256([]byte(runner.ProbePrompt))
	response := probe.Response
	if !w.config.TelemetryContent {
		response = ""
	}
	resolvedUsage := make(map[string]state.ResolvedModelUsage, len(probe.ModelUsage))
	for model, usage := range probe.ModelUsage {
		resolvedUsage[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	w.state.RecordModelCallLog(state.ModelCallLog{
		TaskID:              w.state.ReadOr("task.id", "unknown"),
		CallType:            state.CallTypeProbe,
		SessionID:           "none",
		StartedAt:           startedAt,
		CompletedAt:         completedAt,
		Phase:               fmt.Sprintf("%s-probe-%d", checkpoint.Phase, attempt),
		Role:                checkpoint.Role,
		ModelAlias:          checkpoint.Model,
		ResolvedModelUsage:  resolvedUsage,
		Effort:              "low",
		ReadOnly:            true,
		Outcome:             outcome,
		ProbeAttempt:        attempt,
		PromptBytes:         len(runner.ProbePrompt),
		PromptSHA256:        hex.EncodeToString(promptHash[:]),
		Response:            response,
		ResponseBytes:       len(probe.Response),
		Error:               errorText,
		TopLevelUsage:       state.TokenUsage(probe.Usage),
		WallDurationMS:      completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:    probe.DurationMS,
		ClaudeAPIDurationMS: probe.DurationAPIMS,
		TotalCostUSD:        probe.TotalCostUSD,
	})
}

func (w *Workflow) recordModelCall(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
	diag callDiagnostics,
) {
	entry := w.buildModelCallLog(checkpoint, runResult, startedAt, completedAt, outcome, packetStatus, callErr, outputPath)
	if entry.CallID == "" {
		if callID, err := state.NewUUID(); err == nil {
			entry.CallID = callID
		}
	}
	w.applyCallDiagnostics(&entry, checkpoint, outcome, callErr, diag)
	w.state.RecordModelCallLog(entry)
	w.lastCallID = entry.CallID
}

func (w *Workflow) buildModelCallLog(
	checkpoint state.ResumeCheckpoint,
	runResult runner.RunResult,
	startedAt time.Time,
	completedAt time.Time,
	outcome string,
	packetStatus string,
	callErr error,
	outputPath string,
) state.ModelCallLog {
	response := runResult.Response
	if response == "" {
		response = packet.Tail(outputPath, packet.MaxDiagnosticBytes)
	}
	promptHash := sha256.Sum256([]byte(checkpoint.Prompt))
	responseHash := sha256.Sum256([]byte(response))
	errorText := modelCallErrorText(callErr)
	promptContent, systemPromptContent, responseContent := w.telemetryContents(checkpoint.Prompt, runResult.SystemPrompt, response)
	return state.ModelCallLog{
		CallID:                            runResult.CallID,
		TaskID:                            w.state.ReadOr("task.id", "unknown"),
		CallType:                          state.CallTypeTask,
		SessionID:                         modelSessionID(w.state, checkpoint.Role, runResult.SessionID),
		StartedAt:                         startedAt,
		CompletedAt:                       completedAt,
		Phase:                             checkpoint.Phase,
		Role:                              checkpoint.Role,
		ModelAlias:                        checkpoint.Model,
		ResolvedModelID:                   runResult.ResolvedModelID,
		ConfiguredAutoCompactWindowTokens: runResult.ConfiguredAutoCompactWindowTokens,
		KnownModelContextWindowTokens:     runResult.KnownModelContextWindowTokens,
		DeclaredMaxContextWindowTokens:    runResult.DeclaredMaxContextWindowTokens,
		ContextWindowSource:               runResult.ContextWindowSource,
		ResolvedModelUsage:                resolvedModelUsage(runResult.ModelUsage),
		Effort:                            checkpoint.Effort,
		ReadOnly:                          checkpoint.ReadOnly,
		Resumed:                           runResult.Resumed,
		Outcome:                           outcome,
		PacketStatus:                      packetStatus,
		Prompt:                            promptContent,
		PromptBytes:                       len([]byte(checkpoint.Prompt)),
		PromptSHA256:                      hex.EncodeToString(promptHash[:]),
		SystemPromptBytes:                 runResult.SystemPromptBytes,
		SystemPromptSHA256:                runResult.SystemPromptSHA256,
		SystemPrompt:                      systemPromptContent,
		Response:                          responseContent,
		ResponseBytes:                     len([]byte(response)),
		ResponseSHA256:                    hex.EncodeToString(responseHash[:]),
		Error:                             errorText,
		TopLevelUsage:                     topLevelUsage(runResult.TopLevelUsage),
		Runtime:                           runResult.Runtime,
		WallDurationMS:                    completedAt.Sub(startedAt).Milliseconds(),
		ClaudeDurationMS:                  runResult.DurationMS,
		ClaudeAPIDurationMS:               runResult.DurationAPIMS,
		TopLevelTurns:                     runResult.TopLevelTurns,
		TotalCostUSD:                      runResult.TotalCostUSD,
	}
}

func topLevelUsage(usage runner.TokenUsage) state.TokenUsage {
	return state.TokenUsage{
		InputTokens:              usage.InputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		OutputTokens:             usage.OutputTokens,
	}
}

func modelCallErrorText(callErr error) string {
	if callErr == nil {
		return ""
	}
	return boundedText(callErr.Error(), packet.MaxDiagnosticBytes)
}

func resolvedModelUsage(usageByModel map[string]runner.ModelUsage) map[string]state.ResolvedModelUsage {
	resolved := make(map[string]state.ResolvedModelUsage, len(usageByModel))
	for model, usage := range usageByModel {
		resolved[model] = state.ResolvedModelUsage{
			InputTokens:              usage.InputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
			OutputTokens:             usage.OutputTokens,
			CostUSD:                  usage.CostUSD,
		}
	}
	return resolved
}

func (w *Workflow) telemetryContents(prompt string, systemPrompt string, response string) (string, string, string) {
	if !w.config.TelemetryContent {
		return "", "", ""
	}
	return prompt, systemPrompt, response
}

func (w *Workflow) applyCallDiagnostics(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint, outcome string, callErr error, diag callDiagnostics) {
	if diag.reportedRisk != "" {
		if checkpoint.Role == state.ReviewerRole {
			entry.ReviewerReportedRisk = diag.reportedRisk
		} else {
			entry.WorkerReportedRisk = diag.reportedRisk
		}
	}
	w.applyEffectiveRiskDiagnostic(entry, checkpoint)
	if diag.providerClassification != "" {
		entry.ProviderClassification = diag.providerClassification
	}
	if w.currentResumeSource != "" {
		entry.ResumeSource = w.currentResumeSource
		w.currentResumeSource = ""
	}
	if w.pendingRetry != nil {
		entry.RetryOf = w.pendingRetry.callID
		entry.RetryReason = w.pendingRetry.reason
		w.pendingRetry = nil
	}
	if outcome == "invalid_packet" && callErr != nil {
		category := packet.RejectCategory(callErr)
		if runner.IsStructuredOutputError(callErr) {
			category = "structured-output"
		}
		entry.PacketRejectReason = category
		w.state.RecordPacketReject(category)
	}
	if checkpoint.Role == state.ReviewerRole && outcome == "success" && w.pendingSnapshot != nil {
		entry.Snapshot = w.pendingSnapshot
		w.pendingSnapshot = nil
	}
}

func (w *Workflow) applyEffectiveRiskDiagnostic(entry *state.ModelCallLog, checkpoint state.ResumeCheckpoint) {
	if checkpoint.EffectiveRisk == "" {
		return
	}
	entry.EffectiveRisk = checkpoint.EffectiveRisk
	entry.RiskFloorSource = checkpoint.EffectiveRiskSource
	if checkpoint.Role != state.ReviewerRole || checkpoint.EffectiveRisk != highRiskValue {
		return
	}
	category := riskFloorCategory(checkpoint.EffectiveRiskSource)
	entry.RiskFloorCategory = category
	w.state.RecordRiskFloor(category)
}

func riskFloorCategory(source string) string {
	if source == "" {
		return ""
	}
	var categories []string
	for _, raw := range strings.Split(source, ";") {
		name := strings.SplitN(raw, ":", 2)[0]
		if name != "" {
			categories = append(categories, name)
		}
	}
	return strings.Join(categories, ",")
}

func modelSessionID(st *state.StateStore, role state.SessionRole, fromRunner string) string {
	if fromRunner != "" {
		return fromRunner
	}
	return st.ReadOr(string(role)+".id", "unknown")
}
