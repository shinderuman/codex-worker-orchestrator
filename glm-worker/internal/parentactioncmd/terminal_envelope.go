package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type parentActionTerminalEnvelopePayload struct {
	Status       string          `json:"status"`
	Terminal     json.RawMessage `json:"terminal"`
	Handoff      json.RawMessage `json:"handoff,omitempty"`
	HandoffError string          `json:"handoff_error,omitempty"`
}

const (
	actionAccept = "accept"
	actionPark   = "park"
	actionReopen = "reopen"
	actionResume = "resume"
	actionUnpark = "unpark"
)

func executeWithTerminalEnvelope(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !terminalEnvelopeAction(args[0]) {
		return execute(cfg, args, stdout, stderr)
	}

	var terminal bytes.Buffer
	var err error
	switch args[0] {
	case actionReviewEvidence:
		err = executeParentReviewEvidence(cfg, args, &terminal)
	case string(parentaction.ActionDecision):
		err = executePreflightedDecision(cfg, args, &terminal, stderr)
	default:
		err = execute(cfg, args, &terminal, stderr)
	}
	if err != nil {
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

	return json.NewEncoder(stdout).Encode(parentActionTerminalEnvelope(terminalJSON, handoffJSON))
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
	case actionStart, actionApprove, actionAccept, actionResume, "no-go", actionRecordPublicationFinding, actionReopen, actionPark, actionUnpark, actionReviewEvidence:
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
