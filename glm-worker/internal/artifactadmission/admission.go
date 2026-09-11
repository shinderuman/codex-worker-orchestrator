package artifactadmission

import (
	"bytes"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
)

type sensitiveValue struct {
	category string
	value    string
}

const parentActionTokenCategory = "parent-action-token"

func Validate(artifacts []string, root string, cfg config.AppConfig) error {
	if err := packet.ValidateArtifacts(artifacts, root); err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return nil
	}
	values, err := sensitiveValues(cfg)
	if err != nil {
		return err
	}
	for _, path := range artifacts {
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("artifact content admission unavailable")
		}
		for _, candidate := range values {
			if candidate.value != "" && bytes.Contains(content, []byte(candidate.value)) {
				return fmt.Errorf("artifact contains machine-known sensitive value: %s", candidate.category)
			}
		}
	}
	return nil
}

func sensitiveValues(cfg config.AppConfig) ([]sensitiveValue, error) {
	providerValues, err := runner.SensitiveArtifactValues(cfg)
	if err != nil {
		return nil, fmt.Errorf("artifact sensitive-source admission unavailable: provider-runtime")
	}
	values := make([]sensitiveValue, 0, len(providerValues)+1)
	for _, candidate := range providerValues {
		if candidate.Value != "" {
			values = append(values, sensitiveValue{category: candidate.Category, value: candidate.Value})
		}
	}
	parentTokens, err := parentaction.LiveTokens(cfg.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("artifact sensitive-source admission unavailable: parent-action-token")
	}
	for _, token := range parentTokens {
		if token != "" {
			values = append(values, sensitiveValue{category: parentActionTokenCategory, value: token})
		}
	}
	return values, nil
}
