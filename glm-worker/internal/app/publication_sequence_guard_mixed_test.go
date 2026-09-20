package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublicationSequenceGuardAdmissionRejectsMixedDefectsWithoutPartialRepair(t *testing.T) {
	repoRoot := t.TempDir()
	mustRunPublicationGuardTestCommand(t, "git", "-C", repoRoot, "init")
	if err := os.MkdirAll(filepath.Join(repoRoot, ".githooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, ".githooks", "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunPublicationGuardTestCommand(t, "git", "-C", repoRoot, "config", "core.hooksPath", ".githooks")

	blocked := publicationSequenceGuardAdmission(repoRoot)
	if blocked == nil || blocked.Failure == nil {
		t.Fatalf("guard admission = %#v, want blocked failure", blocked)
	}
	if blocked.Failure.Reason != "publication_guard_setup_invalid" {
		t.Fatalf("failure reason = %q", blocked.Failure.Reason)
	}
	if blocked.NextAction != nil {
		t.Fatalf("partial repair action = %#v, want nil while a non-repairable defect exists", blocked.NextAction)
	}
	if !strings.Contains(blocked.Failure.Detail, "reference-transaction is missing") {
		t.Fatalf("failure detail = %q", blocked.Failure.Detail)
	}
}

func mustRunPublicationGuardTestCommand(t *testing.T, name string, args ...string) {
	t.Helper()
	command := exec.Command(name, args...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s %v failed: %v: %s", name, args, err, output)
	}
}
