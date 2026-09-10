package parentactioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRuntimeInstallPathClassification(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{path: "glm-worker/internal/app/app.go", want: true},
		{path: "glm-worker/internal/app/app_test.go", want: false},
		{path: "glm-worker/go.mod", want: true},
		{path: "codex/instructions/glm-execution.md", want: true},
		{path: "codex/config-managed.toml", want: true},
		{path: "claude/settings-managed.json", want: true},
		{path: "install.sh", want: true},
		{path: "quality-tools.yml", want: true},
		{path: "README.md", want: false},
		{path: "IMPLEMENTATION_TASKS/example.md", want: false},
	}
	for _, tc := range cases {
		if got := runtimeInstallPath(tc.path); got != tc.want {
			t.Fatalf("runtimeInstallPath(%q) = %v want %v", tc.path, got, tc.want)
		}
	}
}

func TestRuntimeInstallRequirementMixedDiffKeepsOnlyRuntimePaths(t *testing.T) {
	cfg, st := newInstallActionRepo(t)
	writeInstallActionScript(t, cfg.RepoRoot, "#!/bin/sh\nexit 0\n", 0o755)
	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "README.md"), []byte("metadata\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "add", "README.md").CombinedOutput(); err != nil {
		t.Fatalf("git add metadata: %v: %s", err, output)
	}
	if output, err := exec.Command("git", "-C", cfg.RepoRoot, "commit", "-q", "-m", "metadata change").CombinedOutput(); err != nil {
		t.Fatalf("git commit metadata: %v: %s", err, output)
	}

	requirement, err := runtimeInstallRequirementForTask(cfg.RepoRoot, st)
	if err != nil {
		t.Fatal(err)
	}
	if !requirement.Required {
		t.Fatal("mixed diff did not require runtime install")
	}
	if len(requirement.Paths) != 1 || requirement.Paths[0] != installScriptName {
		t.Fatalf("mixed diff runtime paths = %#v", requirement.Paths)
	}
}

func TestCompleteRequiresRuntimeInstallEvidenceAndAllowsMetadataHeadAdvance(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	writeRuntimeInstallHarnessMarker(t, fixture.repo)
	writeRuntimeInstallSource(t, fixture.repo, "version=1\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "runtime")
	installedHead := completeFixtureHead(t, fixture.repo)
	requirement, err := runtimeInstallRequirementForTask(fixture.repo, fixture.st)
	if err != nil || !requirement.Required {
		t.Fatalf("runtime requirement = %#v err=%v", requirement, err)
	}

	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	missing := runCompleteCommand(t, fixture)
	if missing.Status != completeStatusAwaiting || missing.Completed || missing.Failure == nil || missing.Failure.Reason != runtimeInstallFailureEvidence {
		t.Fatalf("completion without install evidence = %#v", missing)
	}

	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              installedHead,
		SourceDigest:      requirement.SourceDigest,
		InstalledRevision: installedHead,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}
	writeInstalledRuntimeProbeStub(t, installedHead)

	completed := runCompleteCommand(t, fixture)
	if completed.Status != completeStatusComplete || !completed.Completed {
		t.Fatalf("metadata-only head advance rejected valid runtime evidence = %#v", completed)
	}
}

func TestCompleteRejectsInstallEvidenceAfterLaterRuntimeChange(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := state.CaptureGitBaseline(fixture.cfg, fixture.st); err != nil {
		t.Fatal(err)
	}
	writeRuntimeInstallHarnessMarker(t, fixture.repo)
	writeRuntimeInstallSource(t, fixture.repo, "version=1\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "runtime one")
	installedHead := completeFixtureHead(t, fixture.repo)
	installedRequirement, err := runtimeInstallRequirementForTask(fixture.repo, fixture.st)
	if err != nil || !installedRequirement.Required {
		t.Fatalf("installed requirement = %#v err=%v", installedRequirement, err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.SaveRuntimeInstallEvidence(state.RuntimeInstallEvidence{
		Version:           1,
		TaskID:            taskID,
		Head:              installedHead,
		SourceDigest:      installedRequirement.SourceDigest,
		InstalledRevision: installedHead,
		SmokeResult:       state.ValidationResultPass,
	}); err != nil {
		t.Fatal(err)
	}

	writeRuntimeInstallSource(t, fixture.repo, "version=2\n")
	runFinalizationGit(t, fixture.repo, "add", "-A")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "runtime two")
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	writeInstalledRuntimeProbeStub(t, installedHead)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed || output.Failure == nil || output.Failure.Reason != runtimeInstallFailureStale {
		t.Fatalf("completion accepted stale runtime evidence = %#v", output)
	}
}

func TestVerifyRuntimeInstalledFilesRejectsManagedInstructionDrift(t *testing.T) {
	cfg, _ := newInstallActionRepo(t)
	cfg.CodexConfigDir = t.TempDir()
	sourcePath := filepath.Join(cfg.RepoRoot, "codex", "instructions", "example.md")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("current\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	installedPath := filepath.Join(cfg.CodexConfigDir, "instructions", "example.md")
	if err := os.MkdirAll(filepath.Dir(installedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeInstalledFiles(cfg, []string{"codex/instructions/example.md"}); err == nil {
		t.Fatal("stale managed instruction was accepted")
	}
}

func TestVerifyRuntimeInstalledFilesHandlesManagedInstructionDeletion(t *testing.T) {
	cfg, _ := newInstallActionRepo(t)
	cfg.CodexConfigDir = t.TempDir()
	installedPath := filepath.Join(cfg.CodexConfigDir, "instructions", "removed.md")
	if err := os.MkdirAll(filepath.Dir(installedPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installedPath, []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paths := []string{"codex/instructions/removed.md"}
	if err := verifyRuntimeInstalledFiles(cfg, paths); err == nil {
		t.Fatal("installed managed instruction surviving source deletion was accepted")
	}
	if err := os.Remove(installedPath); err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimeInstalledFiles(cfg, paths); err != nil {
		t.Fatalf("matching managed deletion was rejected: %v", err)
	}
}

func TestRunRuntimeInstallSmokeRejectsFailedInstalledSmoke(t *testing.T) {
	cfg, _ := newInstallActionRepo(t)
	writeInstalledRuntimeProbeStub(t, "unused")
	failure := runRuntimeInstallSmoke(cfg)
	if failure == nil || failure.Reason != runtimeInstallFailureSmoke {
		t.Fatalf("failed installed smoke = %#v", failure)
	}
}

func writeRuntimeInstallHarnessMarker(t *testing.T, repo string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, repositoryharness.MarkerPath), []byte(repositoryharness.MarkerContent), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeInstallSource(t *testing.T, repo, content string) {
	t.Helper()
	path := filepath.Join(repo, "glm-worker", "internal", "runtime_install_fixture.go")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package internal\n\nvar runtimeInstallFixture = \""+content[:len(content)-1]+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeInstalledRuntimeProbeStub(t *testing.T, revision string) {
	t.Helper()
	binDir := t.TempDir()
	script := `#!/bin/sh
if [ "${1:-}" != "--status" ]; then
  exit 2
fi
head=$(git -C "$PWD" rev-parse HEAD)
if [ "$head" = "` + revision + `" ]; then
  relationship=same
else
  relationship=ancestor
fi
printf '{"runtime_build":{"vcs_revision":"%s","vcs_modified":false,"repository_head":"%s","relationship":"%s"}}\n' "` + revision + `" "$head" "$relationship"
`
	if err := os.WriteFile(filepath.Join(binDir, "glm-worker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
