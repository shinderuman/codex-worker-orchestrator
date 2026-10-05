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
			payload := `{"tool_name":"Bash","tool_input":{"command":` + jsonString(t, command) + `}}`
			if err := Run([]string{"pre-tool-use"}, strings.NewReader(payload), &output); err != nil {
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
	payload := `{"tool_name":"Bash","tool_input":{"command":"git pull --ff-only origin main"}}`
	if err := Run([]string{"pre-tool-use"}, strings.NewReader(payload), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("ordinary pull was blocked: %s", output.String())
	}
}

func TestPreToolUseIgnoresNonBashTools(t *testing.T) {
	var output bytes.Buffer
	payload := `{"tool_name":"Read","tool_input":{"command":"git push --no-verify origin main"}}`
	if err := Run([]string{"pre-tool-use"}, strings.NewReader(payload), &output); err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("non-Bash tool was blocked: %s", output.String())
	}
}

func jsonString(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
