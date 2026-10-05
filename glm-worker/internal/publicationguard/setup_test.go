package publicationguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const publicationGuardTestHook = "#!/bin/sh\nexit 1\n"

func TestInspectPublicationGuardSetupRequiresBinding(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 1 || report.Defects[0].Hook != publicationGuardBindingName || report.Defects[0].Defect != PublicationGuardHookMissing {
		t.Fatalf("defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupAcceptsExecutableBinding(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	bindCanonicalPublicationGuardT(t, repo, filepath.Join(repo, ".githooks"))
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 0 {
		t.Fatalf("defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupAcceptsCanonicalCustomHooksPath(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	hooks := t.TempDir()
	for _, name := range publicationGuardHookNames {
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(publicationGuardTestHook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bindCanonicalPublicationGuardT(t, repo, hooks)
	runPublicationGuardGit(t, repo, "config", "core.hooksPath", hooks)
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 0 {
		t.Fatalf("defects = %#v", report.Defects)
	}
	if report.TrackedHooksMode {
		t.Fatal("custom hooks path was reported as tracked-hooks mode")
	}
}

func TestInspectPublicationGuardSetupUsesInstallerSnapshotAfterHeadMoves(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	hooks := t.TempDir()
	for _, name := range publicationGuardHookNames {
		body, err := os.ReadFile(filepath.Join(repo, ".githooks", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hooks, name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bindCanonicalPublicationGuardT(t, repo, hooks)
	runPublicationGuardGit(t, repo, "config", "core.hooksPath", hooks)

	if err := os.WriteFile(filepath.Join(repo, ".githooks", "pre-push"), []byte("#!/bin/sh\n# newer canonical hook\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runPublicationGuardGit(t, repo, "add", ".githooks/pre-push")
	runPublicationGuardGit(t, repo, "-c", "core.hooksPath="+t.TempDir(), "commit", "-q", "-m", "advance canonical hook")

	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 0 {
		t.Fatalf("HEAD movement invalidated installed snapshot: %#v", report.Defects)
	}

	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err = InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 1 || report.Defects[0].Hook != "pre-push" || report.Defects[0].Defect != PublicationGuardHookIdentityMismatch {
		t.Fatalf("tampered snapshot defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupRejectsReplacedHookIdentity(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	hooks := filepath.Join(repo, ".githooks")
	bindCanonicalPublicationGuardT(t, repo, hooks)
	if err := os.WriteFile(filepath.Join(hooks, "pre-push"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 1 || report.Defects[0].Hook != "pre-push" || report.Defects[0].Defect != PublicationGuardHookIdentityMismatch {
		t.Fatalf("defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupRejectsBindingWithoutCanonicalContract(t *testing.T) {
	repo := newPublicationGuardTestRepo(t)
	target := filepath.Join(t.TempDir(), "glm-publication-guard")
	if err := os.WriteFile(target, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePublicationGuardBindingT(t, filepath.Join(repo, ".githooks"), target+"\nsnapshot="+publicationGuardTestHead(t, repo)+"\n")
	report, err := InspectPublicationGuardSetup(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Defects) != 1 || report.Defects[0].Hook != publicationGuardBindingName || report.Defects[0].Defect != PublicationGuardBindingContract {
		t.Fatalf("defects = %#v", report.Defects)
	}
}

func TestInspectPublicationGuardSetupRejectsBindingShapesHooksReject(t *testing.T) {
	target := canonicalPublicationGuardT(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "missing snapshot", body: target + "\n"},
		{name: "missing newline", body: target + "\nsnapshot=deadbeef"},
		{name: "leading whitespace", body: " " + target + "\nsnapshot=deadbeef\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newPublicationGuardTestRepo(t)
			writePublicationGuardBindingT(t, filepath.Join(repo, ".githooks"), tc.body)
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
		if err := os.WriteFile(filepath.Join(hooks, name), []byte(publicationGuardTestHook), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runPublicationGuardGit(t, repo, "config", "user.name", "publication-guard-test")
	runPublicationGuardGit(t, repo, "config", "user.email", "publication-guard-test@example.invalid")
	runPublicationGuardGit(t, repo, "add", ".githooks")
	runPublicationGuardGit(t, repo, "commit", "-q", "-m", "seed canonical hooks")
	runPublicationGuardGit(t, repo, "config", "core.hooksPath", publicationTrackedHooksPath)
	return repo
}

func bindCanonicalPublicationGuardT(t *testing.T, repo, hooksDir string) {
	t.Helper()
	target := canonicalPublicationGuardT(t)
	writePublicationGuardBindingT(t, hooksDir, target+"\nsnapshot="+publicationGuardTestHead(t, repo)+"\n")
}

func publicationGuardTestHead(t *testing.T, repo string) string {
	t.Helper()
	command := exec.Command("git", "-C", repo, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(output[:len(output)-1])
}

func canonicalPublicationGuardT(t *testing.T) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "glm-publication-guard")
	body := "#!/bin/sh\nif [ \"${1:-}\" = probe ]; then\n  printf '%s\\n' '" + Contract + "'\n  exit 0\nfi\nexit 1\n"
	if err := os.WriteFile(target, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return target
}

func writePublicationGuardBindingT(t *testing.T, hooksDir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(hooksDir, publicationGuardBindingName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runPublicationGuardGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repo}, args...)
	command := exec.Command("git", commandArgs...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
