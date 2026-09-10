package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentWaitOutput struct {
	Status     string           `json:"status"`
	TaskStatus state.TaskStatus `json:"task_status"`
	OwnerLost  bool             `json:"owner_lost"`
	Handoff    json.RawMessage  `json:"handoff"`
}

const parentWaitLockFile = "parent-wait.lock"

const parentWaitStatusReleased = "released"

func withParentWaitLease(cfg config.AppConfig, body func() error) error {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		return fmt.Errorf("parent wait owner is already active: %w", err)
	}
	defer func() { _ = lock.Close() }()
	return body()
}

func executeParentWait(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action wait")
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	owner, err := repolock.AcquireWait(st.Path(parentWaitLockFile))
	if err != nil {
		return fmt.Errorf("wait for parent owner: %w", err)
	}
	defer func() { _ = owner.Close() }()

	worker, err := repolock.AcquireWait(st.LockPath())
	if err != nil {
		return fmt.Errorf("wait for worker owner: %w", err)
	}
	if err := worker.Close(); err != nil {
		return err
	}

	handoff, err := parentWaitHandoff(cfg, stderr)
	if err != nil {
		return err
	}
	status := st.TaskStatus()
	return json.NewEncoder(stdout).Encode(parentWaitOutput{
		Status:     parentWaitStatusReleased,
		TaskStatus: status,
		OwnerLost:  status == state.TaskStatusActive,
		Handoff:    handoff,
	})
}

func parentWaitHandoff(cfg config.AppConfig, stderr io.Writer) (json.RawMessage, error) {
	var output bytes.Buffer
	if err := runWorker(cfg.RepoRoot, []string{"--handoff", "recovery"}, nil, &output, stderr, nil); err != nil {
		return nil, err
	}
	data := bytes.TrimSpace(output.Bytes())
	var object map[string]any
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("parent wait recovery handoff is not a JSON object")
	}
	return json.RawMessage(bytes.Clone(data)), nil
}
