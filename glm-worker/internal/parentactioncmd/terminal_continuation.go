package parentactioncmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentActionTerminalContinuation struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

func writeTerminalProjectionContinuation(stdout io.Writer, failure parentActionTerminalEnvelopePayload, continuation parentActionTerminalContinuation, projectionErr error) error {
	plainFailure := cloneTerminalProjectionFailure(failure)
	failure.Continuation = &continuation
	if stats := failure.Projection; stats != nil {
		if statsErr := finalizeProjectionStats(&failure, stats); statsErr != nil {
			return encodeTerminalProjectionFallback(stdout, plainFailure, errors.Join(projectionErr, statsErr))
		}
		if stats.ProjectedBytes > stats.BudgetBytes {
			budgetErr := fmt.Errorf("terminal continuation envelope exceeds budget: projected=%d budget=%d", stats.ProjectedBytes, stats.BudgetBytes)
			return encodeTerminalProjectionFallback(stdout, plainFailure, errors.Join(projectionErr, budgetErr))
		}
	}
	if encodeErr := json.NewEncoder(stdout).Encode(failure); encodeErr != nil {
		return fmt.Errorf("%w; encode projection overflow continuation envelope: %w", projectionErr, encodeErr)
	}
	return projectionErr
}

func cloneTerminalProjectionFailure(failure parentActionTerminalEnvelopePayload) parentActionTerminalEnvelopePayload {
	clone := failure
	if failure.Projection != nil {
		stats := *failure.Projection
		stats.OmittedFields = append([]string(nil), failure.Projection.OmittedFields...)
		stats.ProjectedFields = append([]string(nil), failure.Projection.ProjectedFields...)
		clone.Projection = &stats
	}
	clone.Continuation = nil
	return clone
}

func encodeTerminalProjectionFallback(stdout io.Writer, failure parentActionTerminalEnvelopePayload, cause error) error {
	if encodeErr := json.NewEncoder(stdout).Encode(failure); encodeErr != nil {
		return errors.Join(cause, fmt.Errorf("encode projection overflow fallback envelope: %w", encodeErr))
	}
	return cause
}

func persistParentActionTerminalContinuation(cfg config.AppConfig, terminalJSON json.RawMessage) (parentActionTerminalContinuation, error) {
	st := state.AttachStateStore(cfg)
	dir, err := terminalContinuationArtifactDir(cfg, st, terminalJSON)
	if err != nil {
		return parentActionTerminalContinuation{}, err
	}
	digest := sha256.Sum256(terminalJSON)
	digestHex := hex.EncodeToString(digest[:])
	path := filepath.Join(dir, "parent-action-terminal-continuation-"+digestHex+".json")
	if existing, readErr := os.ReadFile(path); readErr == nil {
		if !bytes.Equal(existing, terminalJSON) {
			return parentActionTerminalContinuation{}, fmt.Errorf("terminal continuation digest collision at %s", path)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return parentActionTerminalContinuation{}, readErr
	} else if err := writeTerminalContinuationAtomic(path, terminalJSON); err != nil {
		return parentActionTerminalContinuation{}, err
	}
	return parentActionTerminalContinuation{
		Kind:    "terminal-json-artifact",
		Locator: path,
		Bytes:   len(terminalJSON),
		SHA256:  digestHex,
	}, nil
}

func terminalContinuationArtifactDir(cfg config.AppConfig, st *state.StateStore, terminalJSON json.RawMessage) (string, error) {
	if st.Exists("task.id") {
		return st.PrepareArtifactDir()
	}
	var terminal struct {
		TransitionID string `json:"transition_id"`
	}
	if err := json.Unmarshal(terminalJSON, &terminal); err != nil || terminal.TransitionID == "" {
		return st.PrepareArtifactDir()
	}
	store, err := controller.Open(cfg)
	if err != nil {
		return "", err
	}
	record, _, err := store.LoadTransition(terminal.TransitionID)
	if err != nil {
		return "", err
	}
	task, snapshotID := record.SourceExecutionTaskRef, record.ProjectSnapshotOld
	if task.Empty() {
		task, snapshotID = record.TargetExecutionTaskRef, record.ProjectSnapshotNew
	}
	snapshot, err := store.LoadProjectSnapshot(snapshotID)
	if err != nil {
		return "", err
	}
	authority, err := controller.ResolveCommittedTaskAuthorityAt(store.Identity().PrimaryRoot, snapshot.HeadOID)
	if err != nil {
		return "", err
	}
	if !authority.Task.Equal(task) || authority.ExecutionMode != controller.ExecutionAuthorityParentOnly {
		return "", fmt.Errorf("terminal continuation requires a bound parent-only operation")
	}
	identity := sha256.Sum256([]byte(task.TaskPath + "\x00" + task.ContractDigest))
	return st.PrepareArtifactDirForTask("controller-" + hex.EncodeToString(identity[:]))
}

func writeTerminalContinuationAtomic(path string, data []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".parent-action-terminal-continuation-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
