package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

const (
	actionAccept = "accept"
	actionPark   = "park"
)

type parentActionTerminalEnvelope struct {
	Status   string          `json:"status"`
	Terminal json.RawMessage `json:"terminal"`
	Handoff  json.RawMessage `json:"handoff"`
}

func executeWithTerminalEnvelope(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || !terminalEnvelopeAction(args[0]) {
		return execute(cfg, args, stdout, stderr)
	}

	var terminal bytes.Buffer
	if err := execute(cfg, args, &terminal, stderr); err != nil {
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
	if err := runWorker(cfg.RepoRoot, []string{"--handoff"}, nil, &handoff, stderr, nil); err != nil {
		return fmt.Errorf("canonical handoff failed after parent action: %w", err)
	}
	handoffJSON, err := decodeSingleMachineJSON(handoff.Bytes(), "canonical handoff")
	if err != nil {
		return err
	}

	return json.NewEncoder(stdout).Encode(parentActionTerminalEnvelope{
		Status:   "parent_action_terminal",
		Terminal: terminalJSON,
		Handoff:  handoffJSON,
	})
}

func terminalEnvelopeAction(action string) bool {
	if descriptor, ok := parentaction.LookupPayloadAction(action); ok {
		return descriptor.Action != parentaction.ActionReviseMilestones
	}
	switch action {
	case actionStart, actionApprove, actionAccept, "resume", "no-go", actionPark, "unpark":
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
