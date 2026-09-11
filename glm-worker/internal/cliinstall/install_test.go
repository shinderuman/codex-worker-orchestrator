package cliinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRejectsUnownedCollisionsBeforeChangingAnyBinary(t *testing.T) {
	for _, collisionName := range []string{"glm-worker", "commentlint", "harnesslint"} {
		t.Run(collisionName, func(t *testing.T) {
			buildDir := t.TempDir()
			binDir := t.TempDir()
			writeBuildSet(t, buildDir, "v1")
			collisionPath := filepath.Join(binDir, collisionName)
			writeExecutable(t, collisionPath, "unrelated")

			_, err := Install(buildDir, binDir)
			if err == nil || !strings.Contains(err.Error(), "repository CLI collision") {
				t.Fatalf("Install() error = %v", err)
			}
			if got := readFile(t, collisionPath); got != "unrelated" {
				t.Fatalf("collision content = %q", got)
			}
			for _, name := range managedNames {
				if name == collisionName {
					continue
				}
				if _, err := os.Lstat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
					t.Fatalf("%s changed before collision resolved: %v", name, err)
				}
			}
		})
	}
}

func TestOwnedInstallationUpgradesAndRerunsIdempotently(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")

	first, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	assertAllStatus(t, first, "installed")
	writeBuildSet(t, buildDir, "v2")
	second, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	assertAllStatus(t, second, "upgraded")
	for _, name := range managedNames {
		if got := readFile(t, filepath.Join(binDir, name)); got != name+"-v2" {
			t.Fatalf("%s content = %q", name, got)
		}
	}
	third, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	assertAllStatus(t, third, "unchanged")
}

func TestIdenticalUnownedBinaryIsNotClaimed(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	unowned := filepath.Join(binDir, "commentlint")
	writeExecutable(t, unowned, "commentlint-v1")

	results, err := Install(buildDir, binDir)
	if err != nil {
		t.Fatal(err)
	}
	if statusFor(results, "commentlint") != "unchanged-unowned" {
		t.Fatalf("commentlint status = %q", statusFor(results, "commentlint"))
	}
	state := readStateForTest(t, binDir)
	if _, ok := state.Binaries["commentlint"]; ok {
		t.Fatal("identical unowned commentlint was claimed")
	}

	retired, err := Retire(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if statusFor(retired, "commentlint") != "" {
		t.Fatalf("retire unexpectedly managed commentlint: %+v", retired)
	}
	if got := readFile(t, unowned); got != "commentlint-v1" {
		t.Fatalf("unowned commentlint = %q", got)
	}
}

func TestRetireRemovesOnlyStillOwnedBinaries(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(binDir, "harnesslint")
	writeExecutable(t, changed, "user-modified")

	results, err := Retire(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if statusFor(results, "harnesslint") != "preserved" {
		t.Fatalf("harnesslint retire status = %q", statusFor(results, "harnesslint"))
	}
	if got := readFile(t, changed); got != "user-modified" {
		t.Fatalf("changed harnesslint = %q", got)
	}
	for _, name := range managedNames {
		if name == "harnesslint" {
			continue
		}
		if _, err := os.Lstat(filepath.Join(binDir, name)); !os.IsNotExist(err) {
			t.Fatalf("owned %s was not removed: %v", name, err)
		}
	}
	if _, err := os.Lstat(statePathForTest(binDir)); !os.IsNotExist(err) {
		t.Fatalf("ownership state remains: %v", err)
	}
}

func TestOwnedBinaryExternalChangeFailsClosed(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	if _, err := Install(buildDir, binDir); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(binDir, "glm-parent-action")
	writeExecutable(t, changed, "external-change")
	writeBuildSet(t, buildDir, "v2")

	_, err := Install(buildDir, binDir)
	if err == nil || !strings.Contains(err.Error(), "content changed externally") {
		t.Fatalf("Install() error = %v", err)
	}
	if got := readFile(t, changed); got != "external-change" {
		t.Fatalf("externally changed binary = %q", got)
	}
	if got := readFile(t, filepath.Join(binDir, "glm-worker")); got != "glm-worker-v1" {
		t.Fatalf("preflight did not prevent partial upgrade: %q", got)
	}
}

func TestDanglingSymlinkCollisionIsPreserved(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	target := filepath.Join(binDir, "commentlint")
	if err := os.Symlink(filepath.Join(binDir, "missing"), target); err != nil {
		t.Fatal(err)
	}

	_, err := Install(buildDir, binDir)
	if err == nil || !strings.Contains(err.Error(), "repository CLI collision") {
		t.Fatalf("Install() error = %v", err)
	}
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("dangling symlink changed: info=%v err=%v", info, err)
	}
}

func TestInvalidOwnershipStateFailsClosed(t *testing.T) {
	buildDir := t.TempDir()
	binDir := t.TempDir()
	writeBuildSet(t, buildDir, "v1")
	statePath := statePathForTest(binDir)
	if err := os.MkdirAll(filepath.Dir(statePath), 0o700); err != nil {
		t.Fatal(err)
	}
	data := "{\"version\":1,\"binaries\":{\"unknown\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"}}"
	if err := os.WriteFile(statePath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(buildDir, binDir); err == nil || !strings.Contains(err.Error(), "invalid CLI ownership state entry") {
		t.Fatalf("Install() error = %v", err)
	}
}

func writeBuildSet(t *testing.T, dir, version string) {
	t.Helper()
	for _, name := range managedNames {
		writeExecutable(t, filepath.Join(dir, name), name+"-"+version)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertAllStatus(t *testing.T, results []Result, want string) {
	t.Helper()
	if len(results) != len(managedNames) {
		t.Fatalf("results = %+v", results)
	}
	for _, result := range results {
		if result.Status != want {
			t.Fatalf("%s status = %q, want %q", result.Name, result.Status, want)
		}
	}
}

func statusFor(results []Result, name string) string {
	for _, result := range results {
		if result.Name == name {
			return result.Status
		}
	}
	return ""
}

func statePathForTest(binDir string) string {
	return filepath.Join(binDir, ".codex-worker-orchestrator", "cli-install-state.json")
}

func readStateForTest(t *testing.T, binDir string) installState {
	t.Helper()
	data, err := os.ReadFile(statePathForTest(binDir))
	if err != nil {
		t.Fatal(err)
	}
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		t.Fatal(err)
	}
	return state
}
