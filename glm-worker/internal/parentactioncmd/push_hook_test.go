package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrePushHookEnforcesParentCompletionAuthorization(t *testing.T) {
	hook, err := filepath.Abs(filepath.Join("..", "..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	fakeBin := t.TempDir()
	fakeParentAction := filepath.Join(fakeBin, "glm-parent-action")
	if err := os.WriteFile(fakeParentAction, []byte("#!/bin/sh\nprintf '%s\\n' \"$FAKE_PUSH_BINDING_OUTPUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	localOID := strings.Repeat("a", 40)
	remoteOID := strings.Repeat("0", 40)
	stdin := "refs/heads/main " + localOID + " refs/heads/main " + remoteOID + "\n"
	authorized := `{"status":"classified","target":{"remote_name":"origin","remote_ref":"refs/heads/main"},"classification":"remote_sync_pending_local_ahead","remote_write":{"authorization":"parent_standing_authority","remote_name":"origin","remote_ref":"refs/heads/main"}}`
	blocked := `{"status":"blocked","target":{"remote_name":"origin","remote_ref":"refs/heads/main"},"classification":"remote_sync_pending_local_ahead","failure":{"stage":"authorization","reason":"parent_completion_not_ready"}}`

	if output, err := runPrePushHookFixture(hook, fakeBin, authorized, "origin", stdin); err != nil {
		t.Fatalf("authorized push was rejected: %v: %s", err, output)
	}
	if output, err := runPrePushHookFixture(hook, fakeBin, blocked, "origin", stdin); err == nil {
		t.Fatalf("unready push was accepted: %s", output)
	}
	wrongRef := "refs/heads/main " + localOID + " refs/heads/other " + remoteOID + "\n"
	if output, err := runPrePushHookFixture(hook, fakeBin, authorized, "origin", wrongRef); err == nil {
		t.Fatalf("wrong-ref push was accepted: %s", output)
	}
}

func runPrePushHookFixture(hook, fakeBin, bindingOutput, remoteName, stdin string) (string, error) {
	command := exec.Command(hook, remoteName, "unused-remote-url")
	command.Stdin = strings.NewReader(stdin)
	command.Env = append(os.Environ(),
		"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_PUSH_BINDING_OUTPUT="+bindingOutput,
	)
	output, err := command.CombinedOutput()
	return string(output), err
}
