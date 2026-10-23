package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"io"
	"path/filepath"
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
	Inconsistency  *string         `json:"inconsistency"`
	Projection     string          `json:"projection"`
	Consistent     *bool           `json:"consistent"`
	Controller     json.RawMessage `json:"controller"`
	TaskID         *string         `json:"task_id"`
	TaskStatus     *string         `json:"task_status"`
	RequiredAction *string         `json:"required_action"`
	AllowedActions []string        `json:"allowed_actions"`
}

const parentWaitLockFile = "parent-wait.lock"
const parentWaitRecoveryLockFile = "parent-wait-recovery.lock"
const parentWaitOwnerEpochFile = "parent-wait-owner.epoch"

const parentWaitStatusReleased = "released"
const parentWaitStatusSuperseded = "superseded"

func withParentWaitLease(cfg config.AppConfig, body func() error) error {
	st, err := parentWaitStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.Path(parentWaitLockFile))
	if err != nil {
		return fmt.Errorf("parent wait owner is already active: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if _, err := rotateParentWaitOwnerEpoch(st); err != nil {
		return err
	}
	return body()
}

func rotateParentWaitOwnerEpoch(st *state.StateStore) (string, error) {
	epoch, err := state.NewUUID()
	if err != nil {
		return "", fmt.Errorf("create parent wait owner epoch: %w", err)
	}
	if err := st.Write(parentWaitOwnerEpochFile, epoch); err != nil {
		return "", fmt.Errorf("persist parent wait owner epoch: %w", err)
	}
	return epoch, nil
}

func readParentWaitOwnerEpoch(st *state.StateStore) (string, error) {
	epoch, err := st.Read(parentWaitOwnerEpochFile)
	if err != nil {
		return "", fmt.Errorf("read parent wait owner epoch: %w", err)
	}
	if !state.ValidGeneratedUUID(epoch) {
		return "", fmt.Errorf("parent wait owner epoch is invalid")
	}
	return epoch, nil
}

func executeParentWait(cfg config.AppConfig, args []string, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action wait")
	}
	st, err := parentWaitStore(cfg)
	if err != nil {
		return err
	}
	expectedEpoch, err := readParentWaitOwnerEpoch(st)
	if err != nil {
		return fmt.Errorf("bind parent recovery waiter: %w", err)
	}

	recovery, err := repolock.Acquire(st.Path(parentWaitRecoveryLockFile))
	if err != nil {
		return fmt.Errorf("acquire parent recovery waiter: %w", err)
	}
	defer func() { _ = recovery.Close() }()

	owner, err := repolock.AcquireWait(st.Path(parentWaitLockFile))
	if err != nil {
		return fmt.Errorf("wait for parent owner: %w", err)
	}
	defer func() { _ = owner.Close() }()

	currentEpoch, err := readParentWaitOwnerEpoch(st)
	if err != nil {
		return fmt.Errorf("verify parent recovery owner epoch: %w", err)
	}
	if currentEpoch != expectedEpoch {
		return encodeParentWaitSuperseded(stdout)
	}

	worker, err := repolock.AcquireWait(state.AttachStateStore(cfg).LockPath())
	if err != nil {
		return fmt.Errorf("wait for worker owner: %w", err)
	}
	defer func() { _ = worker.Close() }()

	currentEpoch, err = readParentWaitOwnerEpoch(st)
	if err != nil {
		return fmt.Errorf("verify parent recovery owner epoch after worker: %w", err)
	}
	if currentEpoch != expectedEpoch {
		return encodeParentWaitSuperseded(stdout)
	}

	handoff, err := parentWaitHandoff(cfg, stderr)
	if err != nil {
		return err
	}
	status := state.AttachStateStore(cfg).TaskStatus()
	return json.NewEncoder(stdout).Encode(parentWaitOutput{
		Status:     parentWaitStatusReleased,
		TaskStatus: status,
		OwnerLost:  status == state.TaskStatusActive,
		Handoff:    handoff,
	})
}

func encodeParentWaitSuperseded(stdout io.Writer) error {
	return json.NewEncoder(stdout).Encode(struct {
		Status string `json:"status"`
	}{Status: parentWaitStatusSuperseded})
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
	if !*handoff.Consistent {
		return validateInconsistentRecoveryHandoff(handoff)
	}
	if !handoffHasRuntimeIdentity(handoff) && !controllerRecoveryHandoff(handoff) {
		return fmt.Errorf("parent wait recovery handoff has no Runtime or controller authority")
	}
	if handoff.RequiredAction == nil || strings.TrimSpace(*handoff.RequiredAction) == "" {
		return fmt.Errorf("parent wait recovery handoff is missing required_action")
	}
	if handoff.AllowedActions == nil {
		return fmt.Errorf("parent wait recovery handoff is missing allowed_actions")
	}
	return nil
}

func controllerRecoveryHandoff(handoff parentWaitRecoveryHandoff) bool {
	var report controller.ControllerStatusReport
	if json.Unmarshal(handoff.Controller, &report) != nil || !report.CanonicalActive || report.Head.RepositoryIdentity == "" || handoff.RequiredAction == nil || handoff.Consistent == nil || !*handoff.Consistent {
		return false
	}
	return *handoff.RequiredAction == "controller-execution" || *handoff.RequiredAction == "controller-publication" || *handoff.RequiredAction == "start"
}

func parentWaitStore(cfg config.AppConfig) (*state.StateStore, error) {
	if cfg.RepositoryLockPath != "" {
		cfg.StateBase = filepath.Dir(cfg.RepositoryLockPath)
		cfg.RepoHash = filepath.Base(cfg.RepositoryLockPath) + ".parent-wait"
	}
	return state.NewStateStore(cfg)
}

func handoffHasRuntimeIdentity(handoff parentWaitRecoveryHandoff) bool {
	return handoff.TaskID != nil && strings.TrimSpace(*handoff.TaskID) != "" && handoff.TaskStatus != nil && state.TaskStatus(*handoff.TaskStatus).Known()
}

func validateInconsistentRecoveryHandoff(handoff parentWaitRecoveryHandoff) error {
	if handoff.Inconsistency == nil || strings.TrimSpace(*handoff.Inconsistency) == "" {
		return fmt.Errorf("inconsistent recovery handoff has no diagnostic")
	}
	if !handoffHasRuntimeIdentity(handoff) && !handoffHasControllerAuthority(handoff) {
		return fmt.Errorf("inconsistent recovery handoff has no Runtime or controller authority")
	}
	return nil
}

func handoffHasControllerAuthority(handoff parentWaitRecoveryHandoff) bool {
	var report controller.ControllerStatusReport
	return json.Unmarshal(handoff.Controller, &report) == nil && report.CanonicalActive && report.Head.RepositoryIdentity != ""
}
