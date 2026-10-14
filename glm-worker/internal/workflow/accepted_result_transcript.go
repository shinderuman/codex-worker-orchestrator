package workflow

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type acceptedResultTranscriptEntry struct {
	SessionID string    `json:"sessionId"`
	CWD       string    `json:"cwd"`
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type acceptedResultTranscriptBlock struct {
	Type  string          `json:"type"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

func (w *Workflow) acceptedResultFromTranscript(path string, call state.ModelCallLog) (packet.Result, error) {
	paths, err := runner.FindClaudeTranscriptPaths(w.config.ClaudeConfigDir, []string{call.SessionID})
	if err != nil {
		return packet.Result{}, err
	}
	if matches := paths[call.SessionID]; len(matches) != 1 || filepath.Clean(matches[0]) != filepath.Clean(path) {
		return packet.Result{}, fmt.Errorf("accepted-result recovery transcript identity is not unique")
	}
	inputs, err := w.readAcceptedResultTranscriptInputs(path, call)
	if err != nil {
		return packet.Result{}, err
	}
	if len(inputs) != 1 || !equalResultRecoveryJSON(inputs[0], []byte(call.Response)) {
		return packet.Result{}, fmt.Errorf("accepted-result recovery structured packet is missing, ambiguous, or differs from the saved response")
	}
	result, err := packet.ParseStructured(inputs[0])
	if err != nil {
		return result, err
	}
	if err := packet.ValidateWorkerResult(result); err != nil {
		return result, err
	}
	return result, packet.ValidateArtifacts(result.Artifacts, w.state.ArtifactDir(call.TaskID))
}

func (w *Workflow) readAcceptedResultTranscriptInputs(path string, call state.ModelCallLog) ([]json.RawMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	var inputs []json.RawMessage
	for scanner.Scan() {
		var entry acceptedResultTranscriptEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return nil, err
		}
		if entry.Timestamp.Before(call.StartedAt) || entry.Timestamp.After(call.CompletedAt) {
			continue
		}
		found, err := w.acceptedResultInputsForEntry(entry, call)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, found...)
	}
	return inputs, scanner.Err()
}

func (w *Workflow) acceptedResultInputsForEntry(entry acceptedResultTranscriptEntry, call state.ModelCallLog) ([]json.RawMessage, error) {
	blocks, err := acceptedResultBlocks(entry.Message.Content)
	if err != nil {
		return nil, err
	}
	var inputs []json.RawMessage
	for _, block := range blocks {
		if block.Type != "tool_use" || block.Name != "StructuredOutput" {
			continue
		}
		if entry.SessionID != call.SessionID || filepath.Clean(entry.CWD) != filepath.Clean(w.config.RepoRoot) {
			return nil, fmt.Errorf("accepted-result recovery transcript session/workspace mismatch")
		}
		inputs = append(inputs, block.Input)
	}
	return inputs, nil
}

func acceptedResultBlocks(raw json.RawMessage) ([]acceptedResultTranscriptBlock, error) {
	if len(raw) == 0 || raw[0] != '[' {
		return nil, nil
	}
	var blocks []acceptedResultTranscriptBlock
	err := json.Unmarshal(raw, &blocks)
	return blocks, err
}

func equalResultRecoveryJSON(first, second []byte) bool {
	var left, right any
	if json.Unmarshal(first, &left) != nil || json.Unmarshal(second, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}
