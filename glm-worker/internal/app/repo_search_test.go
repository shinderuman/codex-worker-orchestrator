package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPrintRepoSearchReturnsMachineReadableCandidates(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	home := t.TempDir()
	t.Setenv("GLM_WORKER_HOME", home)
	cfg := config.AppConfig{RepoRoot: newRepoSearchGitRepo(t, "clireposearch"), RepoSearch: true}
	var stdout bytes.Buffer
	request := repoSearchRequest{
		Question:    "clireposearch unique corpus",
		Scopes:      []string{"corpus.md"},
		BudgetBytes: 4096,
	}
	if err := printRepoSearch(request, cfg, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	var output repoSearchOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "executed" || output.Result != "hit" || output.Question != "clireposearch unique corpus" {
		t.Fatalf("output = %#v", output)
	}
	if output.ResultCount != len(output.Results) || output.ResultCount == 0 {
		t.Fatalf("result_count = %d results = %#v", output.ResultCount, output.Results)
	}
	if output.Results[0].Path != "corpus.md" || output.Results[0].Line <= 0 {
		t.Fatalf("results = %#v", output.Results)
	}
	if output.Candidates < output.ResultCount {
		t.Fatalf("candidates = %d below result_count = %d", output.Candidates, output.ResultCount)
	}
	if output.CacheStatus == "" || output.IndexedFiles <= 0 {
		t.Fatalf("report metadata missing: %#v", output)
	}
	if _, err := os.Stat(filepath.Join(home, "search")); !os.IsNotExist(err) {
		t.Fatalf("read-only repo search created cache state: %v", err)
	}
}

func TestPrintRepoSearchDisabledReportsDisabledWithoutSearch(t *testing.T) {
	cfg := config.AppConfig{RepoRoot: filepath.Join(t.TempDir(), "missing"), RepoSearch: false}
	var stdout bytes.Buffer
	request := repoSearchRequest{Question: "anything", Scopes: []string{"cmd"}, BudgetBytes: 512}
	if err := printRepoSearch(request, cfg, nil, &stdout); err != nil {
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

func TestPrintRepoSearchRequiresBudgetAndScopesAtParseLevel(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "bare query", args: []string{"--repo-search", "find lifecycle transitions"}},
		{name: "missing budget", args: []string{"--repo-search", "find lifecycle transitions", "--scope", "cmd"}},
		{name: "missing scope", args: []string{"--repo-search", "find lifecycle transitions", "--budget", "1024"}},
		{name: "zero budget", args: []string{"--repo-search", "q", "--scope", "cmd", "--budget", "0"}},
		{name: "over-limit budget", args: []string{"--repo-search", "q", "--scope", "cmd", "--budget", "1048576"}},
		{name: "unknown flag", args: []string{"--repo-search", "q", "--scope", "cmd", "--budget", "512", "--limit", "5"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseCommand(test.args); err == nil {
				t.Fatalf("ParseCommand(%v) succeeded, want usage error", test.args)
			}
		})
	}
	command, err := ParseCommand([]string{"--repo-search", "where is the stop endpoint handled", "--scope", "internal/app", "--scope", "symbol:requestStop", "--budget", "2048"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeRepoSearch || command.Payload != "where is the stop endpoint handled" {
		t.Fatalf("command = %#v", command)
	}
	if len(command.SearchScopes) != 2 || command.SearchBudgetBytes != 2048 {
		t.Fatalf("command = %#v", command)
	}
}

func TestPrintRepoSearchReturnsRefinementRequiredInsteadOfTruncation(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	t.Setenv("GLM_WORKER_HOME", t.TempDir())
	cfg := config.AppConfig{RepoRoot: newRepoSearchGitRepo(t, "refinementcorpus"), RepoSearch: true}
	var stdout bytes.Buffer
	request := repoSearchRequest{
		Question:    "refinementcorpus unique corpus",
		Scopes:      []string{"corpus.md"},
		BudgetBytes: 8,
	}
	if err := printRepoSearch(request, cfg, nil, &stdout); err != nil {
		t.Fatal(err)
	}
	var output repoSearchOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "refinement_required" || output.Result != "refinement_required" {
		t.Fatalf("output = %#v", output)
	}
	if output.Reason == "" || len(output.Results) != 0 || output.ResultCount != 1 {
		t.Fatalf("refinement output carries results or loses counts: %#v", output)
	}
}

func TestPrintRepoSearchDuplicateWithinDecisionLeaseIsRejected(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	t.Setenv("GLM_WORKER_HOME", t.TempDir())
	repoRoot := newRepoSearchGitRepo(t, "leasecorpus")
	cfg := config.AppConfig{RepoRoot: repoRoot, RepoSearch: true}
	st, storeErr := state.NewStateStore(config.AppConfig{
		StateBase: filepath.Join(t.TempDir(), "state"),
		RepoHash:  "leasesearch",
		RepoRoot:  repoRoot,
	})
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	request := repoSearchRequest{Question: "leasecorpus unique corpus", Scopes: []string{"corpus.md"}, BudgetBytes: 4096}

	var first bytes.Buffer
	if err := printRepoSearch(request, cfg, st, &first); err != nil {
		t.Fatal(err)
	}
	var firstOutput repoSearchOutput
	if err := json.Unmarshal(first.Bytes(), &firstOutput); err != nil || firstOutput.Status != "executed" {
		t.Fatalf("first projection = %s err = %v", first.String(), err)
	}

	var second bytes.Buffer
	err := printRepoSearch(request, cfg, st, &second)
	if err == nil {
		t.Fatalf("second identical projection succeeded: %s", second.String())
	}
	var duplicate *DuplicateParentProjectionError
	if !errors.As(err, &duplicate) {
		t.Fatalf("second projection error = %T, want DuplicateParentProjectionError", err)
	}
	if duplicate.Surface != state.ParentEvidenceSurfaceSearch {
		t.Fatalf("duplicate surface = %q", duplicate.Surface)
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
