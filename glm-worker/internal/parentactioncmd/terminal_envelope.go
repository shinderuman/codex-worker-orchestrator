package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

type parentActionTerminalEnvelopePayload struct {
	Status          string                               `json:"status"`
	Terminal        json.RawMessage                      `json:"terminal"`
	Handoff         json.RawMessage                      `json:"handoff,omitempty"`
	HandoffError    string                               `json:"handoff_error,omitempty"`
	ProjectionError string                               `json:"projection_error,omitempty"`
	Projection      *parentActionTerminalProjectionStats `json:"projection,omitempty"`
	Continuation    *parentActionTerminalContinuation    `json:"continuation,omitempty"`
}

type terminalHandoffLoader func() (json.RawMessage, error)

const (
	actionAccept             = "accept"
	actionResume             = "resume"
	actionResubmitReport     = "resubmit-report"
	actionObservationExecute = "observation-execute"
)

func executeWithTerminalEnvelope(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return execute(cfg, args, stdout, stderr)
	}
	descriptor, ok := lookupParentActionCommand(args[0])
	if !ok || !descriptor.TerminalEnvelope {
		return execute(cfg, args, stdout, stderr)
	}

	var terminal bytes.Buffer
	terminalErr := executeParentActionCommand(cfg, descriptor, args, &terminal, stderr, true)
	if terminalErr != nil {
		return writeFailedTerminalAction(cfg, stdout, terminal.Bytes(), terminalErr, func() (json.RawMessage, error) {
			return readTerminalHandoff(cfg, stderr, true)
		})
	}
	terminalJSON, err := decodeSingleMachineJSON(terminal.Bytes(), "parent action terminal")
	if err != nil {
		return err
	}
	handoffJSON, err := readTerminalHandoff(cfg, stderr, false)
	if err != nil {
		return writeTerminalHandoffFailure(cfg, stdout, terminalJSON, fmt.Errorf("canonical handoff failed after parent action: %w", err))
	}
	return writeProjectedTerminalEnvelopeMode(cfg, stdout, terminalJSON, handoffJSON, false)
}

func writeFailedTerminalAction(cfg config.AppConfig, stdout io.Writer, terminalBytes []byte, terminalErr error, loadRecoveryHandoff terminalHandoffLoader) error {
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
		return errors.Join(terminalErr, err, writeTerminalHandoffFailure(cfg, stdout, terminalJSON, recoveryErr))
	}
	projectionErr := writeProjectedTerminalEnvelopeMode(cfg, stdout, terminalJSON, handoffJSON, true)
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

func readTerminalHandoff(cfg config.AppConfig, stderr io.Writer, recovery bool) (json.RawMessage, error) {
	var handoff bytes.Buffer
	var err error
	if recovery {
		err = runWorker(cfg.RepoRoot, []string{"--handoff", "recovery"}, nil, &handoff, stderr, nil)
	} else {
		err = runWorker(cfg.RepoRoot, []string{"--handoff"}, nil, &handoff, stderr, nil)
	}
	if err != nil {
		return nil, err
	}
	return decodeSingleMachineJSON(handoff.Bytes(), "canonical handoff")
}

func writeProjectedTerminalEnvelopeMode(cfg config.AppConfig, stdout io.Writer, terminalJSON, handoffJSON json.RawMessage, recovery bool) error {
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
	continuation, continuationErr := persistParentActionTerminalContinuation(cfg, terminalJSON)
	if continuationErr != nil {
		if encodeErr := json.NewEncoder(stdout).Encode(failure); encodeErr != nil {
			return errors.Join(err, fmt.Errorf("persist terminal continuation: %w", continuationErr), fmt.Errorf("encode projection overflow envelope: %w", encodeErr))
		}
		return errors.Join(err, fmt.Errorf("persist terminal continuation: %w", continuationErr))
	}
	return writeTerminalProjectionContinuation(stdout, failure, continuation, err)
}

func parentActionTerminalEnvelope(terminalJSON, handoffJSON json.RawMessage) parentActionTerminalEnvelopePayload {
	return parentActionTerminalEnvelopePayload{
		Status:   "parent_action_terminal",
		Terminal: terminalJSON,
		Handoff:  handoffJSON,
	}
}

func writeTerminalHandoffFailure(cfg config.AppConfig, stdout io.Writer, terminalJSON json.RawMessage, handoffErr error) error {
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
		identity, identityOmitted, identityErr := projectTerminalIdentity(terminalJSON)
		if identityErr != nil {
			return errors.Join(handoffErr, identityErr)
		}
		stats := parentActionTerminalProjectionStats{
			BudgetBytes:     parentActionTerminalBudgetBytes,
			RawBytes:        encodedJSONLineBytes(raw),
			TerminalMode:    "identity-only",
			HandoffMode:     "unavailable",
			Overflow:        true,
			OmittedFields:   sortedUniqueStrings(identityOmitted),
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
		if err := attachTerminalContinuation(cfg, stdout, &envelope, terminalJSON, handoffErr); err != nil {
			return err
		}
		return handoffErr
	}
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		return fmt.Errorf("%w; encode terminal handoff failure envelope: %w", handoffErr, err)
	}
	return handoffErr
}

func attachTerminalContinuation(cfg config.AppConfig, stdout io.Writer, envelope *parentActionTerminalEnvelopePayload, terminalJSON json.RawMessage, cause error) error {
	continuation, continuationErr := persistParentActionTerminalContinuation(cfg, terminalJSON)
	if continuationErr != nil {
		continuationErr = fmt.Errorf("persist terminal continuation: %w", continuationErr)
		if encodeErr := json.NewEncoder(stdout).Encode(envelope); encodeErr != nil {
			return errors.Join(cause, continuationErr, fmt.Errorf("encode terminal handoff failure envelope: %w", encodeErr))
		}
		return errors.Join(cause, continuationErr)
	}
	return writeTerminalProjectionContinuation(stdout, *envelope, continuation, cause)
}

func boundTerminalDiagnostic(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "…"
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
