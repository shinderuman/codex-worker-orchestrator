package cliinstall

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMigrateLegacyRepositoryInstallThenUpgrade(t *testing.T) {
	binDir := t.TempDir()
	buildLegacyRepositoryCLIs(t, binDir)

	migrated, err := MigrateLegacy(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if !migrated {
		t.Fatal("legacy repository CLI installation was not migrated")
	}
	state := readStateForTest(t, binDir)
	if len(state.Binaries) != len(managedNames) {
		t.Fatalf("migrated ownership entries = %d, want %d", len(state.Binaries), len(managedNames))
	}

	buildDir := t.TempDir()
	writeBuildSet(t, buildDir, "next")
	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	assertAllStatus(t, results, "upgraded")
	for _, name := range managedNames {
		if got := readFile(t, filepath.Join(binDir, name)); got != name+"-next" {
			t.Fatalf("%s content = %q", name, got)
		}
	}

	migrated, err = MigrateLegacy(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Fatal("ownership state was migrated twice")
	}
}

func TestMigrateLegacyDoesNotClaimPartialUnprovenInstall(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "glm-worker"), "unrelated")

	migrated, err := MigrateLegacy(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if migrated {
		t.Fatal("partial unproven installation was claimed")
	}
	if _, err := os.Lstat(statePathForTest(binDir)); !os.IsNotExist(err) {
		t.Fatalf("ownership state unexpectedly created: %v", err)
	}
}

func TestLegacyBuildInfoMatchesExactRepositoryCommand(t *testing.T) {
	commandPath := repositoryModulePath + "/cmd/commentlint"
	if !legacyBuildInfoMatches(commandPath, repositoryModulePath, "commentlint") {
		t.Fatal("exact repository command metadata did not match")
	}
	if legacyBuildInfoMatches(commandPath, repositoryModulePath, "harnesslint") {
		t.Fatal("different command name matched")
	}
	if legacyBuildInfoMatches(commandPath, "example.com/unrelated", "commentlint") {
		t.Fatal("different module matched")
	}
}

func buildLegacyRepositoryCLIs(t *testing.T, binDir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	moduleRoot := filepath.Clean(filepath.Join(cwd, "..", ".."))
	for _, name := range managedNames {
		target := filepath.Join(binDir, name)
		command := exec.Command("go", "build", "-buildvcs=false", "-trimpath", "-o", target, "./cmd/"+name)
		command.Dir = moduleRoot
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("build legacy %s: %v\n%s", name, err, output)
		}
		if err := os.Chmod(target, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}
