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
	if err := executeTerminalAction(cfg, args, &terminal, stderr); err != nil {
		if terminal.Len() != 0 {
			_, _ = stdout.Write(terminal.Bytes())
		}
		return err
	}
	terminalJSON, err := decodeSingleMachineJSON(terminal.Bytes(), "parent action terminal")
	if err != nil {
		return err
	}

	var handoff bytes.Buffer
	if args[0] == actionReviewEvidence {
		err = app.Execute(app.Command{Mode: app.ModeHandoff}, cfg, nil, &handoff, stderr)
	} else {
		err = runWorker(cfg.RepoRoot, []string{"--handoff"}, nil, &handoff, stderr, nil)
	}
	if err != nil {
		return writeTerminalHandoffFailure(stdout, terminalJSON, fmt.Errorf("canonical handoff failed after parent action: %w", err))
	}
	handoffJSON, err := decodeSingleMachineJSON(handoff.Bytes(), "canonical handoff")
	if err != nil {
		return writeTerminalHandoffFailure(stdout, terminalJSON, err)
	}
	return writeProjectedTerminalEnvelope(stdout, terminalJSON, handoffJSON)
}

func writeProjectedTerminalEnvelope(stdout io.Writer, terminalJSON, handoffJSON json.RawMessage) error {
	envelope, err := projectParentActionTerminalEnvelope(terminalJSON, handoffJSON)
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
	if err := json.NewEncoder(stdout).Encode(envelope); err != nil {
		return fmt.Errorf("%w; encode terminal handoff failure envelope: %w", handoffErr, err)
	}
	return handoffErr
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
