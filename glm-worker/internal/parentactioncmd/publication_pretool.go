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
	publicationShellHelpOption             = "--help"
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
		if publicationShellAssignment(word.Value) || publicationShellControlPrefix(word.Value) {
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

func publicationShellControlPrefix(value string) bool {
	switch value {
	case "!", "if", "then", "elif", "else", "while", "until", "do":
		return true
	}
	return false
}

func publicationShellAssignment(value string) bool {
	equals := strings.IndexByte(value, '=')
	if equals <= 0 {
		return false
	}
	name := strings.TrimSuffix(value[:equals], "+")
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
	case "command":
		next, unavailable := publicationCommandWrapperNext(segment, index+1)
		return next, true, unavailable
	case "exec":
		next, unavailable := publicationExecWrapperNext(segment, index+1)
		return next, true, unavailable
	case "sudo":
		next, unavailable := publicationSudoWrapperNext(segment, index+1)
		return next, true, unavailable
	case "env":
		next, unavailable := publicationEnvWrapperNext(segment, index+1)
		return next, true, unavailable
	case "time":
		next, unavailable := publicationTimeWrapperNext(segment, index+1)
		return next, true, unavailable
	case "coproc":
		return index, true, true
	}
	return index, false, false
}

func publicationTimeWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	for index < len(segment) {
		word := segment[index]
		if word.Dynamic {
			return index, true
		}
		if word.Value == "-p" {
			index++
			continue
		}
		if strings.HasPrefix(word.Value, "-") {
			return index, true
		}
		return index, false
	}
	return index, false
}

func publicationCommandWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	query := false
	for index < len(segment) {
		next, stepQuery, done, unavailable := publicationCommandWrapperStep(segment, index)
		if unavailable {
			return index, true
		}
		query = query || stepQuery
		index = next
		if done {
			break
		}
	}
	if query {
		return len(segment), false
	}
	return index, false
}

func publicationCommandWrapperStep(segment []publicationShellWord, index int) (int, bool, bool, bool) {
	word := segment[index]
	if word.Dynamic {
		return index, false, false, true
	}
	if word.Value == "--" {
		return index + 1, false, true, false
	}
	if !strings.HasPrefix(word.Value, "-") || word.Value == "-" {
		return index, false, true, false
	}
	if word.Value == publicationShellHelpOption {
		return len(segment), true, true, false
	}
	if strings.HasPrefix(word.Value, "--") {
		return index, false, false, true
	}
	query, ok := publicationCommandShortOptions(word.Value)
	if !ok {
		return index, false, false, true
	}
	return index + 1, query, false, false
}

func publicationCommandShortOptions(value string) (bool, bool) {
	query := false
	for _, option := range value[1:] {
		switch option {
		case 'p':
		case 'v', 'V':
			query = true
		default:
			return false, false
		}
	}
	return query, true
}

func publicationExecWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	for index < len(segment) {
		word := segment[index]
		if word.Dynamic {
			return index, true
		}
		if word.Value == "--" {
			return index + 1, false
		}
		if word.Value == "-a" {
			return publicationWrapperValueNext(segment, index)
		}
		if !strings.HasPrefix(word.Value, "-") || word.Value == "-" {
			return index, false
		}
		if strings.HasPrefix(word.Value, "--") || !publicationShortOptionsOnly(word.Value, "cl") {
			return index, true
		}
		index++
	}
	return index, false
}

func publicationSudoWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	for index < len(segment) {
		next, done, unavailable := publicationSudoWrapperStep(segment, index)
		if unavailable {
			return index, true
		}
		index = next
		if done {
			return index, false
		}
	}
	return index, false
}

func publicationSudoWrapperStep(segment []publicationShellWord, index int) (int, bool, bool) {
	word := segment[index]
	if word.Dynamic {
		return index, false, true
	}
	if word.Value == "--" {
		return index + 1, true, false
	}
	if !strings.HasPrefix(word.Value, "-") || word.Value == "-" {
		return index, true, false
	}
	if publicationSudoNonExecutingOption(word.Value) {
		return len(segment), true, false
	}
	if publicationSudoFlag(word.Value) || publicationSudoInlineValueOption(word.Value) {
		return index + 1, false, false
	}
	if publicationSudoValueOption(word.Value) {
		next, unavailable := publicationWrapperValueNext(segment, index)
		return next, false, unavailable
	}
	return index, false, true
}

func publicationSudoNonExecutingOption(value string) bool {
	switch value {
	case "-e", "--edit", "-l", "--list", "-v", "--validate", "-V", "--version", "-h", publicationShellHelpOption:
		return true
	}
	return false
}

func publicationSudoFlag(value string) bool {
	switch value {
	case "-A", "--askpass", "-b", "--background", "-E", "--preserve-env", "-H", "--set-home", "-K", "--remove-timestamp", "-k", "--reset-timestamp", "-n", "--non-interactive", "-P", "--preserve-groups", "-S", "--stdin":
		return true
	}
	return false
}

func publicationSudoInlineValueOption(value string) bool {
	prefixes := []string{"--user=", "--group=", "--host=", "--prompt=", "--close-from=", "--command-timeout=", "--chdir=", "--chroot=", "--role=", "--type="}
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return strings.HasPrefix(value, "--preserve-env=")
}

func publicationSudoValueOption(value string) bool {
	switch value {
	case "-u", "--user", "-g", "--group", "--host", "-p", "--prompt", "-C", "--close-from", "-T", "--command-timeout", "-D", "--chdir", "-R", "--chroot", "-r", "--role", "-t", "--type":
		return true
	}
	return false
}

func publicationEnvWrapperNext(segment []publicationShellWord, index int) (int, bool) {
	for index < len(segment) {
		next, done, unavailable := publicationEnvWrapperStep(segment, index)
		if unavailable {
			return index, true
		}
		index = next
		if done {
			return index, false
		}
	}
	return index, false
}

func publicationEnvWrapperStep(segment []publicationShellWord, index int) (int, bool, bool) {
	word := segment[index]
	if word.Dynamic {
		return index, false, true
	}
	if publicationShellAssignment(word.Value) {
		return index + 1, false, false
	}
	if word.Value == "--" {
		return index + 1, true, false
	}
	if !strings.HasPrefix(word.Value, "-") || word.Value == "-" {
		return index, true, false
	}
	if publicationEnvNonExecutingOption(word.Value) {
		return len(segment), true, false
	}
	if publicationEnvFlag(word.Value) || publicationEnvInlineValueOption(word.Value) {
		return index + 1, false, false
	}
	if publicationEnvValueOption(word.Value) {
		next, unavailable := publicationWrapperValueNext(segment, index)
		return next, false, unavailable
	}
	return index, false, true
}

func publicationEnvNonExecutingOption(value string) bool {
	return value == publicationShellHelpOption || value == "--version"
}

func publicationEnvFlag(value string) bool {
	switch value {
	case "-i", "--ignore-environment", "-0", "--null", "-v", "--debug":
		return true
	}
	return false
}

func publicationEnvInlineValueOption(value string) bool {
	return strings.HasPrefix(value, "--unset=") || strings.HasPrefix(value, "--chdir=") || len(value) > 2 && strings.HasPrefix(value, "-u")
}

func publicationEnvValueOption(value string) bool {
	switch value {
	case "-u", "--unset", "-C", "--chdir":
		return true
	}
	return false
}

func publicationWrapperValueNext(segment []publicationShellWord, index int) (int, bool) {
	if index+1 >= len(segment) || segment[index+1].Dynamic {
		return index, true
	}
	return index + 2, false
}

func publicationShortOptionsOnly(value, allowed string) bool {
	if len(value) < 2 || value[0] != '-' || value[1] == '-' {
		return false
	}
	for _, option := range value[1:] {
		if !strings.ContainsRune(allowed, option) {
			return false
		}
	}
	return true
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
	case '(':
		lexer.consumeOpenParen()
	case ')':
		lexer.flushSegment()
	case '{', '}':
		lexer.consumeBrace(ch)
	case '\'', '"':
		lexer.quote = ch
		lexer.wordStarted = true
	case '\\':
		return lexer.writeEscaped()
	case '`', '$', '*', '?', '<', '>':
		return lexer.scanUnquotedDynamic(ch)
	case '#':
		lexer.consumeCommentOrLiteral(ch)
	default:
		lexer.wordStarted = true
		lexer.value.WriteByte(ch)
	}
	return nil
}

func (lexer *publicationShellLexer) scanUnquotedDynamic(ch byte) error {
	switch ch {
	case '`':
		return lexer.consumeBacktickDynamic()
	case '$':
		return lexer.writeDynamic(ch)
	case '*', '?':
		lexer.writeDynamicByte(ch)
	case '<', '>':
		return lexer.writeRedirectionOrProcessSubstitution(ch)
	}
	return nil
}

func (lexer *publicationShellLexer) consumeOpenParen() {
	if lexer.wordStarted {
		lexer.value.WriteByte('(')
		return
	}
	lexer.flushSegment()
}

func (lexer *publicationShellLexer) consumeBrace(ch byte) {
	if !lexer.wordStarted && lexer.shellOperatorBoundaryAhead() {
		lexer.flushSegment()
		return
	}
	lexer.writeDynamicByte(ch)
}

func (lexer *publicationShellLexer) shellOperatorBoundaryAhead() bool {
	if lexer.index+1 >= len(lexer.command) {
		return true
	}
	switch lexer.command[lexer.index+1] {
	case ' ', '\t', '\r', '\n', ';', '|', '&', '(', ')':
		return true
	}
	return false
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
			index, quote = lexer.consumeParenthesizedQuoted(index, quote, ch)
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

func (lexer *publicationShellLexer) consumeParenthesizedQuoted(index int, quote, ch byte) (int, byte) {
	if ch == quote {
		return index, 0
	}
	if ch == '\\' && quote == '"' && index+1 < len(lexer.command) {
		index++
		lexer.value.WriteByte(lexer.command[index])
	}
	return index, quote
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
