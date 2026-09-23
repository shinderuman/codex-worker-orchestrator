package abeval

import (
	"encoding/json"
	"fmt"
)

func (u *CodexUsage) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	if fields == nil {
		return fmt.Errorf("codex_usageはobjectである必要があります")
	}
	for name := range fields {
		switch name {
		case "source", "input_tokens", "output_tokens":
		default:
			return fmt.Errorf("codex_usageに未知fieldがあります: %s", name)
		}
	}

	var raw struct {
		Source       string `json:"source"`
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.Source != "" && (raw.InputTokens == nil || raw.OutputTokens == nil) {
		return fmt.Errorf("codex_usage.sourceが設定されている場合はinput_tokens/output_tokensの明示値が必要です")
	}

	*u = CodexUsage{Source: raw.Source}
	if raw.InputTokens != nil {
		u.InputTokens = *raw.InputTokens
	}
	if raw.OutputTokens != nil {
		u.OutputTokens = *raw.OutputTokens
	}
	return nil
}
