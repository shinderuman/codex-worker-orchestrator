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

func validateSensitiveResultArtifacts(base *ClaudeRunner, result RunResult) error {
	parsed, err := packet.ParseStructured(result.StructuredOutput)
	if err != nil || len(parsed.Artifacts) == 0 {
		return nil
	}
	taskID, err := base.state.TaskID()
	if err != nil {
		return fmt.Errorf("artifact sensitive admission unavailable: task identity")
	}
	artifactRoot := base.state.ArtifactDir(taskID)
	if err := packet.ValidateArtifacts(parsed.Artifacts, artifactRoot); err != nil {
		return nil
	}
	values, err := SensitiveArtifactValues(base.config)
	if err != nil {
		return fmt.Errorf("artifact sensitive admission unavailable: provider-runtime")
	}
	parentTokens, err := parentaction.LiveTokens(base.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("artifact sensitive admission unavailable: parent-action-token")
	}
	for _, token := range parentTokens {
		if token != "" {
			values = append(values, SensitiveArtifactValue{Category: "parent-action-token", Value: token})
		}
	}
	for _, path := range parsed.Artifacts {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("artifact sensitive admission unavailable: artifact-content")
		}
		for _, candidate := range values {
			if candidate.Value != "" && bytes.Contains(content, []byte(candidate.Value)) {
				return &SensitiveArtifactError{Category: candidate.Category}
			}
		}
	}
	return nil
}
