package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestPrintRepoSearchReturnsMachineReadableCandidates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	t.Setenv("GLM_WORKER_HOME", t.TempDir())
	cfg := config.AppConfig{RepoRoot: newRepoSearchGitRepo(t, "clireposearch"), RepoSearch: true}
	var stdout bytes.Buffer
	if err := printRepoSearch("clireposearch unique corpus", cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output repoSearchOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "executed" || output.Result != "hit" || output.Query != "clireposearch unique corpus" {
		t.Fatalf("output = %#v", output)
	}
	if output.ResultCount != len(output.Results) || output.ResultCount == 0 {
		t.Fatalf("result_count = %d results = %#v", output.ResultCount, output.Results)
	}
	if output.Results[0].Path != "corpus.md" || output.Results[0].Line <= 0 {
		t.Fatalf("results = %#v", output.Results)
	}
	if output.CacheStatus == "" || output.IndexedFiles <= 0 {
		t.Fatalf("report metadata missing: %#v", output)
	}
}

func TestPrintRepoSearchDisabledReportsDisabledWithoutSearch(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: filepath.Join(t.TempDir(), "missing"), RepoSearch: false}
	var stdout bytes.Buffer
	if err := printRepoSearch("anything", cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output repoSearchOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "disabled" || output.Result != "disabled" || output.ResultCount != 0 || len(output.Results) != 0 {
		t.Fatalf("output = %#v", output)
	}
}

func newRepoSearchGitRepo(t *testing.T, marker string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "reposearch@example.invalid")
	run("config", "user.name", "repo search test")
	document := fmt.Sprintf("%s unique corpus\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "corpus.md"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "corpus.md")
	run("commit", "-q", "-m", "initial")
	return dir
}
