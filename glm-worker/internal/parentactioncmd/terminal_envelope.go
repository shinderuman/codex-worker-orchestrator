package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type parentActionTerminalEnvelopePayload struct {
	Status          string                               `json:"status"`
	Terminal        json.RawMessage                      `json:"terminal"`
	Handoff         json.RawMessage                      `json:"handoff,omitempty"`
	HandoffError    string                               `json:"handoff_error,omitempty"`
	ProjectionError string                               `json:"projection_error,omitempty"`
	Projection      *parentActionTerminalProjectionStats `json:"projection,omitempty"`
}

type terminalHandoffLoader func() (json.RawMessage, error)

const (
	actionAccept = "accept"
	actionPark   = "park"
	actionReopen = "reopen"
	actionResume = "resume"
	actionUnpark = "unpark"
)

func executeWithTerminalEnvelope(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return execute(cfg, args, stdout, stderr)
	}
	if err := requireImprovementSignalDisposition(cfg, args[0]); err != nil {
		return err
	}
	if !terminalEnvelopeAction(args[0]) {
		return execute(cfg, args, stdout, stderr)
	}

	var terminal bytes.Buffer
	terminalErr := executeTerminalAction(cfg, args, &terminal, stderr)
	if terminalErr != nil {
		return writeFailedTerminalAction(stdout, terminal.Bytes(), terminalErr, func() (json.RawMessage, error) {
			return readTerminalHandoff(cfg, args[0], stderr, true)
		})
	}
	terminalJSON, err := decodeSingleMachineJSON(terminal.Bytes(), "parent action terminal")
	if err != nil {
		return err
	}
	handoffJSON, err := readTerminalHandoff(cfg, args[0], stderr, false)
	if err != nil {
		return writeTerminalHandoffFailure(stdout, terminalJSON, fmt.Errorf("canonical handoff failed after parent action: %w", err))
	}
	return writeProjectedTerminalEnvelope(stdout, terminalJSON, handoffJSON)
}

func writeFailedTerminalAction(stdout io.Writer, terminalBytes []byte, terminalErr error, loadRecoveryHandoff terminalHandoffLoader) error {
	if len(terminalBytes) == 0 {
		return terminalErr
	}
	terminalJSON, err := decodeSingleMachineJSON(terminalBytes, "failed parent action terminal")
	if err != nil {
		var buildErr error
		terminalJSON, buildErr = malformedFailedTerminal(terminalBytes, err)
		if buildErr != nil {
			return errors.Join(terminalErr, err, buildErr)
		}
	}
	handoffJSON, handoffErr := loadRecoveryHandoff()
	if handoffErr != nil {
		recoveryErr := fmt.Errorf("canonical recovery handoff failed after parent action error: %w", handoffErr)
		return errors.Join(terminalErr, err, writeTerminalHandoffFailure(stdout, terminalJSON, recoveryErr))
	}
	projectionErr := writeProjectedRecoveryTerminalEnvelope(stdout, terminalJSON, handoffJSON)
	return errors.Join(terminalErr, err, projectionErr)
}

func malformedFailedTerminal(raw []byte, decodeErr error) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"error": map[string]any{
			"kind":    "machine_output_violation",
			"message": boundTerminalDiagnostic("failed parent action terminal could not be decoded: "+decodeErr.Error(), 160),
			"detail":  map[string]any{"raw_bytes": len(raw)},
		},
	})
}

func readTerminalHandoff(cfg config.AppConfig, action string, stderr io.Writer, recovery bool) (json.RawMessage, error) {
	var handoff bytes.Buffer
	var err error
	switch {
	case recovery:
		err = runWorker(cfg.RepoRoot, []string{"--handoff", "recovery"}, nil, &handoff, stderr, nil)
	case action == actionReviewEvidence:
		err = app.Execute(app.Command{Mode: app.ModeHandoff}, cfg, nil, &handoff, stderr)
	default:
		err = runWorker(cfg.RepoRoot, []string{"--handoff"}, nil, &handoff, stderr, nil)
	}
	if err != nil {
		return nil, err
	}
	return decodeSingleMachineJSON(handoff.Bytes(), "canonical handoff")
}

func writeProjectedTerminalEnvelope(stdout io.Writer, terminalJSON, handoffJSON json.RawMessage) error {
	return writeProjectedTerminalEnvelopeMode(stdout, terminalJSON, handoffJSON, false)
}

func writeProjectedRecoveryTerminalEnvelope(stdout io.Writer, terminalJSON, handoffJSON json.RawMessage) error {
	return writeProjectedTerminalEnvelopeMode(stdout, terminalJSON, handoffJSON, true)
}

func writeProjectedTerminalEnvelopeMode(stdout io.Writer, terminalJSON, handoffJSON json.RawMessage, recovery bool) error {
	envelope, err := projectParentActionTerminalEnvelopeMode(terminalJSON, handoffJSON, recovery)
	if err == nil {
		return json.NewEncoder(stdout).Encode(envelope)
	}
	var projectionErr *parentActionTerminalProjectionError
	if !errors.As(err, &projectionErr) {
		return err
	}
	failure, failureErr := writeTerminalProjectionFailurePayload(terminalJSON, handoffJSON, projectionErr)
	if failureErr != nil {
		return fmt.Errorf("%w; encode projection overflow payload: %w", err, failureErr)
	}
	if encodeErr := json.NewEncoder(stdout).Encode(failure); encodeErr != nil {
		return fmt.Errorf("%w; encode projection overflow envelope: %w", err, encodeErr)
	}
	return err
}

func executeTerminalAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	switch args[0] {
	case actionReviewEvidence:
		return executeParentReviewEvidence(cfg, args, stdout)
	case string(parentaction.ActionDecision):
		return executePreflightedDecision(cfg, args, stdout, stderr)
	case actionRecordDefectFinding, actionBindDefectTask:
		return executeDefectRegistrationAction(cfg, args, stdout)
	case actionImprovementDisposition:
		return executeImprovementDisposition(cfg, args, stdout)
	default:
		return execute(cfg, args, stdout, stderr)
	}
}

func parentActionTerminalEnvelope(terminalJSON, handoffJSON json.RawMessage) parentActionTerminalEnvelopePayload {
	return parentActionTerminalEnvelopePayload{
		Status:   "parent_action_terminal",
		Terminal: terminalJSON,
		Handoff:  handoffJSON,
	}
}

func writeTerminalHandoffFailure(stdout io.Writer, terminalJSON json.RawMessage, handoffErr error) error {
	envelope := parentActionTerminalEnvelopePayload{
		Status:       "parent_action_terminal_handoff_failed",
		Terminal:     terminalJSON,
		HandoffError: handoffErr.Error(),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		return errors.Join(handoffErr, err)
	}
	if encodedJSONLineBytes(raw) > parentActionTerminalBudgetBytes {
		identity, identityErr := projectTerminalIdentity(terminalJSON)
		if identityErr != nil {
			return errors.Join(handoffErr, identityErr)
		}
		stats := parentActionTerminalProjectionStats{
			BudgetBytes:     parentActionTerminalBudgetBytes,
			RawBytes:        encodedJSONLineBytes(raw),
			TerminalMode:    "identity-only",
			HandoffMode:     "unavailable",
			Overflow:        true,
			ParentToolCalls: 1,
			RecoveryCalls:   0,
		}
		envelope.Terminal = identity
		envelope.HandoffError = boundTerminalDiagnostic(handoffErr.Error(), 160)
		envelope.Projection = &stats
		if err := finalizeProjectionStats(&envelope, &stats); err != nil {
			return errors.Join(handoffErr, err)
		}
		if stats.ProjectedBytes > stats.BudgetBytes {
			return fmt.Errorf("%w; bounded terminal handoff failure exceeds budget: projected=%d budget=%d", handoffErr, stats.ProjectedBytes, stats.BudgetBytes)
		}
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		return fmt.Errorf("%w; encode terminal handoff failure envelope: %w", handoffErr, err)
	}
	return handoffErr
}

func boundTerminalDiagnostic(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
}

func terminalEnvelopeAction(action string) bool {
	if descriptor, ok := parentaction.LookupPayloadAction(action); ok {
		return descriptor.Action != parentaction.ActionReviseMilestones
	}
	switch action {
	case actionStart, actionApprove, actionAccept, actionResume, "no-go", actionRecordPublicationFinding, actionRecordDefectFinding, actionBindDefectTask, actionImprovementDisposition, actionReopen, actionPark, actionUnpark, actionReviewEvidence:
		return true
	default:
		return false
	}
}

func decodeSingleMachineJSON(raw []byte, label string) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s is not a single JSON value: %w", label, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%s contains multiple JSON values", label)
		}
		return nil, fmt.Errorf("%s has trailing non-JSON data: %w", label, err)
	}
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return nil, fmt.Errorf("%s is empty", label)
	}
	return value, nil
}
