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

func publicationPreToolUseBlockReason(command string) string {
	_, reason := publicationPreToolUseBlockDecision(command)
	return reason
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
	commandIndex, ok := publicationShellCommandIndex(segment)
	if !ok {
		return "", ""
	}
	command := segment[commandIndex]
	if command.Dynamic {
		return publicationGitClassificationCode, publicationGitClassificationReason
	}
	executable := filepath.Base(command.Value)
	switch executable {
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

func publicationShellCommandIndex(segment []publicationShellWord) (int, bool) {
	for index, word := range segment {
		if word.Dynamic {
			return index, true
		}
		clean := word.Value
		if strings.Contains(clean, "=") || clean == "command" || clean == "exec" || clean == "sudo" || clean == "env" {
			continue
		}
		return index, true
	}
	return 0, false
}

func publicationClassifyInterpreter(args []publicationShellWord, depth int) (string, string) {
	for index, word := range args {
		if word.Dynamic {
			return publicationGitClassificationCode, publicationGitClassificationReason
		}
		if !strings.HasPrefix(word.Value, "-") || !strings.Contains(strings.TrimPrefix(word.Value, "-"), "c") {
			continue
		}
		if index+1 >= len(args) || args[index+1].Dynamic {
			return publicationGitClassificationCode, publicationGitClassificationReason
		}
		return publicationClassifyShell(args[index+1].Value, depth)
	}
	return "", ""
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
	argv := segment[commandIndex+1:]
	if publicationGitNoVerify(argv) {
		return publicationGitGuardBypassCode, publicationGitNoVerifyReason
	}
	if publicationGitHooksPathBypass(segment) {
		return publicationGitGuardBypassCode, publicationGitHooksPathReason
	}
	return "", ""
}

func publicationShellSegments(command string) ([][]publicationShellWord, error) {
	segments := make([][]publicationShellWord, 0, 1)
	current := make([]publicationShellWord, 0, 4)
	var value strings.Builder
	var quote byte
	wordStarted := false
	dynamic := false

	flushWord := func() {
		if !wordStarted {
			return
		}
		current = append(current, publicationShellWord{Value: value.String(), Dynamic: dynamic})
		value.Reset()
		wordStarted = false
		dynamic = false
	}
	flushSegment := func() {
		flushWord()
		if len(current) != 0 {
			segments = append(segments, current)
			current = nil
		}
	}

	for index := 0; index < len(command); index++ {
		ch := command[index]
		switch quote {
		case '\'':
			if ch == '\'' {
				quote = 0
				continue
			}
			value.WriteByte(ch)
			continue
		case '"':
			switch ch {
			case '"':
				quote = 0
			case '`':
				return nil, fmt.Errorf("dynamic command substitution")
			case '$':
				if index+1 < len(command) && command[index+1] == '(' {
					return nil, fmt.Errorf("dynamic command substitution")
				}
				dynamic = true
				wordStarted = true
				value.WriteByte(ch)
			case '\\':
				if index+1 >= len(command) {
					return nil, fmt.Errorf("unterminated escape")
				}
				index++
				value.WriteByte(command[index])
			default:
				value.WriteByte(ch)
			}
			continue
		}

		switch ch {
		case ' ', '\t', '\r':
			flushWord()
		case '\n', ';', '|', '&':
			flushSegment()
			if (ch == '|' || ch == '&') && index+1 < len(command) && command[index+1] == ch {
				index++
			}
		case '\'', '"':
			quote = ch
			wordStarted = true
		case '\\':
			if index+1 >= len(command) {
				return nil, fmt.Errorf("unterminated escape")
			}
			index++
			wordStarted = true
			value.WriteByte(command[index])
		case '`':
			return nil, fmt.Errorf("dynamic command substitution")
		case '$':
			if index+1 < len(command) && command[index+1] == '(' {
				return nil, fmt.Errorf("dynamic command substitution")
			}
			dynamic = true
			wordStarted = true
			value.WriteByte(ch)
		case '#':
			if !wordStarted {
				for index+1 < len(command) && command[index+1] != '\n' {
					index++
				}
				flushSegment()
				continue
			}
			wordStarted = true
			value.WriteByte(ch)
		default:
			wordStarted = true
			value.WriteByte(ch)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flushSegment()
	return segments, nil
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
