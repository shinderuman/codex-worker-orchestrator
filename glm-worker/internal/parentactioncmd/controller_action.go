package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func executeControllerAction(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 2 || !parentaction.IsControllerAction(args[0]) {
		return fmt.Errorf("usage: glm-parent-action <controller-semantic|controller-execution|controller-publication|controller-evidence> <token>")
	}
	payload, err := parentaction.Peek(cfg.RepoRoot, args[0], args[1])
	if err != nil {
		return err
	}
	var command map[string]json.RawMessage
	if err := json.Unmarshal(payload, &command); err != nil {
		return fmt.Errorf("controller command JSON: %w", err)
	}
	if command == nil {
		return fmt.Errorf("controller command must be a JSON object")
	}
	if err := runWorker(cfg.RepoRoot, []string{"--authority", args[0]}, bytes.NewReader(payload), stdout, stderr, nil); err != nil {
		return err
	}
	_, err = parentaction.ConsumeExpected(cfg.RepoRoot, args[0], args[1], payload)
	return err
}
