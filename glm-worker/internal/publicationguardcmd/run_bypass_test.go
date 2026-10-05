package publicationguardcmd

import "testing"

func TestManagedHookBypassRejectsCommitShortNoVerifyOnlyForGitCommit(t *testing.T) {
	for _, command := range []string{
		"git commit -n -m test",
		"git -C repo commit -n -m test",
		"cd repo && git commit -n -m test",
	} {
		if code, _ := managedHookBypass(command); code != "managed_git_hook_bypass" {
			t.Fatalf("commit -n was not rejected: %q", command)
		}
	}
	for _, command := range []string{
		"git show -n 1",
		"printf -n",
		"echo git commit -n",
	} {
		if code, _ := managedHookBypass(command); code != "" {
			t.Fatalf("unrelated -n was rejected: %q", command)
		}
	}
}
