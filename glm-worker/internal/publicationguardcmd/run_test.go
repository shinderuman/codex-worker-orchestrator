package publicationguardcmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreToolUseBlocksManagedGitHookBypass(t *testing.T) {
	for _, command := range []string{
		"git push --no-verify origin main",
		"git -c core.hooksPath=/tmp/disabled push origin main",
		"git config --local core.hooksPath /tmp/disabled",
	} {
		t.Run(command, func(t *testing.T) {
			var output bytes.Buffer
			if err := Run([]string{"pre-tool-use"}, strings.NewReader(preToolUsePayload(t, "Bash", command)), &output); err != nil {
				t.Fatal(err)
			}
			var decision preToolUseOutput
			if err := json.Unmarshal(output.Bytes(), &decision); err != nil {
				t.Fatalf("output = %q: %v", output.String(), err)
			}
			if decision.Decision != "block" || decision.Code != "managed_git_hook_bypass" {
				t.Fatalf("decision = %#v", decision)
			}
		})
	}
}

func TestPreToolUseAllowsOrdinaryGitPull(t *testing.T) {
	var output bytes.Buffer
	if err := Run([]string{"pre-tool-use"}, strings.NewReader(preToolUsePayload(t, "Bash", "git pull --ff-only origin main")), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("ordinary pull was blocked: %s", output.String())
	}
}

func TestPreToolUseIgnoresNonBashTools(t *testing.T) {
	var output bytes.Buffer
	if err := Run([]string{"pre-tool-use"}, strings.NewReader(preToolUsePayload(t, "Read", "git push --no-verify origin main")), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("non-Bash tool was blocked: %s", output.String())
	}
}

func preToolUsePayload(t *testing.T, toolName, command string) string {
	t.Helper()
	input := preToolUseInput{ToolName: toolName}
	input.ToolInput.Command = command
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
