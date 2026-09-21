package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

type analysisRolloutItemPayloadRaw struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Input     string `json:"input"`
}

type analysisExecPragma struct {
	yieldMS  uint64
	hasYield bool
}

const (
	analysisWaitSessionIDKey       = "session_id"
	analysisWaitCharsKey           = "chars"
	analysisWaitYieldMSKey         = "yield_time_ms"
	analysisExecYieldMSKey         = "yield_time_ms"
	analysisWaitMaxOutputTokensKey = "max_output_tokens"
	analysisWaitWrapperPrefix      = "constr=awaittools.write_stdin("
)

var analysisWaitWrapperSuffixes = []string{
	");text(r);",
	");text(JSON.stringify(r));",
}

func (item *codexRolloutItemPayload) UnmarshalJSON(data []byte) error {
	var raw analysisRolloutItemPayloadRaw
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*item = codexRolloutItemPayload{
		Type:      raw.Type,
		Name:      raw.Name,
		CallID:    raw.CallID,
		Arguments: raw.Arguments,
	}

	switch raw.Type {
	case codexRolloutCustomToolCallType:
		analysisNormalizeCustomWait(item, raw)
	case codexRolloutCustomToolCallOutputType:
		item.Type = codexRolloutFunctionCallOutputType
	}
	return nil
}

func analysisNormalizeCustomWait(item *codexRolloutItemPayload, raw analysisRolloutItemPayloadRaw) {
	if raw.Name != "exec" {
		return
	}
	yieldMS, recognized := analysisCanonicalCustomWriteStdinWait(raw.Input)
	if !recognized {
		return
	}
	item.Type = codexRolloutFunctionCallType
	item.Name = codexRolloutWaitCallName
	item.Arguments = "{}"
	if yieldMS != nil {
		item.Arguments = "{\"yield-time_ms\":" + strconv.FormatUint(*yieldMS, 10) + "}"
	}
}

func analysisCanonicalCustomWriteStdinWait(input string) (*uint64, bool) {
	code, pragma, ok := analysisStripExecPragma(input)
	if !ok {
		return nil, false
	}
	compact := analysisCompactWaitWrapper(code)
	if !strings.HasPrefix(compact, analysisWaitWrapperPrefix) {
		return nil, false
	}
	objectWithTail := strings.TrimPrefix(compact, analysisWaitWrapperPrefix)
	object, ok := analysisWaitWrapperObject(objectWithTail)
	if !ok {
		return nil, false
	}
	yieldMS, recognized := analysisCanonicalWriteStdinObject(object)
	if !recognized {
		return nil, false
	}
	if pragma.hasYield && yieldMS != nil && pragma.yieldMS != *yieldMS {
		return nil, true
	}
	return yieldMS, true
}

func analysisWaitWrapperObject(value string) (string, bool) {
	for _, suffix := range analysisWaitWrapperSuffixes {
		if strings.HasSuffix(value, suffix) {
			return strings.TrimSuffix(value, suffix), true
		}
	}
	return "", false
}

func analysisStripExecPragma(input string) (string, analysisExecPragma, bool) {
	trimmed := strings.TrimSpace(input)
	pragma := analysisExecPragma{}
	if !strings.HasPrefix(trimmed, "// @exec:") {
		return trimmed, pragma, true
	}
	lineEnd := strings.IndexByte(trimmed, '\n')
	if lineEnd < 0 {
		return "", pragma, false
	}
	directive := strings.TrimSpace(strings.TrimPrefix(trimmed[:lineEnd], "// @exec:"))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(directive), &fields); err != nil {
		return "", pragma, false
	}
	for key, value := range fields {
		if key != analysisExecYieldMSKey && key != analysisWaitMaxOutputTokensKey {
			return "", pragma, false
		}
		var number uint64
		if err := json.Unmarshal(value, &number); err != nil {
			return "", pragma, false
		}
		if key == analysisExecYieldMSKey {
			pragma.yieldMS = number
			pragma.hasYield = true
		}
	}
	return strings.TrimSpace(trimmed[lineEnd+1:]), pragma, true
}

func analysisCanonicalWriteStdinObject(object string) (*uint64, bool) {
	if len(object) < 2 || object[0] != '{' || object[len(object)-1] != '}' {
		return nil, false
	}
	seen := map[string]bool{}
	var yieldMS *uint64
	yieldUnknown := false
	for _, field := range strings.Split(object[1:len(object)-1], ",") {
		key, value, ok := strings.Cut(field, ":")
		if !ok || !analysisApplyWriteStdinField(seen, strings.TrimSpace(key), strings.TrimSpace(value), &yieldMS, &yieldUnknown) {
			return nil, false
		}
	}
	if !seen[analysisWaitSessionIDKey] || !seen[analysisWaitCharsKey] {
		return nil, false
	}
	if yieldUnknown {
		return nil, true
	}
	return yieldMS, true
}

func analysisApplyWriteStdinField(seen map[string]bool, key, value string, yieldMS **uint64, yieldUnknown *bool) bool {
	if seen[key] {
		if key != analysisWaitYieldMSKey {
			return false
		}
		*yieldMS = nil
		*yieldUnknown = true
		return true
	}
	seen[key] = true

	switch key {
	case analysisWaitSessionIDKey:
		_, ok := analysisUnsignedJSLiteral(value)
		return ok
	case analysisWaitCharsKey:
		return value == "\"\"" || value == "''"
	case analysisWaitYieldMSKey:
		parsed, ok := analysisUnsignedJSLiteral(value)
		if !ok {
			*yieldUnknown = true
			return true
		}
		*yieldMS = &parsed
		return true
	case analysisWaitMaxOutputTokensKey:
		_, ok := analysisUnsignedJSLiteral(value)
		return ok
	default:
		return false
	}
}

func analysisUnsignedJSLiteral(raw string) (uint64, bool) {
	if raw == "" {
		return 0, false
	}
	for _, r := range raw {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	return value, err == nil
}

func analysisCompactWaitWrapper(value string) string {
	replacer := strings.NewReplacer(" ", "", "\t", "", "\n", "", "\r", "")
	return replacer.Replace(strings.TrimSpace(value))
}
