package publicationguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInspectPublicationGuardSetupRequiresBinding(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 1 || report.Defects[0].Hook != publicationGuardBindingName || report.Defects[0].Defect != PublicationGuardHookMissing {
		t.Fatalf("defects = %#v", report.Defects)
	}
	if err := VerifyPublicationGuardSetup(repo); err == nil {
		t.Fatal("tracked hooks without glm-parent-action binding were admitted")
	}
}

func TestInspectPublicationGuardSetupAcceptsExecutableBinding(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	target := filepath.Join(t.TempDir(), "glm-parent-action")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".githooks", publicationGuardBindingName), []byte(target+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 0 {
		t.Fatalf("defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupRejectsBindingShapesHooksReject(t *testing.T) {
	target := filepath.Join(t.TempDir(), "glm-parent-action")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing newline", body: target},
		{name: "leading whitespace", body: " " + target + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationGuardTestRepo(t)
			if err := os.WriteFile(filepath.Join(repo, ".githooks", publicationGuardBindingName), []byte(tc.body), 0o600); err != nil {
				t.Fatal(err)
			}
			report, err := InspectPublicationGuardSetup(repo)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Defects) != 1 || report.Defects[0].Defect != PublicationGuardBindingInvalid {
				t.Fatalf("defects = %#v", report.Defects)
			}
		})
	}
}

func newPublicationGuardTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runPublicationGuardGit(t, repo, "init", "-q")
	hooks := filepath.Join(repo, ".githooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range publicationGuardHookNames {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runPublicationGuardGit(t, repo, "config", "core.hooksPath", publicationTrackedHooksPath)
	return repo
}

func runPublicationGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	command := exec.Command("git", commandArgs...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
