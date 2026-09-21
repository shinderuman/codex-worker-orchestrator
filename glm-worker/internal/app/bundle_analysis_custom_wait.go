package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

type analysisExecPragma struct {
	yieldMS  uint64
	hasYield bool
}

type analysisWaitQuoteState struct {
	quote   byte
	escaped bool
}

const (
	analysisWaitSessionIDKey       = "session_id"
	analysisWaitCharsKey           = "chars"
	analysisWaitYieldMSKey         = "yield" + "_" + "time_ms"
	analysisExecYieldMSKey         = "yield" + "-" + "time_ms"
	analysisWaitMaxOutputTokensKey = "max_output_tokens"
	analysisWaitWrapperPrefix      = "constr=awaittools.write_stdin("
	analysisWaitWrapperTextSuffix  = ");text(r);"
	analysisWaitWrapperJSONSuffix  = ");text(JSON.stringify(r));"
)

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

func analysisCustomWaitRequestedYield(input string) (*float64, bool) {
	yieldMS, recognized := analysisCanonicalCustomWriteStdinWait(input)
	if !recognized || yieldMS == nil {
		return nil, recognized
	}
	value := float64(*yieldMS)
	return &value, true
}

func analysisWaitWrapperObject(value string) (string, bool) {
	if strings.HasSuffix(value, analysisWaitWrapperTextSuffix) {
		return strings.TrimSuffix(value, analysisWaitWrapperTextSuffix), true
	}
	if strings.HasSuffix(value, analysisWaitWrapperJSONSuffix) {
		return strings.TrimSuffix(value, analysisWaitWrapperJSONSuffix), true
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
	var builder strings.Builder
	state := analysisWaitQuoteState{}
	for index := 0; index < len(value); index++ {
		ch := value[index]
		if state.consumeQuoted(&builder, ch) {
			continue
		}
		if ch == '\'' || ch == '"' {
			state.quote = ch
			builder.WriteByte(ch)
			continue
		}
		switch ch {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			builder.WriteByte(ch)
		}
	}
	return builder.String()
}

func (state *analysisWaitQuoteState) consumeQuoted(builder *strings.Builder, ch byte) bool {
	if state.quote == 0 {
		return false
	}
	builder.WriteByte(ch)
	if state.escaped {
		state.escaped = false
		return true
	}
	if ch == '\\' {
		state.escaped = true
		return true
	}
	if ch == state.quote {
		state.quote = 0
	}
	return true
}
