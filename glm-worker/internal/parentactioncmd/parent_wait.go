package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

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

type parentWaitRecoveryHandoff struct {
	Projection     string   `json:"projection"`
	Consistent     *bool    `json:"consistent"`
	TaskID         *string  `json:"task_id"`
	TaskStatus     *string  `json:"task_status"`
	RequiredAction *string  `json:"required_action"`
	AllowedActions []string `json:"allowed_actions"`
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
	defer func() { _ = worker.Close() }()

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
	if err := validateParentWaitRecoveryHandoff(data); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.Clone(data)), nil
}

func validateParentWaitRecoveryHandoff(data []byte) error {
	var handoff parentWaitRecoveryHandoff
	if err := json.Unmarshal(data, &handoff); err != nil {
		return fmt.Errorf("parent wait recovery handoff is not a JSON object")
	}
	if handoff.Projection != "recovery" {
		return fmt.Errorf("parent wait recovery handoff has invalid projection")
	}
	if handoff.Consistent == nil {
		return fmt.Errorf("parent wait recovery handoff is missing consistent")
	}
	if handoff.TaskID == nil || strings.TrimSpace(*handoff.TaskID) == "" {
		return fmt.Errorf("parent wait recovery handoff is missing task_id")
	}
	if handoff.TaskStatus == nil || !state.TaskStatus(*handoff.TaskStatus).Known() {
		return fmt.Errorf("parent wait recovery handoff has invalid task_status")
	}
	if handoff.RequiredAction == nil || strings.TrimSpace(*handoff.RequiredAction) == "" {
		return fmt.Errorf("parent wait recovery handoff is missing required_action")
	}
	if handoff.AllowedActions == nil {
		return fmt.Errorf("parent wait recovery handoff is missing allowed_actions")
	}
	return nil
}
