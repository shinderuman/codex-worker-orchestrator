package goqualitycmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestGoqualityScriptDetectsRawSourceChangeWithPresentationDriver(t *testing.T) {
	for _, mode := range []string{"environment-external", "repository-external", "textconv"} {
		t.Run(mode, func(t *testing.T) {
			root := newScriptFixtureRepo(t)
			writeScriptFixtureModule(t, root, map[string]string{"source.txt": "baseline bytes\n"})
			bin := t.TempDir()
			marker := filepath.Join(t.TempDir(), "driver-called")
			driver := filepath.Join(bin, "driver")
			if err := execfile.Write(driver, []byte("#!/bin/sh\nprintf called > '"+marker+"'\nprintf 'constant presentation\\n'\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".git/info/attributes"), []byte("source.txt diff=raw-audit\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "environment-external":
				t.Setenv("GIT_EXTERNAL_DIFF", driver)
			case "repository-external":
				runScriptFixtureGit(t, root, "config", "diff.raw-audit.command", driver)
			case "textconv":
				runScriptFixtureGit(t, root, "config", "diff.raw-audit.textconv", driver)
			}
			version := strings.TrimPrefix(runtime.Version(), "go")
			fakeGo := "#!/bin/sh\ncase \"$*\" in\n" +
				"'env GOVERSION') printf 'go" + version + "\\n' ;;\n" +
				"'env GOCACHE') printf '/fixture/cache\\n' ;;\n" +
				"'env GOOS') printf 'linux\\n' ;;\n" +
				"'env GOARCH') printf 'arm64\\n' ;;\n" +
				"*' --backend '*) printf 'native\\n' ;;\n" +
				"*' test '*) printf 'changed raw bytes\\n' > '" + filepath.Join(root, "source.txt") + "' ;;\n" +
				"*) exit 97 ;;\nesac\n"
			if err := execfile.Write(filepath.Join(bin, "go"), []byte(fakeGo), 0o700); err != nil {
				t.Fatal(err)
			}
			script, err := filepath.Abs(filepath.Join("..", "..", "..", "goquality"))
			if err != nil {
				t.Fatal(err)
			}
			command := exec.Command(script, "test", "./...")
			command.Dir = root
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
			output, runErr := command.CombinedOutput()
			if runErr == nil {
				t.Fatalf("source変更が成功扱いされました: %s", output)
			}
			var diagnostic struct {
				SourceDigest  string `json:"source_digest"`
				SourceChanged bool   `json:"source_changed"`
				ChildExit     int    `json:"child_exit"`
				Exit          int    `json:"exit"`
			}
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				if payload, ok := strings.CutPrefix(line, "goquality-diagnostic "); ok {
					if err := json.Unmarshal([]byte(payload), &diagnostic); err != nil {
						t.Fatal(err)
					}
					found = true
				}
			}
			if !found || diagnostic.SourceDigest == "" || !diagnostic.SourceChanged || diagnostic.ChildExit != 0 || diagnostic.Exit != 1 {
				t.Fatalf("raw source変更の診断がありません: %+v output=%s", diagnostic, output)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("source snapshotがdriverを起動しました: %v", err)
			}
			control := exec.Command("git", "-C", root, "diff", "--ext-diff", "--textconv", "HEAD", "--", "source.txt")
			if output, err := control.CombinedOutput(); err != nil || (mode == "textconv" && len(output) != 0) {
				t.Fatalf("表示用driver対照 = %q error=%v", output, err)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("対照diffがdriverを起動していません: %v", err)
			}
		})
	}
}
