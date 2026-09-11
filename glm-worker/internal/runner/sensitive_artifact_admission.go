package runner

import (
	"bytes"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

type SensitiveArtifactError struct {
	Category string
}

func (e *SensitiveArtifactError) Error() string {
	return "artifact contains machine-known sensitive value: " + e.Category
}

func validateSensitiveResultArtifacts(base *ClaudeRunner, result RunResult, providerValues []SensitiveArtifactValue) error {
	artifacts, ok := sensitiveArtifactPaths(base, result)
	if !ok {
		return nil
	}
	values, err := sensitiveArtifactCandidates(base, providerValues)
	if err != nil {
		return err
	}
	return validateSensitiveArtifactContents(artifacts, values)
}

func sensitiveArtifactPaths(base *ClaudeRunner, result RunResult) ([]string, bool) {
	parsed, err := packet.ParseStructured(result.StructuredOutput)
	if err != nil || len(parsed.Artifacts) == 0 {
		return nil, false
	}
	taskID, err := base.state.TaskID()
	if err != nil {
		return nil, false
	}
	if err := packet.ValidateArtifacts(parsed.Artifacts, base.state.ArtifactDir(taskID)); err != nil {
		return nil, false
	}
	return parsed.Artifacts, true
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
	for _, path := range artifacts {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("artifact sensitive admission unavailable: artifact-content")
		}
		if err := rejectSensitiveArtifactContent(content, values); err != nil {
			return err
		}
	}
	return nil
}

func rejectSensitiveArtifactContent(content []byte, values []SensitiveArtifactValue) error {
	for _, candidate := range values {
		if candidate.Value != "" && bytes.Contains(content, []byte(candidate.Value)) {
			return &SensitiveArtifactError{Category: candidate.Category}
		}
	}
	return nil
}
