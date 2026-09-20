package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
)

type publicationPreToolUseInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		Command string `json:"command"`
	} `json:"tool_input"`
}

type publicationPreToolUseOutput struct {
	Decision string `json:"decision"`
	Code     string `json:"code,omitempty"`
	Reason   string `json:"reason"`
}

type publicationShellWord struct {
	Value   string
	Dynamic bool
}

type publicationShellLexer struct {
	command     string
	segments    [][]publicationShellWord
	current     []publicationShellWord
	value       strings.Builder
	quote       byte
	wordStarted bool
	dynamic     bool
	index       int
}

const (
	publicationPreToolUseFlag              = "--pre-tool-use"
	publicationGitGuardBypassCode          = "publication_git_guard_bypass"
	publicationGitClassificationCode       = "publication_git_classification_unavailable"
	publicationGitClassificationReason     = "publication Git operation rejected: shell command cannot be statically classified"
	publicationGitNoVerifyReason           = "publication Git operation rejected: --no-verify may not bypass managed repository guards"
	publicationGitHooksPathReason          = "publication Git operation rejected: core.hooksPath may not bypass managed repository guards"
	publicationShellClassificationMaxDepth = 4
)

func runPublicationPreToolUse(payload string, stdout io.Writer) error {
	var input publicationPreToolUseInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return fmt.Errorf("decode publication PreToolUse input: %w", err)
	}
	if input.ToolName != "Bash" {
		return nil
	}
	code, reason := publicationPreToolUseBlockDecision(input.ToolInput.Command)
	if reason == "" {
		return nil
	}
	return json.NewEncoder(stdout).Encode(publicationPreToolUseOutput{Decision: "block", Code: code, Reason: reason})
}

func publicationPreToolUseBlockDecision(command string) (string, string) {
	return publicationClassifyShell(command, 0)
}

func publicationClassifyShell(command string, depth int) (string, string) {
	if depth >= publicationShellClassificationMaxDepth {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	segments, err := publicationShellSegments(command)
	if err != nil {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	for _, segment := range segments {
		if code, reason := publicationClassifySegment(segment, depth); reason != "" {
			return code, reason
		}
	}
	return "", ""
}

func publicationClassifySegment(segment []publicationShellWord, depth int) (string, string) {
	commandIndex, ok, unavailable := publicationShellCommandIndex(segment)
	if unavailable {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	if !ok {
		return "", ""
	}
	command := segment[commandIndex]
	if command.Dynamic {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	switch filepath.Base(command.Value) {
	case "sh", "bash", "zsh":
		return publicationClassifyInterpreter(segment[commandIndex+1:], depth+1)
	case "eval":
		return publicationClassifyEval(segment[commandIndex+1:], depth+1)
	case "git":
		return publicationClassifyGit(segment, commandIndex)
	default:
		return "", ""
	}
}

func publicationShellCommandIndex(segment []publicationShellWord) (int, bool, bool) {
	for index := 0; index < len(segment); {
		word := segment[index]
		if word.Dynamic {
			return index, true, false
		}
		if publicationShellAssignment(word.Value) || word.Value == "!" {
			index++
			continue
		}
		next, wrapper, unavailable := publicationShellWrapperNext(segment, index)
		if unavailable {
			return 0, false, true
		}
		if wrapper {
			index = next
			continue
		}
		return index, true, false
	}
	return 0, false, false
}

func publicationShellAssignment(value string) bool {
	equals := strings.IndexByte(value, '=')
	if equals <= 0 {
		return false
	}
	name := value[:equals]
	if strings.HasSuffix(name, "+") {
		name = strings.TrimSuffix(name, "+")
	}
	if bracket := strings.IndexByte(name, '['); bracket > 0 && strings.HasSuffix(name, "]") {
		name = name[:bracket]
	}
	if name == "" || !publicationShellNameStart(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		if !publicationShellNamePart(name[index]) {
			return false
		}
	}
	return true
}

func publicationShellNameStart(value byte) bool {
	return value == '_' || value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func publicationShellNamePart(value byte) bool {
	return publicationShellNameStart(value) || value >= '0' && value <= '9'
}

func publicationShellWrapperNext(segment []publicationShellWord, index int) (int, bool, bool) {
	switch segment[index].Value {
	case "command", "exec", "sudo":
		next, unavailable := publicationSimpleWrapperNext(segment, index+1)
		return next, true, unavailable
	case "env":
		next, unavailable := publicationEnvWrapperNext(segment, index+1)
		return next, true, unavailable
	}
	return index, false, false
}

func publicationSimpleWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	if index >= len(segment) {
		return index, false
	}
	if segment[index].Dynamic {
		return index, true
	}
	if segment[index].Value == "--" {
		index++
	}
	if index < len(segment) && strings.HasPrefix(segment[index].Value, "-") {
		return index, true
	}
	return index, false
}

func publicationEnvWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	for index < len(segment) {
		word := segment[index]
		if word.Dynamic {
			return index, true
		}
		if publicationShellAssignment(word.Value) {
			index++
			continue
		}
		if word.Value == "--" {
			return index + 1, false
		}
		if strings.HasPrefix(word.Value, "-") {
			return index, true
		}
		return index, false
	}
	return index, false
}

func publicationClassifyInterpreter(args []publicationShellWord, depth int) (string, string) {
	payload, dynamic, ok := publicationInterpreterCommandPayload(args)
	if dynamic {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	if !ok {
		return "", ""
	}
	return publicationClassifyShell(payload, depth)
}

func publicationInterpreterCommandPayload(args []publicationShellWord) (string, bool, bool) {
	for index := 0; index < len(args); index++ {
		word := args[index]
		if word.Dynamic {
			return "", true, false
		}
		if word.Value == "--" || !strings.HasPrefix(word.Value, "-") {
			return "", false, false
		}
		if publicationInterpreterInlineValueOption(word.Value) {
			continue
		}
		if publicationInterpreterCommandOption(word.Value) {
			return publicationInterpreterPayloadAfterOption(args, index)
		}
		if publicationInterpreterOptionConsumesValue(word.Value) {
			next, dynamic := publicationInterpreterConsumeValue(args, index)
			if dynamic {
				return "", true, false
			}
			index = next
		}
	}
	return "", false, false
}

func publicationInterpreterPayloadAfterOption(args []publicationShellWord, index int) (string, bool, bool) {
	if index+1 >= len(args) || args[index+1].Dynamic {
		return "", true, false
	}
	return args[index+1].Value, false, true
}

func publicationInterpreterConsumeValue(args []publicationShellWord, index int) (int, bool) {
	if index+1 >= len(args) || args[index+1].Dynamic {
		return index, true
	}
	return index + 1, false
}

func publicationInterpreterInlineValueOption(value string) bool {
	return len(value) > 2 && (strings.HasPrefix(value, "-O") || strings.HasPrefix(value, "-o"))
}

func publicationInterpreterCommandOption(value string) bool {
	return len(value) > 1 && value[0] == '-' && value[1] != '-' && strings.Contains(value[1:], "c")
}

func publicationInterpreterOptionConsumesValue(value string) bool {
	switch value {
	case "--init-file", "--rcfile", "-O", "-o":
		return true
	}
	return false
}

func publicationClassifyEval(args []publicationShellWord, depth int) (string, string) {
	if len(args) == 0 {
		return "", ""
	}
	parts := make([]string, 0, len(args))
	for _, word := range args {
		if word.Dynamic {
			return publicationGitClassificationCode, publicationGitClassificationReason
		}
		parts = append(parts, word.Value)
	}
	return publicationClassifyShell(strings.Join(parts, " "), depth)
}

func publicationClassifyGit(segment []publicationShellWord, commandIndex int) (string, string) {
	for _, word := range segment {
		if word.Dynamic {
			return publicationGitClassificationCode, publicationGitClassificationReason
		}
	}
	if publicationGitNoVerify(segment[commandIndex+1:]) {
		return publicationGitGuardBypassCode, publicationGitNoVerifyReason
	}
	if publicationGitHooksPathBypass(segment) {
		return publicationGitGuardBypassCode, publicationGitHooksPathReason
	}
	return "", ""
}

func publicationShellSegments(command string) ([][]publicationShellWord, error) {
	lexer := publicationShellLexer{command: command, segments: make([][]publicationShellWord, 0, 1)}
	if err := lexer.scan(); err != nil {
		return nil, err
	}
	lexer.flushSegment()
	return lexer.segments, nil
}

func (lexer *publicationShellLexer) scan() error {
	for lexer.index < len(lexer.command) {
		ch := lexer.command[lexer.index]
		var err error
		if lexer.quote == 0 {
			err = lexer.scanUnquoted(ch)
		} else {
			err = lexer.scanQuoted(ch)
		}
		if err != nil {
			return err
		}
		lexer.index++
	}
	if lexer.quote != 0 {
		return fmt.Errorf("unterminated quote")
	}
	return nil
}

func (lexer *publicationShellLexer) scanQuoted(ch byte) error {
	if lexer.quote == '\'' {
		return lexer.scanSingleQuoted(ch)
	}
	return lexer.scanDoubleQuoted(ch)
}

func (lexer *publicationShellLexer) scanSingleQuoted(ch byte) error {
	if ch == '\'' {
		lexer.quote = 0
		return nil
	}
	lexer.value.WriteByte(ch)
	return nil
}

func (lexer *publicationShellLexer) scanDoubleQuoted(ch byte) error {
	switch ch {
	case '"':
		lexer.quote = 0
	case '`':
		return lexer.consumeBacktickDynamic()
	case '$':
		return lexer.writeDynamic(ch)
	case '\\':
		return lexer.writeEscaped()
	default:
		lexer.value.WriteByte(ch)
	}
	return nil
}

func (lexer *publicationShellLexer) scanUnquoted(ch byte) error {
	switch ch {
	case ' ', '\t', '\r':
		lexer.flushWord()
	case '\n', ';', '|', '&':
		lexer.consumeSeparator(ch)
	case '\'', '"':
		lexer.quote = ch
		lexer.wordStarted = true
	case '\\':
		return lexer.writeEscaped()
	case '`':
		return lexer.consumeBacktickDynamic()
	case '$':
		return lexer.writeDynamic(ch)
	case '*', '?':
		lexer.writeDynamicByte(ch)
	case '<', '>':
		return lexer.writeRedirectionOrProcessSubstitution(ch)
	case '#':
		lexer.consumeCommentOrLiteral(ch)
	default:
		lexer.wordStarted = true
		lexer.value.WriteByte(ch)
	}
	return nil
}

func (lexer *publicationShellLexer) writeEscaped() error {
	if lexer.index+1 >= len(lexer.command) {
		return fmt.Errorf("unterminated escape")
	}
	lexer.index++
	lexer.wordStarted = true
	lexer.value.WriteByte(lexer.command[lexer.index])
	return nil
}

func (lexer *publicationShellLexer) writeDynamic(ch byte) error {
	lexer.writeDynamicByte(ch)
	if lexer.index+1 < len(lexer.command) && lexer.command[lexer.index+1] == '(' {
		return lexer.consumeParenthesizedDynamic()
	}
	return nil
}

func (lexer *publicationShellLexer) writeDynamicByte(ch byte) {
	lexer.dynamic = true
	lexer.wordStarted = true
	lexer.value.WriteByte(ch)
}

func (lexer *publicationShellLexer) writeRedirectionOrProcessSubstitution(ch byte) error {
	lexer.wordStarted = true
	lexer.value.WriteByte(ch)
	if lexer.index+1 < len(lexer.command) && lexer.command[lexer.index+1] == '(' {
		lexer.dynamic = true
		return lexer.consumeParenthesizedDynamic()
	}
	return nil
}

func (lexer *publicationShellLexer) consumeParenthesizedDynamic() error {
	depth := 0
	var quote byte
	for index := lexer.index + 1; index < len(lexer.command); index++ {
		ch := lexer.command[index]
		lexer.value.WriteByte(ch)
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else if ch == '\\' && quote == '"' && index+1 < len(lexer.command) {
				index++
				lexer.value.WriteByte(lexer.command[index])
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case '\\':
			if index+1 < len(lexer.command) {
				index++
				lexer.value.WriteByte(lexer.command[index])
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				lexer.index = index
				return nil
			}
		}
	}
	return fmt.Errorf("unterminated dynamic substitution")
}

func (lexer *publicationShellLexer) consumeBacktickDynamic() error {
	lexer.writeDynamicByte('`')
	for index := lexer.index + 1; index < len(lexer.command); index++ {
		ch := lexer.command[index]
		lexer.value.WriteByte(ch)
		if ch == '\\' && index+1 < len(lexer.command) {
			index++
			lexer.value.WriteByte(lexer.command[index])
			continue
		}
		if ch == '`' {
			lexer.index = index
			return nil
		}
	}
	return fmt.Errorf("unterminated backtick substitution")
}

func (lexer *publicationShellLexer) consumeSeparator(ch byte) {
	lexer.flushSegment()
	if (ch == '|' || ch == '&') && lexer.index+1 < len(lexer.command) && lexer.command[lexer.index+1] == ch {
		lexer.index++
	}
}

func (lexer *publicationShellLexer) consumeCommentOrLiteral(ch byte) {
	if lexer.wordStarted {
		lexer.value.WriteByte(ch)
		return
	}
	lexer.flushSegment()
	for lexer.index+1 < len(lexer.command) && lexer.command[lexer.index+1] != '\n' {
		lexer.index++
	}
}

func (lexer *publicationShellLexer) flushWord() {
	if !lexer.wordStarted {
		return
	}
	lexer.current = append(lexer.current, publicationShellWord{Value: lexer.value.String(), Dynamic: lexer.dynamic})
	lexer.value.Reset()
	lexer.wordStarted = false
	lexer.dynamic = false
}

func (lexer *publicationShellLexer) flushSegment() {
	lexer.flushWord()
	if len(lexer.current) == 0 {
		return
	}
	lexer.segments = append(lexer.segments, lexer.current)
	lexer.current = nil
}

func publicationGitNoVerify(argv []publicationShellWord) bool {
	for _, token := range argv {
		if token.Value == "--no-verify" {
			return true
		}
	}
	return false
}

func publicationGitHooksPathBypass(segment []publicationShellWord) bool {
	for _, token := range segment {
		if strings.Contains(strings.ToLower(token.Value), "core.hookspath") {
			return true
		}
	}
	return false
}
