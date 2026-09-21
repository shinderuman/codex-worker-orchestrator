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

// UnmarshalJSON keeps the existing wait scanner transport-neutral. Legacy
// function_call records are copied as-is. A current Code Mode exec record is
// normalized to the legacy wait shape only when its complete source matches the
// bounded write_stdin wrapper accepted below. Other JavaScript remains a normal
// custom_tool_call and is invisible to parent_wait_calls.
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
		if raw.Name != "exec" {
			return nil
		}
		yieldMS, recognized := analysisCanonicalCustomWriteStdinWait(raw.Input)
		if !recognized {
			return nil
		}
		item.Type = codexRolloutFunctionCallType
		item.Name = codexRolloutWaitCallName
		item.Arguments = "{}"
		if yieldMS != nil {
			item.Arguments = `{"yield-time_ms":` + strconv.FormatUint(*yieldMS, 10) + `}`
		}
	case codexRolloutCustomToolCallOutputType:
		// Pairing stays on the outer call_id. Outputs for unrelated custom calls
		// are harmless because analysisWaitCallEntries only joins known waits.
		item.Type = codexRolloutFunctionCallOutputType
	}
	return nil
}

func analysisCanonicalCustomWriteStdinWait(input string) (*uint64, bool) {
	code, ok := analysisStripExecPragma(input)
	if !ok {
		return nil, false
	}
	parser := analysisWaitJSParser{source: strings.TrimSpace(code)}
	parser.skipSpace()

	assigned := ""
	if parser.consumeWord("const") {
		parser.skipSpace()
		assigned = parser.identifier()
		if assigned == "" {
			return nil, false
		}
		parser.skipSpace()
		if !parser.consumeByte('=') {
			return nil, false
		}
		parser.skipSpace()
	}
	if !parser.consumeWord("await") {
		return nil, false
	}
	parser.skipSpace()
	if !parser.consumeWord("tools") {
		return nil, false
	}
	parser.skipSpace()
	if !parser.consumeByte('.') {
		return nil, false
	}
	parser.skipSpace()
	if !parser.consumeWord("write_stdin") {
		return nil, false
	}
	parser.skipSpace()
	if !parser.consumeByte('(') {
		return nil, false
	}
	parser.skipSpace()
	object, ok := parser.objectLiteral()
	if !ok {
		return nil, false
	}
	parser.skipSpace()
	if !parser.consumeByte(')') {
		return nil, false
	}

	tail := analysisCompactJSWhitespace(parser.remaining())
	if assigned == "" {
		if tail != "" && tail != ";" {
			return nil, false
		}
	} else {
		want := ";text(" + assigned + ".output);"
		if tail != want && tail != strings.TrimSuffix(want, ";") {
			return nil, false
		}
	}
	return analysisCanonicalWriteStdinObject(object)
}

func analysisStripExecPragma(input string) (string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "// @exec:") {
		return trimmed, true
	}
	lineEnd := strings.IndexByte(trimmed, '\n')
	if lineEnd < 0 {
		return "", false
	}
	directive := strings.TrimSpace(strings.TrimPrefix(trimmed[:lineEnd], "// @exec:"))
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(directive), &fields); err != nil {
		return "", false
	}
	for key, value := range fields {
		if key != "yield-time_ms" && key != "max_output_tokens" {
			return "", false
		}
		var number uint64
		if err := json.Unmarshal(value, &number); err != nil {
			return "", false
		}
	}
	return strings.TrimSpace(trimmed[lineEnd+1:]), true
}

func analysisCanonicalWriteStdinObject(object string) (*uint64, bool) {
	fields, ok := analysisSplitJSObjectFields(object)
	if !ok {
		return nil, false
	}
	seen := map[string]bool{}
	var yieldMS *uint64
	yieldUnknown := false
	for _, field := range fields {
		if strings.TrimSpace(field) == "" {
			continue
		}
		keyRaw, value, ok := analysisSplitJSProperty(field)
		if !ok {
			return nil, false
		}
		key, ok := analysisWaitPropertyKey(keyRaw)
		if !ok {
			return nil, false
		}
		if seen[key] {
			if key == "yield-time_ms" {
				yieldMS = nil
				yieldUnknown = true
				continue
			}
			return nil, false
		}
		seen[key] = true
		switch key {
		case "session_id":
			if _, ok := analysisUnsignedJSLiteral(value); !ok {
				return nil, false
			}
		case "chars":
			if !analysisEmptyJSString(value) {
				return nil, false
			}
		case "yield-time_ms":
			parsed, ok := analysisUnsignedJSLiteral(value)
			if !ok {
				yieldUnknown = true
				continue
			}
			yieldMS = &parsed
		case "max_output_tokens":
			if _, ok := analysisUnsignedJSLiteral(value); !ok {
				return nil, false
			}
		}
	}
	if !seen["session_id"] {
		return nil, false
	}
	if yieldUnknown {
		return nil, true
	}
	return yieldMS, true
}

func analysisWaitPropertyKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	for _, key := range []string{"session_id", "chars", "yield-time_ms", "max_output_tokens"} {
		if raw == key || raw == `"`+key+`"` || raw == `'`+key+`'` {
			return key, true
		}
	}
	return "", false
}

func analysisUnsignedJSLiteral(raw string) (uint64, bool) {
	raw = strings.TrimSpace(raw)
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

func analysisEmptyJSString(raw string) bool {
	raw = strings.TrimSpace(raw)
	return raw == `""` || raw == `''`
}

func analysisSplitJSObjectFields(object string) ([]string, bool) {
	var fields []string
	start := 0
	quote := byte(0)
	escaped := false
	for index := 0; index < len(object); index++ {
		ch := object[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ',':
			fields = append(fields, object[start:index])
			start = index + 1
		case '{', '}', '[', ']', '(', ')':
			return nil, false
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	fields = append(fields, object[start:])
	return fields, true
}

func analysisSplitJSProperty(field string) (string, string, bool) {
	quote := byte(0)
	escaped := false
	for index := 0; index < len(field); index++ {
		ch := field[index]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ':':
			return field[:index], field[index+1:], true
		}
	}
	return "", "", false
}

type analysisWaitJSParser struct {
	source string
	pos    int
}

func (p *analysisWaitJSParser) skipSpace() {
	for p.pos < len(p.source) {
		switch p.source[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *analysisWaitJSParser) consumeWord(word string) bool {
	if !strings.HasPrefix(p.source[p.pos:], word) {
		return false
	}
	end := p.pos + len(word)
	if end < len(p.source) && analysisJSIdentifierByte(p.source[end]) {
		return false
	}
	p.pos = end
	return true
}

func (p *analysisWaitJSParser) consumeByte(expected byte) bool {
	if p.pos >= len(p.source) || p.source[p.pos] != expected {
		return false
	}
	p.pos++
	return true
}

func (p *analysisWaitJSParser) identifier() string {
	start := p.pos
	if start >= len(p.source) || !analysisJSIdentifierStartByte(p.source[start]) {
		return ""
	}
	p.pos++
	for p.pos < len(p.source) && analysisJSIdentifierByte(p.source[p.pos]) {
		p.pos++
	}
	return p.source[start:p.pos]
}

func (p *analysisWaitJSParser) objectLiteral() (string, bool) {
	if !p.consumeByte('{') {
		return "", false
	}
	start := p.pos
	quote := byte(0)
	escaped := false
	for p.pos < len(p.source) {
		ch := p.source[p.pos]
		if quote != 0 {
			p.pos++
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			p.pos++
		case '}':
			object := p.source[start:p.pos]
			p.pos++
			return object, true
		case '{':
			return "", false
		default:
			p.pos++
		}
	}
	return "", false
}

func (p *analysisWaitJSParser) remaining() string {
	if p.pos >= len(p.source) {
		return ""
	}
	return p.source[p.pos:]
}

func analysisJSIdentifierStartByte(ch byte) bool {
	return ch == '_' || ch == '$' || ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z'
}

func analysisJSIdentifierByte(ch byte) bool {
	return analysisJSIdentifierStartByte(ch) || ch >= '0' && ch <= '9'
}

func analysisCompactJSWhitespace(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			builder.WriteRune(r)
		}
	}
	return builder.String()
}
