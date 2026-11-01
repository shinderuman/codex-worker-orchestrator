package goqualitycmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func runScriptFixtureGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func TestGoqualityScriptRoutesDockerBackendThroughStagingOwner(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Docker test backend契約のrouting検証はDarwin ARM64 host向け")
	}
	root := t.TempDir()
	runScriptFixtureGit(t, root, "init", "-q")
	runScriptFixtureGit(t, root, "config", "user.name", "fixture")
	runScriptFixtureGit(t, root, "config", "user.email", "fixture@example.invalid")
	version := strings.TrimPrefix(runtime.Version(), "go")
	if err := os.WriteFile(filepath.Join(root, "quality-tools.yml"), []byte("go: "+version+"\ngo-test-macos-backend: docker-linux-arm64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "go-invocations")
	fakeGo := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >>'" + record + "'\n" +
		"case \" $* \" in\n" +
		"  *\" env GOVERSION \"*) printf 'go%s\\n' '" + version + "' ;;\n" +
		"esac\n" +
		"case \" $* \" in\n" +
		"  *\" --backend \"*) printf 'docker-linux-arm64\\n' ;;\n" +
		"esac\n" +
		"exit 0\n"
	if err := execfile.Write(filepath.Join(bin, "go"), []byte(fakeGo), 0o700); err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
	if err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), "worker", "docker-test")
	responses := filepath.Join(t.TempDir(), "docker-test-responses")

	runScript := func(extraEnv []string) {
		t.Helper()
		command := exec.Command(script, "test", "./internal/runner", "-run", "^TestFocused$")
		command.Dir = root
		command.Env = append([]string{"PATH=" + bin + ":" + os.Getenv("PATH")}, extraEnv...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("goquality test routing失敗: %v: %s", err, output)
		}
	}
	runScript([]string{
		"GLM_WORKER_DOCKER_TEST_STAGING=" + staging,
		"GLM_WORKER_DOCKER_TEST_RESPONSES=" + responses,
	})
	runScript(nil)

	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	stagedLine := ""
	directLine := ""
	for _, line := range lines {
		switch {
		case strings.Contains(line, "--staging"):
			stagedLine = line
		case strings.Contains(line, "run ./cmd/goquality") && !strings.Contains(line, "--backend"):
			directLine = line
		}
	}
	if !strings.Contains(stagedLine, "--staging "+staging) || !strings.Contains(stagedLine, "--responses "+responses) {
		t.Fatalf("staging owner経路の引数 = %q want staging %q responses %q", stagedLine, staging, responses)
	}
	if !strings.Contains(stagedLine, "./internal/runner -run ^TestFocused$") {
		t.Fatalf("staging経路のgo test引数 = %q", stagedLine)
	}
	if !strings.Contains(directLine, "run ./cmd/goquality "+root+" ./internal/runner") {
		t.Fatalf("非worker sessionの直接経路 = %q", directLine)
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
