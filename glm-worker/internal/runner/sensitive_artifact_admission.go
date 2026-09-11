package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type SensitiveArtifactError struct {
	Category string
}

func (e *SensitiveArtifactError) Error() string {
	return "artifact containing machine-known sensitive value was rejected: " + e.Category
}

const sensitiveArtifactScanChunkBytes = 64 * 1024

func validateSensitiveResultArtifacts(base *ClaudeRunner, result RunResult, providerValues []SensitiveArtifactValue) error {
	artifacts, pathErr := sensitiveArtifactPaths(base, result)
	if len(artifacts) == 0 {
		return pathErr
	}
	values, err := sensitiveArtifactCandidates(base, providerValues)
	if err != nil {
		return err
	}
	if err := validateSensitiveArtifactContents(artifacts, values); err != nil {
		return err
	}
	return pathErr
}

func sensitiveArtifactPaths(base *ClaudeRunner, result RunResult) ([]string, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(result.StructuredOutput, &object); err != nil {
		return nil, nil
	}
	rawArtifacts, ok := object["artifacts"]
	if !ok {
		return nil, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(rawArtifacts, &entries); err != nil || len(entries) == 0 {
		return nil, nil
	}
	taskID, err := base.state.TaskID()
	if err != nil {
		return nil, fmt.Errorf("artifact sensitive admission unavailable: task-artifact-root")
	}
	root := base.state.ArtifactDir(taskID)
	artifacts := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	var validationErr error
	for _, entry := range entries {
		var path string
		if err := json.Unmarshal(entry, &path); err != nil {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		if err := packet.ValidateArtifacts([]string{path}, root); err != nil {
			if validationErr == nil {
				validationErr = err
			}
			continue
		}
		seen[path] = struct{}{}
		artifacts = append(artifacts, path)
	}
	return artifacts, validationErr
}

func sensitiveArtifactCandidates(base *ClaudeRunner, providerValues []SensitiveArtifactValue) ([]SensitiveArtifactValue, error) {
	values := append([]SensitiveArtifactValue(nil), providerValues...)
	parentTokens, err := parentaction.LiveTokens(base.config.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("artifact sensitive admission unavailable: parent-action-token")
	}
	for _, token := range parentTokens {
		values = append(values, SensitiveArtifactValue{Category: "parent-action-token", Value: token})
	}
	return values, nil
}

func validateSensitiveArtifactContents(artifacts []string, values []SensitiveArtifactValue) error {
	rejectedCategory := ""
	for _, path := range artifacts {
		category, err := sensitiveArtifactFileCategory(path, values)
		if err != nil {
			return fmt.Errorf("artifact sensitive admission unavailable: artifact-content")
		}
		if category == "" {
			continue
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("artifact sensitive admission cleanup failed: %s", category)
		}
		if rejectedCategory == "" {
			rejectedCategory = category
		}
	}
	if rejectedCategory != "" {
		return &SensitiveArtifactError{Category: rejectedCategory}
	}
	return nil
}

func sensitiveArtifactFileCategory(path string, values []SensitiveArtifactValue) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	maxValueBytes := 0
	for _, candidate := range values {
		if len(candidate.Value) > maxValueBytes {
			maxValueBytes = len(candidate.Value)
		}
	}
	if maxValueBytes == 0 {
		return "", nil
	}

	chunk := make([]byte, sensitiveArtifactScanChunkBytes)
	carry := make([]byte, 0, maxValueBytes-1)
	for {
		n, readErr := file.Read(chunk)
		if n > 0 {
			window := make([]byte, len(carry)+n)
			copy(window, carry)
			copy(window[len(carry):], chunk[:n])
			if category := sensitiveArtifactCategory(window, values); category != "" {
				return category, nil
			}
			keep := maxValueBytes - 1
			if keep > len(window) {
				keep = len(window)
			}
			carry = append(carry[:0], window[len(window)-keep:]...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return "", nil
			}
			return "", readErr
		}
	}
}

func sensitiveArtifactCategory(content []byte, values []SensitiveArtifactValue) string {
	for _, candidate := range values {
		if candidate.Value != "" && bytes.Contains(content, []byte(candidate.Value)) {
			return candidate.Category
		}
	}
	return ""
}
