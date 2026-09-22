package repositoryharness

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const activeTaskStateKey = "active-task"

func RuntimeActive(repoRoot string, st *state.StateStore) (bool, error) {
	activation, pinned, err := readActivationPin(st)
	if err != nil {
		return false, err
	}
	if pinned {
		switch activation {
		case ActivationActiveValue:
			return true, nil
		case ActivationInactiveValue:
			if !st.Exists(activeTaskStateKey) {
				return false, nil
			}
			activeTask, err := st.Read(activeTaskStateKey)
			if err != nil {
				return false, fmt.Errorf("ACTIVE task pinを読み込めません: %w", err)
			}
			if activeTask != "" {
				return false, fmt.Errorf("repository harness activation pinがinactiveですがACTIVE task %sが固定されています", activeTask)
			}
			return false, nil
		default:
			return false, fmt.Errorf("repository harness activation pinが不正です: %q", activation)
		}
	}
	if st.Exists(activeTaskStateKey) {
		return false, fmt.Errorf("repository harness activation pinが欠落しています")
	}
	decision, err := Evaluate(repoRoot)
	if err != nil {
		return false, err
	}
	return decision.Active, nil
}

func RuntimeActivationPinned(st *state.StateStore) (bool, error) {
	_, pinned, err := readActivationPin(st)
	return pinned, err
}

func readActivationPin(st *state.StateStore) (string, bool, error) {
	data, err := os.ReadFile(st.Path(ActivationStateKey))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("repository harness activation pinを読み込めません: %w", err)
	}
	switch string(data) {
	case ActivationActiveValue + "\n":
		return ActivationActiveValue, true, nil
	case ActivationInactiveValue + "\n":
		return ActivationInactiveValue, true, nil
	default:
		return "", false, fmt.Errorf("repository harness activation pinが不正です: %q", string(data))
	}
}
