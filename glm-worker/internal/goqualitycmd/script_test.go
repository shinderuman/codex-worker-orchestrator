package goqualitycmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func runScriptFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestGoqualityScriptSnapshotHandlesUntrackedNestedRepositoryAndDanglingSymlink(t *testing.T) {
	root := t.TempDir()
	runScriptFixtureGit(t, root, "init", "-q")
	runScriptFixtureGit(t, root, "config", "user.name", "fixture")
	runScriptFixtureGit(t, root, "config", "user.email", "fixture@example.invalid")
	tools := "go: " + strings.TrimPrefix(runtime.Version(), "go") + "\n"
	for name, body := range map[string]string{
		"quality-tools.yml":                    tools,
		filepath.Join("glm-worker", "go.mod"):  "module fixture/glm-worker\n\ngo 1.22\n",
		filepath.Join("glm-worker", "main.go"): "package main\n\nfunc main() {}\n",
	} {
		target := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runScriptFixtureGit(t, root, "add", ".")
	runScriptFixtureGit(t, root, "commit", "-qm", "fixture")
	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "inner.txt"), []byte("inner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runScriptFixtureGit(t, nested, "init", "-q")
	if err := os.Symlink("nowhere", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(script, "test", "./...")
	command.Dir = root
	outputBytes, runErr := command.CombinedOutput()
	text := string(outputBytes)
	if runErr != nil {
		t.Fatalf("goquality testがuntracked特殊pathで失敗しました: %v\n%s", runErr, text)
	}
	if strings.Contains(text, "source snapshotを取得できません") {
		t.Fatalf("source snapshot取得がuntracked特殊pathで中断しました: %s", text)
	}
	if !strings.Contains(text, "goquality-diagnostic ") || !strings.Contains(text, `"source_changed":false`) {
		t.Fatalf("診断行が揃っていません: %s", text)
	}
}
