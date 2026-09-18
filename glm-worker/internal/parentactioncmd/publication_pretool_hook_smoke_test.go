package parentactioncmd

import (
	"os/exec"
	"strings"
	"testing"
)

func TestPublicationPreToolUseHookSmoke(t *testing.T) {
	hook := continuationHookPath(t, ".codex", "hooks", "publication-pre-tool-use.sh")
	payload := `{"tool_name":"Bash","tool_input":{"command":"git push origin main --no-verify"}}`

	t.Run("delegates payload to canonical guard", func(t *testing.T) {
		bin, calls := continuationHookBin(t, true)
		cmd := exec.Command("sh", hook)
		cmd.Env = continuationHookEnv(bin, calls)
		cmd.Stdin = strings.NewReader(payload)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("PreToolUse hook failed: %v: %s", err, output)
		}
		got := readContinuationHookFile(t, calls)
		if !strings.Contains(got, "push-binding push-guard --pre-tool-use") || !strings.Contains(got, `"tool_name":"Bash"`) {
			t.Fatalf("canonical guard call = %q", got)
		}
	})

	t.Run("fails closed when canonical guard unavailable", func(t *testing.T) {
		bin, calls := continuationHookBin(t, false)
		cmd := exec.Command("sh", hook)
		cmd.Env = continuationHookEnv(bin, calls)
		cmd.Stdin = strings.NewReader(payload)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("PreToolUse hook should block with successful hook exit: %v: %s", err, output)
		}
		if !strings.Contains(string(output), `"decision":"block"`) {
			t.Fatalf("PreToolUse hook output = %q, want block decision", output)
		}
	})
}
