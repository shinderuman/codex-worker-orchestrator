package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func validateSensitiveResultArtifacts(base *ClaudeRunner, result RunResult, providerValues []SensitiveArtifactValue) error {
	artifacts, err := sensitiveArtifactPaths(base, result)
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return nil
	}
	values, err := sensitiveArtifactCandidates(base, providerValues)
	if err != nil {
		return err
	}
	return validateSensitiveArtifactContents(artifacts, values)
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
	for _, entry := range entries {
		var path string
		if err := json.Unmarshal(entry, &path); err != nil {
			continue
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		if err := packet.ValidateArtifacts([]string{path}, root); err != nil {
			continue
		}
		seen[path] = struct{}{}
		artifacts = append(artifacts, path)
	}
	return artifacts, nil
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
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("artifact sensitive admission unavailable: artifact-content")
		}
		category := sensitiveArtifactCategory(content, values)
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

func sensitiveArtifactCategory(content []byte, values []SensitiveArtifactValue) string {
	for _, candidate := range values {
		if candidate.Value != "" && bytes.Contains(content, []byte(candidate.Value)) {
			return candidate.Category
		}
	}
	return ""
}
