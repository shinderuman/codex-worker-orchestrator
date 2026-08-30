package harnesslint

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootLaunchersSharePinnedGoEnvironment(t *testing.T) {
	root := launcherTestRepoRoot(t)
	records := map[string]string{}
	for _, name := range []string{"commentlint", "harnesslint"} {
		records[name] = runLauncherRecordingGoEnvironment(t, root, name)
	}
	if records["commentlint"] != records["harnesslint"] {
		t.Fatalf("pinned go environment differs: commentlint=%q harnesslint=%q", records["commentlint"], records["harnesslint"])
	}
	toolchain, gocache, ok := strings.Cut(records["commentlint"], "\n")
	if !ok || !strings.HasPrefix(toolchain, "go") {
		t.Fatalf("recorded environment is not a pinned toolchain: %q", records["commentlint"])
	}
	version := strings.TrimPrefix(toolchain, "go")
	if semanticVersion.FindString(version) != version {
		t.Fatalf("toolchain version is not the full contract version: %q", toolchain)
	}
	expectedCache := launcherTestCacheRoot() + "/codex-worker-orchestrator-go-" + version
	if gocache != expectedCache {
		t.Fatalf("gocache = %q want %q", gocache, expectedCache)
	}
}

func runLauncherRecordingGoEnvironment(t *testing.T, root, name string) string {
	t.Helper()
	stubDir := t.TempDir()
	probe := filepath.Join(stubDir, name+".env")
	stub := "#!/bin/sh\nprintf '%s\\n%s\\n' \"$GOTOOLCHAIN\" \"$GOCACHE\" >\"$GLM_TEST_LAUNCHER_PROBE\"\n"
	if err := os.WriteFile(filepath.Join(stubDir, "go"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GLM_TEST_LAUNCHER_PROBE", probe)
	t.Setenv("GOTOOLCHAIN", "")
	t.Setenv("GOCACHE", "")
	command := exec.Command(filepath.Join(root, name))
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s launcher failed: %v: %s", name, err, output)
	}
	recorded, err := os.ReadFile(probe)
	if err != nil {
		t.Fatalf("%s launcher did not reach the go command: %v", name, err)
	}
	return strings.TrimRight(string(recorded), "\n")
}

func launcherTestRepoRoot(t *testing.T) string {
	t.Helper()
	output, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(output))
}

func launcherTestCacheRoot() string {
	if dir := os.Getenv("TMPDIR"); dir != "" {
		return dir
	}
	return "/tmp"
}
