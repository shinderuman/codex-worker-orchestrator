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
	if reason == "" {
		return nil
	}
	return json.NewEncoder(stdout).Encode(publicationPreToolUseOutput{Decision: "block", Code: code, Reason: reason})
}

func publicationPreToolUseCheckedDecision(command string, depth int) (string, string) {
	if depth >= publicationShellClassificationMaxDepth {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	normalized := publicationNormalizeHereDocDash(command)
	if code, reason := publicationClassifyShell(normalized, depth); reason != "" {
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
	var quote byte
	for index := 0; index < len(command); index++ {
		ch := command[index]
		if quote != 0 {
			output.WriteByte(ch)
			if ch == quote {
				quote = 0
				continue
			}
			if quote == '"' && ch == '\\' && index+1 < len(command) {
				index++
				output.WriteByte(command[index])
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			output.WriteByte(ch)
		case '\\':
			output.WriteByte(ch)
			if index+1 < len(command) {
				index++
				output.WriteByte(command[index])
			}
		case '<':
			if index+2 < len(command) && command[index+1] == '<' && command[index+2] == '-' {
				output.WriteString(" << ")
				index += 2
				continue
			}
			output.WriteByte(ch)
		default:
			output.WriteByte(ch)
		}
	}
	return output.String()
}

func publicationExecutableSubstitutions(command string) ([]string, error) {
	scanner := publicationExecutableSubstitutionScanner{command: command}
	return scanner.scan()
}

func (scanner *publicationExecutableSubstitutionScanner) scan() ([]string, error) {
	bodies := make([]string, 0, 2)
	for scanner.index < len(scanner.command) {
		ch := scanner.command[scanner.index]
		if scanner.quote == '\'' {
			if ch == '\'' {
				scanner.quote = 0
			}
			scanner.index++
			continue
		}
		if ch == '\\' {
			scanner.index += 2
			continue
		}
		if ch == '\'' && scanner.quote == 0 {
			scanner.quote = ch
			scanner.index++
			continue
		}
		if ch == '"' {
			if scanner.quote == '"' {
				scanner.quote = 0
			} else if scanner.quote == 0 {
				scanner.quote = ch
			}
			scanner.index++
			continue
		}
		if ch == '`' {
			body, end, err := publicationBacktickBody(scanner.command, scanner.index)
			if err != nil {
				return nil, err
			}
			bodies = append(bodies, body)
			scanner.index = end + 1
			continue
		}
		if ch == '$' && scanner.index+1 < len(scanner.command) && scanner.command[scanner.index+1] == '(' {
			if scanner.index+2 < len(scanner.command) && scanner.command[scanner.index+2] == '(' {
				scanner.index += 3
				continue
			}
			body, end, err := publicationBalancedBody(scanner.command, scanner.index+1)
			if err != nil {
				return nil, err
			}
			bodies = append(bodies, body)
			scanner.index = end + 1
			continue
		}
		if scanner.quote == 0 && (ch == '<' || ch == '>') && scanner.index+1 < len(scanner.command) && scanner.command[scanner.index+1] == '(' {
			body, end, err := publicationBalancedBody(scanner.command, scanner.index)
			if err != nil {
				return nil, err
			}
			bodies = append(bodies, body)
			scanner.index = end + 1
			continue
		}
		scanner.index++
	}
	return bodies, nil
}

func publicationBalancedBody(command string, marker int) (string, int, error) {
	start := marker + 2
	depth := 1
	var quote byte
	for index := start; index < len(command); index++ {
		ch := command[index]
		if quote == '\'' {
			if ch == '\'' {
				quote = 0
			}
			continue
		}
		if ch == '\\' {
			index++
			continue
		}
		if ch == '\'' {
			quote = ch
			continue
		}
		if ch == '"' {
			if quote == '"' {
				quote = 0
			} else if quote == 0 {
				quote = ch
			}
			continue
		}
		if quote != 0 {
			continue
		}
		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return command[start:index], index, nil
			}
		}
	}
	return "", 0, fmt.Errorf("unterminated executable substitution")
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
