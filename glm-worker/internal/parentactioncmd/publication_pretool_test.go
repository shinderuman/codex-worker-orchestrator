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
		"bash --norc -c 'git push origin main --no-verify'",
		"bash --rcfile /dev/null -c 'git push origin main --no-verify'",
		"env sh -c 'git commit --no-verify -m bypass'",
		"eval git push origin main --no-verify",
		"git push origin main --no-\"verify\"",
		"git commit --no-'verify' -m bypass",
		"/usr/bin/\"git\" push origin main --no-\"verify\"",
		"git -c core.hooksPath=/dev/null push origin main",
		"GIT_CONFIG_KEY_0=core.hooksPath GIT_CONFIG_VALUE_0=/dev/null git push origin main",
		"git config core.hooksPath /dev/null",
		"git config --local --unset-all core.hooksPath",
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			output := publicationPreToolDecision(t, command)
			if output.Decision != "block" || output.Code != publicationGitGuardBypassCode || output.Reason == "" {
				t.Fatalf("bypass was not blocked: %#v", output)
			}
		})
	}
}

func TestPublicationPreToolUseFailsClosedForDynamicGitClassification(t *testing.T) {
	commands := []string{
		`git push origin main "$BYPASS"`,
		`$GIT push origin main`,
		`GIT_OPTION=$VALUE git push origin main`,
		`bash -lc "$COMMAND"`,
		`eval "$COMMAND"`,
		`git push origin main $(printf -- --no-verify)`,
	}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			output := publicationPreToolDecision(t, command)
			if output.Decision != "block" || output.Code != publicationGitClassificationCode || output.Reason != publicationGitClassificationReason {
				t.Fatalf("dynamic Git classification did not fail closed: %#v", output)
			}
		})
	}
}

func TestPublicationPreToolUseAllowsManagedPushAndUnrelatedCommands(t *testing.T) {
	commands := []string{
		"git push origin main",
		"git push -n origin main",
		"command git push origin main",
		"env SAFE=value git push origin main",
		"bash -lc 'git push origin main'",
		"bash --norc -c 'git push origin main'",
		"bash --rcfile /dev/null -c 'git push origin main'",
		"echo --no-verify",
		"echo git push --no-verify",
		`echo "$HOME"`,
		`printf '%s\n' '--no-"verify"'`,
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

func publicationPreToolDecision(t *testing.T, command string) publicationPreToolUseOutput {
	t.Helper()
	var stdout bytes.Buffer
	if err := runPublicationPreToolUse(publicationPreToolPayload(t, command), &stdout); err != nil {
		t.Fatal(err)
	}
	var output publicationPreToolUseOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("block output is not JSON: %v: %q", err, stdout.String())
	}
	return output
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
