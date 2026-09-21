package app

import (
	"encoding/json"
	"strconv"
	"strings"
)

const (
	analysisWaitSessionIDKey       = "session_id"
	analysisWaitCharsKey           = "chars"
	analysisWaitYieldMSKey         = "yield_time_ms"
	analysisWaitMaxOutputTokensKey = "max_output_tokens"
)

type analysisRolloutItemPayloadRaw struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Input     string `json:"input"`
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
		item.Arguments = `{"` + analysisWaitYieldMSKey + `":` + strconv.FormatUint(*yieldMS, 10) + `}`
	}
}

func analysisCanonicalCustomWriteStdinWait(input string) (*uint64, bool) {
	code, ok := analysisStripExecPragma(input)
	if !ok {
		return nil, false
	}
	parser := analysisWaitJSParser{source: strings.TrimSpace(code)}
	parser.skipSpace()

	assigned, ok := analysisWaitAssignment(&parser)
	if !ok {
		return nil, false
	}
	object, ok := analysisWaitInvocationObject(&parser)
	if !ok || !analysisWaitTailMatches(parser.remaining(), assigned) {
		return nil, false
	}
	return analysisCanonicalWriteStdinObject(object)
}

func analysisWaitAssignment(parser *analysisWaitJSParser) (string, bool) {
	if !parser.consumeWord("const") {
		return "", true
	}
	parser.skipSpace()
	assigned := parser.identifier()
	if assigned == "" {
		return "", false
	}
	parser.skipSpace()
	if !parser.consumeByte('=') {
		return "", false
	}
	parser.skipSpace()
	return assigned, true
}

func analysisWaitInvocationObject(parser *analysisWaitJSParser) (string, bool) {
	if !analysisConsumeWaitInvocationPrefix(parser) {
		return "", false
	}
	object, ok := parser.objectLiteral()
	if !ok {
		return "", false
	}
	parser.skipSpace()
	if !parser.consumeByte(')') {
		return "", false
	}
	return object, true
}

func analysisConsumeWaitInvocationPrefix(parser *analysisWaitJSParser) bool {
	if !parser.consumeWord("await") {
		return false
	}
	parser.skipSpace()
	if !parser.consumeWord("tools") {
		return false
	}
	parser.skipSpace()
	if !parser.consumeByte('.') {
		return false
	}
	parser.skipSpace()
	if !parser.consumeWord("write_stdin") {
		return false
	}
	parser.skipSpace()
	if !parser.consumeByte('(') {
		return false
	}
	parser.skipSpace()
	return true
}

func analysisWaitTailMatches(raw, assigned string) bool {
	tail := analysisCompactJSWhitespace(raw)
	if assigned == "" {
		return tail == "" || tail == ";"
	}
	want := ";text(" + assigned + ".output);"
	return tail == want || tail == strings.TrimSuffix(want, ";")
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
		if key != analysisWaitYieldMSKey && key != analysisWaitMaxOutputTokensKey {
			return "", false
		}
		var number uint64
		if err := json.Unmarshal(value, &number); err != nil {
			return "", false
		}
	}
	return strings.TrimSpace(trimmed[lineEnd+1:]), true
}

type analysisWriteStdinFields struct {
	seen         map[string]bool
	yieldMS      *uint64
	yieldUnknown bool
}

func analysisCanonicalWriteStdinObject(object string) (*uint64, bool) {
	fields, ok := analysisSplitJSObjectFields(object)
	if !ok {
		return nil, false
	}
	state := analysisWriteStdinFields{seen: map[string]bool{}}
	for _, field := range fields {
		if strings.TrimSpace(field) != "" && !state.apply(field) {
			return nil, false
		}
	}
	if !state.seen[analysisWaitSessionIDKey] {
		return nil, false
	}
	if state.yieldUnknown {
		return nil, true
	}
	return state.yieldMS, true
}

func (state *analysisWriteStdinFields) apply(field string) bool {
	keyRaw, value, ok := analysisSplitJSProperty(field)
	if !ok {
		return false
	}
	key, ok := analysisWaitPropertyKey(keyRaw)
	if !ok {
		return false
	}
	if state.seen[key] {
		return state.applyDuplicate(key)
	}
	state.seen[key] = true
	return state.applyUnique(key, value)
}

func (state *analysisWriteStdinFields) applyDuplicate(key string) bool {
	if key != analysisWaitYieldMSKey {
		return false
	}
	state.yieldMS = nil
	state.yieldUnknown = true
	return true
}

func (state *analysisWriteStdinFields) applyUnique(key, value string) bool {
	switch key {
	case analysisWaitSessionIDKey:
		_, ok := analysisUnsignedJSLiteral(value)
		return ok
	case analysisWaitCharsKey:
		return analysisEmptyJSString(value)
	case analysisWaitYieldMSKey:
		return state.applyYield(value)
	case analysisWaitMaxOutputTokensKey:
		_, ok := analysisUnsignedJSLiteral(value)
		return ok
	default:
		return false
	}
}

func (state *analysisWriteStdinFields) applyYield(value string) bool {
	parsed, ok := analysisUnsignedJSLiteral(value)
	if !ok {
		state.yieldUnknown = true
		return true
	}
	state.yieldMS = &parsed
	return true
}

func analysisWaitPropertyKey(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	for _, key := range []string{
		analysisWaitSessionIDKey,
		analysisWaitCharsKey,
		analysisWaitYieldMSKey,
		analysisWaitMaxOutputTokensKey,
	} {
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

type analysisJSQuoteState struct {
	quote   byte
	escaped bool
}

func (state *analysisJSQuoteState) consume(ch byte) bool {
	if state.quote == 0 {
		if ch == '\'' || ch == '"' {
			state.quote = ch
			return true
		}
		return false
	}
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

func (state analysisJSQuoteState) complete() bool {
	return state.quote == 0 && !state.escaped
}

func analysisSplitJSObjectFields(object string) ([]string, bool) {
	var fields []string
	start := 0
	var quote analysisJSQuoteState
	for index := 0; index < len(object); index++ {
		ch := object[index]
		if quote.consume(ch) {
			continue
		}
		switch ch {
		case ',':
			fields = append(fields, object[start:index])
			start = index + 1
		case '{', '}', '[', ']', '(', ')':
			return nil, false
		}
	}
	if !quote.complete() {
		return nil, false
	}
	fields = append(fields, object[start:])
	return fields, true
}

func analysisSplitJSProperty(field string) (string, string, bool) {
	var quote analysisJSQuoteState
	for index := 0; index < len(field); index++ {
		ch := field[index]
		if quote.consume(ch) {
			continue
		}
		if ch == ':' {
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
