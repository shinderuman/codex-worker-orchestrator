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
	root := newScriptFixtureRepo(t)
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

func newScriptFixtureRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runScriptFixtureGit(t, root, "init", "-q")
	runScriptFixtureGit(t, root, "config", "user.name", "fixture")
	runScriptFixtureGit(t, root, "config", "user.email", "fixture@example.invalid")
	return root
}

func writeScriptFixtureModule(t *testing.T, root string, moduleFiles map[string]string) {
	t.Helper()
	tools := "go: " + strings.TrimPrefix(runtime.Version(), "go") + "\n"
	files := map[string]string{
		"quality-tools.yml":                    tools,
		filepath.Join("glm-worker", "go.mod"):  "module fixture/glm-worker\n\ngo 1.22\n",
		filepath.Join("glm-worker", "main.go"): "package main\n\nfunc main() {}\n",
	}
	for name, body := range moduleFiles {
		files[name] = body
	}
	for name, body := range files {
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
}

func runScriptTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(script, args...)
	command.Dir = root
	outputBytes, runErr := command.CombinedOutput()
	text := string(outputBytes)
	if runErr != nil {
		t.Fatalf("goquality %vが失敗しました: %v\n%s", args, runErr, text)
	}
	return text
}

func addUntrackedNestedRepository(t *testing.T, root, name string) {
	t.Helper()
	nested := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(nested, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "inner.txt"), []byte("inner\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "sub", "deep.txt"), []byte("deep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("inner.txt", filepath.Join(nested, "link")); err != nil {
		t.Fatal(err)
	}
	innard := filepath.Join(nested, "innard")
	if err := os.MkdirAll(innard, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innard, ".git"), []byte("gitdir: ../.git\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(innard, "data.txt"), []byte("innard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runScriptFixtureGit(t, nested, "init", "-q")
}

func TestGoqualityScriptSnapshotHandlesUntrackedNestedRepositoryAndDanglingSymlink(t *testing.T) {
	root := newScriptFixtureRepo(t)
	writeScriptFixtureModule(t, root, nil)
	addUntrackedNestedRepository(t, root, "nested")
	if err := os.Symlink("nowhere", filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("glm-worker", "go.mod"), filepath.Join(root, "relative")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("plain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	text := runScriptTest(t, root, "test", "-count=1", "./...")
	if strings.Contains(text, "source snapshotを取得できません") {
		t.Fatalf("source snapshot取得がuntracked特殊pathで中断しました: %s", text)
	}
	if !strings.Contains(text, "goquality-diagnostic ") || !strings.Contains(text, `"source_changed":false`) {
		t.Fatalf("診断行が揃っていません: %s", text)
	}
	if strings.Contains(text, "excluded_untracked_directories") {
		t.Fatalf("nested repository worktreeをsource scopeへ含めた状態で除外記録が残っています: %s", text)
	}
}

func TestGoqualityScriptDetectsNestedRepositoryContentChangeDuringTest(t *testing.T) {
	root := newScriptFixtureRepo(t)
	writeScriptFixtureModule(t, root, map[string]string{
		filepath.Join("glm-worker", "mutate_test.go"): "package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMutateNestedRepositoryContent(t *testing.T) {\n\tif err := os.WriteFile(\"../nested/sub/deep.txt\", []byte(\"changed\\n\"), 0o644); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n",
	})
	addUntrackedNestedRepository(t, root, "nested")

	script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(script, "test", "-count=1", "./...")
	command.Dir = root
	outputBytes, runErr := command.CombinedOutput()
	text := string(outputBytes)
	if runErr == nil {
		t.Fatalf("テスト実行中のnested repository内容変更がsource変更として検出されませんでした: %s", text)
	}
	if !strings.Contains(text, `"source_changed":true`) {
		t.Fatalf("nested repositoryの内容変更がsource変更として報告されていません: %s", text)
	}
}

func TestGoqualityScriptIgnoresNestedGitFileMetadataChangeDuringTest(t *testing.T) {
	root := newScriptFixtureRepo(t)
	writeScriptFixtureModule(t, root, map[string]string{
		filepath.Join("glm-worker", "mutate_test.go"): "package main\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestMutateNestedGitFile(t *testing.T) {\n\tif err := os.WriteFile(\"../nested/innard/.git\", []byte(\"gitdir: /elsewhere\\n\"), 0o644); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n",
	})
	addUntrackedNestedRepository(t, root, "nested")

	text := runScriptTest(t, root, "test", "-count=1", "./...")
	if !strings.Contains(text, `"source_changed":false`) {
		t.Fatalf("nested .git fileのmetadata変化がsource変更として報告されました: %s", text)
	}
}

func TestGoqualityScriptRejectsIgnoredFileInsideNestedRepository(t *testing.T) {
	root := newScriptFixtureRepo(t)
	writeScriptFixtureModule(t, root, map[string]string{
		".gitignore": "*.log\n",
	})
	addUntrackedNestedRepository(t, root, "nested")
	if err := os.WriteFile(filepath.Join(root, "nested", "trace.log"), []byte("on disk only after transfer\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
	if err != nil {
		t.Fatal(err)
	}
	tempArea := t.TempDir()
	env := []string{"TMPDIR=" + tempArea}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "TMPDIR=") {
			env = append(env, item)
		}
	}
	command := exec.Command(script, "test", "./...")
	command.Dir = root
	command.Env = env
	outputBytes, runErr := command.CombinedOutput()
	text := string(outputBytes)
	if runErr == nil {
		t.Fatalf("nested repository内のouter-ignored fileが無音通過されました: %s", text)
	}
	if !strings.Contains(text, "nested repository内のignored fileは診断sourceに含められません") {
		t.Fatalf("拒否理由が出力にありません: %s", text)
	}
	residue, err := os.ReadDir(tempArea)
	if err != nil {
		t.Fatal(err)
	}
	if len(residue) != 0 {
		t.Fatalf("拒否run後に一時fileが残留しています: %v", residue)
	}
}

func TestGoqualityScriptRejectsSymlinkOutsideDiagnosticSource(t *testing.T) {
	cases := []struct {
		name   string
		link   string
		target func(root string) string
		reason string
	}{
		{name: "absolute inside repository", link: "absolute-link", target: func(root string) string { return filepath.Join(root, "quality-tools.yml") }, reason: "絶対path symlink"},
		{name: "relative escaping repository", link: "escaping-link", target: func(string) string { return filepath.Join("..", "outside") }, reason: "repository外へのsymlink"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := newScriptFixtureRepo(t)
			writeScriptFixtureModule(t, root, nil)
			if err := os.Symlink(tt.target(root), filepath.Join(root, tt.link)); err != nil {
				t.Fatal(err)
			}
			script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
			if err != nil {
				t.Fatal(err)
			}
			tempArea := t.TempDir()
			env := []string{"TMPDIR=" + tempArea}
			for _, item := range os.Environ() {
				if !strings.HasPrefix(item, "TMPDIR=") {
					env = append(env, item)
				}
			}
			command := exec.Command(script, "test", "./...")
			command.Dir = root
			command.Env = env
			outputBytes, runErr := command.CombinedOutput()
			text := string(outputBytes)
			if runErr == nil {
				t.Fatalf("診断source外symlinkが拒否されませんでした: %s", text)
			}
			if !strings.Contains(text, tt.reason) {
				t.Fatalf("拒否理由が出力にありません: %s", text)
			}
			residue, err := os.ReadDir(tempArea)
			if err != nil {
				t.Fatal(err)
			}
			if len(residue) != 0 {
				t.Fatalf("拒否run後に一時fileが残留しています: %v", residue)
			}
		})
	}
}
