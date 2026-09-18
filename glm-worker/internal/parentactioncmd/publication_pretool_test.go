package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicationPreToolUseBlocksGitGuardBypass(t *testing.T) {
	commands := []string{
		"git push --no-verify origin main",
		"git push origin main --no-verify",
		"git commit --no-verify -m bypass",
		"cd repo && /usr/bin/git push origin main --no-verify",
		"bash -lc 'git push origin main --no-verify'",
		"env sh -c 'git commit --no-verify -m bypass'",
		"eval git push origin main --no-verify",
		"git -c core.hooksPath=/dev/null push origin main",
		"GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null git push origin main",
		"git config core.hooksPath /dev/null",
		"git config --local --unset-all core.hooksPath",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := runPublicationPreToolUse(publicationPreToolPayload(t, command), &stdout); err != nil {
				t.Fatal(err)
			}
			var output publicationPreToolUseOutput
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatalf("block output is not JSON: %v: %q", err, stdout.String())
			}
			if output.Decision != "block" || output.Reason == "" {
				t.Fatalf("bypass was not blocked: %#v", output)
			}
		})
	}
}

func TestPublicationPreToolUseAllowsManagedPushAndUnrelatedCommands(t *testing.T) {
	commands := []string{
		"git push origin main",
		"git push -n origin main",
		"echo --no-verify",
		"echo git push --no-verify",
		"go test ./...",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			var stdout bytes.Buffer
			if err := runPublicationPreToolUse(publicationPreToolPayload(t, command), &stdout); err != nil {
				t.Fatal(err)
			}
			if strings.TrimSpace(stdout.String()) != "" {
				t.Fatalf("allowed command emitted hook decision: %q", stdout.String())
			}
		})
	}
}

func TestPublicationPreToolUseIgnoresNonBashTool(t *testing.T) {
	payload := `{"tool_name":"Read","tool_input":{"command":"git push origin main --no-verify"}}`
	var stdout bytes.Buffer
	if err := runPublicationPreToolUse(payload, &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("non-Bash tool was blocked: %q", stdout.String())
	}
}

func publicationPreToolPayload(t *testing.T, command string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]string{"command": command},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}
