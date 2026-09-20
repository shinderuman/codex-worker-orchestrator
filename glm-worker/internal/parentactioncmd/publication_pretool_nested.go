package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type publicationExecutableSubstitutionScanner struct {
	command string
	index   int
	quote   byte
	bodies  []string
}

func runPublicationPreToolUseChecked(payload string, stdout io.Writer) error {
	var input publicationPreToolUseInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return fmt.Errorf("decode publication PreToolUse input: %w", err)
	}
	if input.ToolName != "Bash" {
		return nil
	}
	code, reason := publicationPreToolUseCheckedDecision(input.ToolInput.Command, 0)
	if reason != "" {
		return json.NewEncoder(stdout).Encode(publicationPreToolUseOutput{Decision: "block", Code: code, Reason: reason})
	}
	return runPublicationPreToolUse(payload, stdout)
}

func publicationPreToolUseCheckedDecision(command string, depth int) (string, string) {
	if depth >= publicationShellClassificationMaxDepth {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	normalized := publicationNormalizeHereDocDash(command)
	if code, reason := publicationPreToolUseBlockDecision(normalized); reason != "" {
		return code, reason
	}
	if code, reason := publicationClassifyBuiltinShell(normalized, depth); reason != "" {
		return code, reason
	}
	bodies, err := publicationExecutableSubstitutions(command)
	if err != nil {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	for _, body := range bodies {
		if code, reason := publicationPreToolUseCheckedDecision(body, depth+1); reason != "" {
			return code, reason
		}
	}
	return "", ""
}

func publicationClassifyBuiltinShell(command string, depth int) (string, string) {
	segments, err := publicationShellSegments(command)
	if err != nil {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	for _, segment := range segments {
		if code, reason := publicationClassifyBuiltinSegment(segment, depth); reason != "" {
			return code, reason
		}
	}
	return "", ""
}

func publicationClassifyBuiltinSegment(segment []publicationShellWord, depth int) (string, string) {
	commandIndex, ok, unavailable := publicationShellCommandIndex(segment)
	if unavailable {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	if !ok || segment[commandIndex].Dynamic || segment[commandIndex].Value != "builtin" {
		return "", ""
	}
	args := segment[commandIndex+1:]
	for len(args) > 0 && args[0].Value == "builtin" && !args[0].Dynamic {
		args = args[1:]
	}
	if len(args) == 0 {
		return "", ""
	}
	if args[0].Dynamic {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	switch args[0].Value {
	case "command", "eval", "exec":
		return publicationClassifySegment(args, depth+1)
	default:
		return "", ""
	}
}

func publicationNormalizeHereDocDash(command string) string {
	var output strings.Builder
	output.Grow(len(command))
	quote := byte(0)
	for index := 0; index < len(command); index++ {
		next, nextQuote := publicationNormalizeHereDocDashStep(command, index, quote, &output)
		index = next
		quote = nextQuote
	}
	return output.String()
}

func publicationNormalizeHereDocDashStep(command string, index int, quote byte, output *strings.Builder) (int, byte) {
	ch := command[index]
	if quote != 0 {
		return publicationNormalizeQuoted(command, index, quote, output)
	}
	switch ch {
	case '\'', '"':
		output.WriteByte(ch)
		return index, ch
	case '\\':
		output.WriteByte(ch)
		if index+1 < len(command) {
			index++
			output.WriteByte(command[index])
		}
		return index, 0
	case '<':
		if index+2 < len(command) && command[index+1] == '<' && command[index+2] == '-' {
			output.WriteString(" << ")
			return index + 2, 0
		}
	}
	output.WriteByte(ch)
	return index, 0
}

func publicationNormalizeQuoted(command string, index int, quote byte, output *strings.Builder) (int, byte) {
	ch := command[index]
	output.WriteByte(ch)
	if ch == quote {
		return index, 0
	}
	if quote == '"' && ch == '\\' && index+1 < len(command) {
		index++
		output.WriteByte(command[index])
	}
	return index, quote
}

func publicationExecutableSubstitutions(command string) ([]string, error) {
	scanner := publicationExecutableSubstitutionScanner{command: command, bodies: make([]string, 0, 2)}
	if err := scanner.scan(); err != nil {
		return nil, err
	}
	return scanner.bodies, nil
}

func (scanner *publicationExecutableSubstitutionScanner) scan() error {
	for scanner.index < len(scanner.command) {
		if err := scanner.scanCurrent(); err != nil {
			return err
		}
	}
	return nil
}

func (scanner *publicationExecutableSubstitutionScanner) scanCurrent() error {
	switch scanner.quote {
	case '\'':
		scanner.scanSingleQuoted()
		return nil
	case '"':
		return scanner.scanDoubleQuoted()
	default:
		return scanner.scanUnquoted()
	}
}

func (scanner *publicationExecutableSubstitutionScanner) scanSingleQuoted() {
	if scanner.command[scanner.index] == '\'' {
		scanner.quote = 0
	}
	scanner.index++
}

func (scanner *publicationExecutableSubstitutionScanner) scanDoubleQuoted() error {
	ch := scanner.command[scanner.index]
	switch ch {
	case '"':
		scanner.quote = 0
		scanner.index++
		return nil
	case '\\':
		scanner.skipEscaped()
		return nil
	case '`':
		return scanner.captureBacktick()
	case '$':
		return scanner.captureDollar()
	default:
		scanner.index++
		return nil
	}
}

func (scanner *publicationExecutableSubstitutionScanner) scanUnquoted() error {
	ch := scanner.command[scanner.index]
	switch ch {
	case '\'', '"':
		scanner.quote = ch
		scanner.index++
		return nil
	case '\\':
		scanner.skipEscaped()
		return nil
	case '`':
		return scanner.captureBacktick()
	case '$':
		return scanner.captureDollar()
	case '<', '>':
		return scanner.captureProcessSubstitution()
	default:
		scanner.index++
		return nil
	}
}

func (scanner *publicationExecutableSubstitutionScanner) skipEscaped() {
	scanner.index++
	if scanner.index < len(scanner.command) {
		scanner.index++
	}
}

func (scanner *publicationExecutableSubstitutionScanner) captureDollar() error {
	if scanner.index+1 >= len(scanner.command) || scanner.command[scanner.index+1] != '(' {
		scanner.index++
		return nil
	}
	if scanner.index+2 < len(scanner.command) && scanner.command[scanner.index+2] == '(' {
		scanner.index += 3
		return nil
	}
	return scanner.captureParenthesized(scanner.index + 1)
}

func (scanner *publicationExecutableSubstitutionScanner) captureProcessSubstitution() error {
	if scanner.index+1 >= len(scanner.command) || scanner.command[scanner.index+1] != '(' {
		scanner.index++
		return nil
	}
	return scanner.captureParenthesized(scanner.index + 1)
}

func (scanner *publicationExecutableSubstitutionScanner) captureParenthesized(openingParen int) error {
	body, end, err := publicationBalancedBody(scanner.command, openingParen)
	if err != nil {
		return err
	}
	scanner.bodies = append(scanner.bodies, body)
	scanner.index = end + 1
	return nil
}

func (scanner *publicationExecutableSubstitutionScanner) captureBacktick() error {
	body, end, err := publicationBacktickBody(scanner.command, scanner.index)
	if err != nil {
		return err
	}
	scanner.bodies = append(scanner.bodies, body)
	scanner.index = end + 1
	return nil
}

func publicationBalancedBody(command string, openingParen int) (string, int, error) {
	start := openingParen + 1
	depth := 1
	quote := byte(0)
	for index := start; index < len(command); index++ {
		next, nextQuote, depthDelta, closed := publicationBalancedBodyStep(command, index, quote)
		index = next
		quote = nextQuote
		depth += depthDelta
		if closed && depth == 0 {
			return command[start:index], index, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated executable substitution")
}

func publicationBalancedBodyStep(command string, index int, quote byte) (int, byte, int, bool) {
	if quote != 0 {
		return publicationBalancedQuotedBodyStep(command, index, quote)
	}
	return publicationBalancedUnquotedBodyStep(command, index)
}

func publicationBalancedQuotedBodyStep(command string, index int, quote byte) (int, byte, int, bool) {
	ch := command[index]
	if ch == quote {
		return index, 0, 0, false
	}
	if quote == '"' && ch == '\\' && index+1 < len(command) {
		return index + 1, quote, 0, false
	}
	return index, quote, 0, false
}

func publicationBalancedUnquotedBodyStep(command string, index int) (int, byte, int, bool) {
	switch command[index] {
	case '\'', '"':
		return index, command[index], 0, false
	case '\\':
		if index+1 < len(command) {
			return index + 1, 0, 0, false
		}
	case '(':
		return index, 0, 1, false
	case ')':
		return index, 0, -1, true
	case '`':
		_, end, err := publicationBacktickBody(command, index)
		if err == nil {
			return end, 0, 0, false
		}
	}
	return index, 0, 0, false
}

func publicationBacktickBody(command string, marker int) (string, int, error) {
	start := marker + 1
	for index := start; index < len(command); index++ {
		if command[index] == '\\' {
			index++
			continue
		}
		if command[index] == '`' {
			return command[start:index], index, nil
		}
	}
	return "", 0, fmt.Errorf("unterminated backtick substitution")
}
