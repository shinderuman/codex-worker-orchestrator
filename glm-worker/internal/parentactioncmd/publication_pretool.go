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
	Reason   string `json:"reason"`
}

const publicationPreToolUseFlag = "--pre-tool-use"

func runPublicationPreToolUse(payload string, stdout io.Writer) error {
	var input publicationPreToolUseInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		return fmt.Errorf("decode publication PreToolUse input: %w", err)
	}
	if input.ToolName != "Bash" {
		return nil
	}
	reason := publicationPreToolUseBlockReason(input.ToolInput.Command)
	if reason == "" {
		return nil
	}
	return json.NewEncoder(stdout).Encode(publicationPreToolUseOutput{Decision: "block", Reason: reason})
}

func publicationPreToolUseBlockReason(command string) string {
	for _, segment := range publicationShellSegments(command) {
		argv, ok := publicationGitArgv(segment)
		if !ok {
			continue
		}
		if publicationGitPushNoVerify(argv) {
			return "publication push rejected: --no-verify may not bypass the managed pre-push guard"
		}
		if publicationGitHooksPathBypass(argv) {
			return "publication Git operation rejected: core.hooksPath may not bypass managed repository guards"
		}
	}
	return ""
}

func publicationShellSegments(command string) [][]string {
	normalized := strings.NewReplacer("&&", " && ", "||", " || ", ";", " ; ", "|", " | ", "\n", " ; ").Replace(command)
	fields := strings.Fields(normalized)
	segments := make([][]string, 0, 1)
	current := make([]string, 0, len(fields))
	for _, field := range fields {
		if publicationShellSeparator(field) {
			if len(current) != 0 {
				segments = append(segments, current)
				current = nil
			}
			continue
		}
		current = append(current, field)
	}
	if len(current) != 0 {
		segments = append(segments, current)
	}
	return segments
}

func publicationShellSeparator(value string) bool {
	return value == "&&" || value == "||" || value == ";" || value == "|"
}

func publicationGitArgv(segment []string) ([]string, bool) {
	for index, token := range segment {
		clean := publicationShellToken(token)
		if filepath.Base(clean) == "git" {
			return segment[index+1:], true
		}
	}
	return nil, false
}

func publicationGitPushNoVerify(argv []string) bool {
	push := false
	noVerify := false
	for _, token := range argv {
		switch publicationShellToken(token) {
		case "push":
			push = true
		case "--no-verify":
			noVerify = true
		}
	}
	return push && noVerify
}

func publicationGitHooksPathBypass(argv []string) bool {
	for _, token := range argv {
		if strings.Contains(strings.ToLower(publicationShellToken(token)), "core.hookspath") {
			return true
		}
	}
	return false
}

func publicationShellToken(value string) string {
	return strings.Trim(value, "\"'(){}[]")
}
