package failurepathtrial

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type shadowOutput struct {
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary"`
}

func ShadowSchemaJSON() (string, error) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"findings": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"target":   map[string]any{"type": "string"},
						"class":    map[string]any{"type": "string", "enum": Classes},
						"issue":    map[string]any{"type": "string"},
						"evidence": map[string]any{"type": "string"},
					},
					"required":             []string{"target", "class", "issue"},
					"additionalProperties": false,
				},
			},
			"summary": map[string]any{"type": "string"},
		},
		"required":             []string{"findings", "summary"},
		"additionalProperties": false,
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return "", fmt.Errorf("failure-path trial schemaをJSON化できません: %w", err)
	}
	return string(data), nil
}

func ParseShadowOutput(raw []byte) ([]Finding, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("failure-path reviewerの出力が空です")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output shadowOutput
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("failure-path reviewerの出力を解析できません: %w", err)
	}
	if len(output.Findings) > findingsMaxItems {
		return nil, fmt.Errorf("failure-path reviewerのfindingsが上限を超えています: %d", len(output.Findings))
	}
	classes := make(map[string]struct{}, len(Classes))
	for _, class := range Classes {
		classes[class] = struct{}{}
	}
	for index, finding := range output.Findings {
		if finding.Target == "" {
			return nil, fmt.Errorf("failure-path reviewerのfindings[%d].targetが空です", index)
		}
		if _, known := classes[finding.Class]; !known {
			return nil, fmt.Errorf("failure-path reviewerのfindings[%d].classが不正です: %q", index, finding.Class)
		}
		if finding.Issue == "" {
			return nil, fmt.Errorf("failure-path reviewerのfindings[%d].issueが空です", index)
		}
		output.Findings[index].Target = boundText(finding.Target, findingTextBoundBytes)
		output.Findings[index].Issue = boundText(finding.Issue, findingTextBoundBytes)
		output.Findings[index].Evidence = boundText(finding.Evidence, findingTextBoundBytes)
	}
	return output.Findings, nil
}
