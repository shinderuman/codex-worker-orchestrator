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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentActionTerminalContinuation struct {
	Kind    string `json:"kind"`
	Locator string `json:"locator"`
	Bytes   int    `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type parentActionTerminalContinuationEnvelope struct {
	parentActionTerminalEnvelopePayload
	Continuation parentActionTerminalContinuation `json:"continuation"`
}

func writeProjectedTerminalEnvelopeWithContinuation(cfg config.AppConfig, stdout io.Writer, terminalJSON, handoffJSON json.RawMessage) error {
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
	continuation, continuationErr := persistParentActionTerminalContinuation(cfg, terminalJSON)
	if continuationErr != nil {
		return errors.Join(err, fmt.Errorf("persist terminal continuation: %w", continuationErr))
	}
	wrapped := parentActionTerminalContinuationEnvelope{
		parentActionTerminalEnvelopePayload: failure,
		Continuation:                       continuation,
	}
	if stats := wrapped.Projection; stats != nil {
		if statsErr := finalizeTerminalContinuationStats(&wrapped, stats); statsErr != nil {
			return errors.Join(err, statsErr)
		}
		if stats.ProjectedBytes > stats.BudgetBytes {
			return fmt.Errorf("%w; terminal continuation envelope exceeds budget: projected=%d budget=%d", err, stats.ProjectedBytes, stats.BudgetBytes)
		}
	}
	if encodeErr := json.NewEncoder(stdout).Encode(wrapped); encodeErr != nil {
		return fmt.Errorf("%w; encode projection overflow continuation envelope: %w", err, encodeErr)
	}
	return err
}

func finalizeTerminalContinuationStats(envelope *parentActionTerminalContinuationEnvelope, stats *parentActionTerminalProjectionStats) error {
	for iteration := 0; iteration < 3; iteration++ {
		raw, err := json.Marshal(envelope)
		if err != nil {
			return fmt.Errorf("marshal projected terminal continuation envelope: %w", err)
		}
		projectedBytes := encodedJSONLineBytes(raw)
		if stats.ProjectedBytes == projectedBytes {
			break
		}
		stats.ProjectedBytes = projectedBytes
		stats.SavedBytes = stats.RawBytes - stats.ProjectedBytes
		if stats.SavedBytes < 0 {
			stats.SavedBytes = 0
		}
	}
	return nil
}

func persistParentActionTerminalContinuation(cfg config.AppConfig, terminalJSON json.RawMessage) (parentActionTerminalContinuation, error) {
	st := state.AttachStateStore(cfg)
	dir, err := st.PrepareArtifactDir()
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
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
