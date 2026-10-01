package harnesslint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type webGPTAutofixResult struct {
	BeforeSHA     string `json:"before_sha"`
	Changed       bool   `json:"changed"`
	ResultingHead string `json:"resulting_head"`
	Validation    string `json:"validation"`
	Publication   string `json:"publication"`
	Error         string `json:"error"`
}

type webGPTAutofixFixture struct {
	root     string
	control  string
	remote   string
	branch   string
	expected string
}

const fakeWebGPTAutofixHarnesslint = `#!/bin/sh
set -eu
[ -n "${HARNESSLINT_REPO_ROOT:-}" ]
cd "$HARNESSLINT_REPO_ROOT"
case "${AUTOFIX_TEST_MODE:-clean}" in
clean)
	exit 0
	;;
fixable)
	if [ "${1:-}" = "--fix" ]; then
		printf 'fixed\n' > fixture.txt
		exit 0
	fi
	grep -Fxq fixed fixture.txt
	;;
nonfixable)
	exit 1
	;;
*)
	exit 2
	;;
esac
`

func TestWebGPTAutofixCleanBranchCreatesNoCommit(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, err := fixture.run(t, "clean", fixture.branch, fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.ResultingHead != fixture.expected || result.Validation != "pass" || result.Publication != "unchanged" {
		t.Fatalf("result = %#v", result)
	}
	if got := fixture.remoteHead(t); got != fixture.expected {
		t.Fatalf("remote head changed: %s", got)
	}
}

func TestWebGPTAutofixFixableChangePublishesOneCommit(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, err := fixture.run(t, "fixable", fixture.branch, fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Validation != "pass" || result.Publication != "pushed" || result.ResultingHead == fixture.expected {
		t.Fatalf("result = %#v", result)
	}
	if got := fixture.remoteHead(t); got != result.ResultingHead {
		t.Fatalf("remote head = %s, result = %s", got, result.ResultingHead)
	}
	if parent := fixture.remoteParent(t, result.ResultingHead); parent != fixture.expected {
		t.Fatalf("autofix commit parent = %s, want %s", parent, fixture.expected)
	}
}

func TestWebGPTAutofixRejectsInvalidAndStaleTargets(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, err := fixture.run(t, "clean", "main", fixture.expected)
	if err == nil || result.Error != "invalid_target_branch" {
		t.Fatalf("invalid target result = %#v, err = %v", result, err)
	}

	writeWebGPTAutofixFile(t, fixture.root, "fixture.txt", "remote moved\n", 0o644)
	runWebGPTAutofixGit(t, fixture.root, "add", "fixture.txt")
	runWebGPTAutofixGit(t, fixture.root, "commit", "-m", "advance remote")
	runWebGPTAutofixGit(t, fixture.root, "push", "origin", "HEAD:refs/heads/"+fixture.branch)
	advanced := fixture.remoteHead(t)
	runWebGPTAutofixGit(t, fixture.root, "reset", "--hard", fixture.expected)

	result, err = fixture.run(t, "clean", fixture.branch, fixture.expected)
	if err == nil || result.Error != "stale_expected_head" {
		t.Fatalf("stale result = %#v, err = %v", result, err)
	}
	if got := fixture.remoteHead(t); got != advanced {
		t.Fatalf("stale run changed remote head: %s", got)
	}
}

func TestWebGPTAutofixLeavesNonFixableViolationUnpublished(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, err := fixture.run(t, "nonfixable", fixture.branch, fixture.expected)
	if err == nil || result.Validation != "fail" || result.Publication != "not_published" || result.Error != "lint_validation_failed" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if got := fixture.remoteHead(t); got != fixture.expected {
		t.Fatalf("non-fixable run changed remote head: %s", got)
	}
}

func TestWebGPTAutofixPublicationRaceDoesNotOverwrite(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	competitor := fixture.prepareCompetingCommit(t)
	fixture.installPrePushRace(t, competitor)
	result, err := fixture.run(t, "fixable", fixture.branch, fixture.expected)
	if err == nil || result.Publication != "rejected" || result.Error != "publication_rejected" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	if got := fixture.remoteHead(t); got != competitor {
		t.Fatalf("race overwrote competitor: got %s want %s", got, competitor)
	}
}

func newWebGPTAutofixFixture(t *testing.T) webGPTAutofixFixture {
	t.Helper()
	base := t.TempDir()
	remote := filepath.Join(base, "remote.git")
	root := filepath.Join(base, "work")
	control := filepath.Join(base, "control")
	branch := "web-gpt/autofix-test"
	runWebGPTAutofixGit(t, base, "init", "--bare", remote)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(control, 0o755); err != nil {
		t.Fatal(err)
	}
	runWebGPTAutofixGit(t, root, "init")
	runWebGPTAutofixGit(t, root, "config", "user.email", "test@example.com")
	runWebGPTAutofixGit(t, root, "config", "user.name", "tester")
	writeWebGPTAutofixFile(t, control, "web-gpt-autofix.sh", readWebGPTAutofixContractFile(t, "../../../web-gpt-autofix.sh"), 0o644)
	writeWebGPTAutofixFile(t, control, "harnesslint", fakeWebGPTAutofixHarnesslint, 0o755)
	writeWebGPTAutofixFile(t, root, "fixture.txt", "bad\n", 0o644)
	runWebGPTAutofixGit(t, root, "add", ".")
	runWebGPTAutofixGit(t, root, "commit", "-m", "base")
	runWebGPTAutofixGit(t, root, "branch", "-M", branch)
	runWebGPTAutofixGit(t, root, "remote", "add", "origin", remote)
	runWebGPTAutofixGit(t, root, "push", "-u", "origin", branch)
	return webGPTAutofixFixture{root: root, control: control, remote: remote, branch: branch, expected: runWebGPTAutofixGit(t, root, "rev-parse", "HEAD")}
}

func (fixture webGPTAutofixFixture) run(t *testing.T, mode, target, expected string) (webGPTAutofixResult, error) {
	t.Helper()
	resultPath := filepath.Join(filepath.Dir(fixture.root), "result.json")
	_ = os.Remove(resultPath)
	command := exec.Command("sh", filepath.Join(fixture.control, "web-gpt-autofix.sh"), target, expected, resultPath)
	command.Dir = fixture.root
	command.Env = append(os.Environ(), "AUTOFIX_TEST_MODE="+mode, "WEB_GPT_AUTOFIX_CONTROL_ROOT="+fixture.control)
	output, err := command.CombinedOutput()
	if _, statErr := os.Stat(resultPath); statErr != nil {
		t.Fatalf("result missing: %v; output: %s", statErr, output)
	}
	data, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	var result webGPTAutofixResult
	if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
		t.Fatalf("decode result: %v: %s", jsonErr, data)
	}
	return result, err
}

func (fixture webGPTAutofixFixture) remoteHead(t *testing.T) string {
	t.Helper()
	return runWebGPTAutofixGit(t, fixture.root, "--git-dir="+fixture.remote, "rev-parse", "refs/heads/"+fixture.branch)
}

func (fixture webGPTAutofixFixture) remoteParent(t *testing.T, sha string) string {
	t.Helper()
	return runWebGPTAutofixGit(t, fixture.root, "--git-dir="+fixture.remote, "rev-parse", sha+"^")
}

func (fixture webGPTAutofixFixture) prepareCompetingCommit(t *testing.T) string {
	t.Helper()
	writeWebGPTAutofixFile(t, fixture.root, "fixture.txt", "competitor\n", 0o644)
	runWebGPTAutofixGit(t, fixture.root, "add", "fixture.txt")
	runWebGPTAutofixGit(t, fixture.root, "commit", "-m", "competitor")
	competitor := runWebGPTAutofixGit(t, fixture.root, "rev-parse", "HEAD")
	runWebGPTAutofixGit(t, fixture.root, "push", "origin", "HEAD:refs/heads/autofix-competitor")
	runWebGPTAutofixGit(t, fixture.root, "reset", "--hard", fixture.expected)
	return competitor
}

func (fixture webGPTAutofixFixture) installPrePushRace(t *testing.T, competitor string) {
	t.Helper()
	hook := "#!/bin/sh\ngit --git-dir='" + fixture.remote + "' update-ref 'refs/heads/" + fixture.branch + "' '" + competitor + "'\n"
	writeWebGPTAutofixFile(t, fixture.root, ".git/hooks/pre-push", hook, 0o755)
}

func runWebGPTAutofixGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeWebGPTAutofixFile(t *testing.T, root, path, content string, mode os.FileMode) {
	t.Helper()
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
