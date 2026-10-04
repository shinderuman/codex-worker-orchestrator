package failurepathadvisory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

type advisoryOutput struct {
	Findings []Finding `json:"findings"`
	Summary  string    `json:"summary"`
}

func StructuredSchemaJSON() (string, error) {
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
						"status":   map[string]any{"type": "string", "enum": []string{FindingStatusVerified, FindingStatusIndeterminate}},
					},
					"required":             []string{"target", "class", "issue", "status"},
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
		return "", fmt.Errorf("failure-path advisory schemaをJSON化できません: %w", err)
	}
	return string(data), nil
}

func ParseStructuredOutput(raw []byte) ([]Finding, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("failure-path reviewerの出力が空です")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var output advisoryOutput
	if err := decoder.Decode(&output); err != nil {
		return nil, fmt.Errorf("failure-path reviewerの出力を解析できません: %w", err)
	}
	if err := requireStructuredOutputEOF(decoder); err != nil {
		return nil, err
	}
	if len(output.Findings) > findingsMaxItems {
		return nil, fmt.Errorf("failure-path reviewerのfindingsが上限を超えています: %d", len(output.Findings))
	}
	classes := knownFailurePathClasses()
	for index, finding := range output.Findings {
		bounded, err := validateAndBoundFinding(index, finding, classes)
		if err != nil {
			return nil, err
		}
		output.Findings[index] = bounded
	}
	return output.Findings, nil
}

func requireStructuredOutputEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("failure-path reviewerの出力に複数のJSON値があります")
		}
		return fmt.Errorf("failure-path reviewerの出力末尾を解析できません: %w", err)
	}
	return nil
}

func knownFailurePathClasses() map[string]struct{} {
	classes := make(map[string]struct{}, len(Classes))
	for _, class := range Classes {
		classes[class] = struct{}{}
	}
	return classes
}

func validateAndBoundFinding(index int, finding Finding, classes map[string]struct{}) (Finding, error) {
	if finding.Target == "" {
		return Finding{}, fmt.Errorf("failure-path reviewerのfindings[%d].targetが空です", index)
	}
	if _, known := classes[finding.Class]; !known {
		return Finding{}, fmt.Errorf("failure-path reviewerのfindings[%d].classが不正です: %q", index, finding.Class)
	}
	if finding.Issue == "" {
		return Finding{}, fmt.Errorf("failure-path reviewerのfindings[%d].issueが空です", index)
	}
	if finding.Status != FindingStatusVerified && finding.Status != FindingStatusIndeterminate {
		return Finding{}, fmt.Errorf("failure-path reviewerのfindings[%d].statusが不正です: %q", index, finding.Status)
	}
	finding.Target = boundText(finding.Target, findingTextBoundBytes)
	finding.Issue = boundText(finding.Issue, findingTextBoundBytes)
	finding.Evidence = boundText(finding.Evidence, findingTextBoundBytes)
	return finding, nil
}
