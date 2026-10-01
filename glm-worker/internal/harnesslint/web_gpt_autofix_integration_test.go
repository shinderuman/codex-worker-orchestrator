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
	base     string
	root     string
	control  string
	remote   string
	branch   string
	expected string
}

const fakeWebGPTAutofixHarnesslint = `#!/bin/sh
set -eu
[ -n "${HARNESSLINT_REPO_ROOT:-}" ]
[ -n "${HARNESSLINT_CONTROL_ROOT:-}" ]
cd "$HARNESSLINT_REPO_ROOT"
mode=${AUTOFIX_TEST_MODE:-clean}
case "${1:-}" in
--deterministic-fix)
	case "$mode" in
	clean|nonfixable)
		exit 0
		;;
	fixable|mutatingcheck)
		printf 'fixed\n' > fixture.txt
		exit 0
		;;
	*)
		exit 2
		;;
	esac
	;;
--controlled-check)
	case "$mode" in
	clean)
		exit 0
		;;
	fixable)
		grep -Fxq fixed fixture.txt
		;;
	nonfixable)
		exit 1
		;;
	mutatingcheck)
		printf 'validation-mutated\n' > fixture.txt
		exit 0
		;;
	*)
		exit 2
		;;
	esac
	;;
*)
	exit 2
	;;
esac
`

const hostileTargetCommentlint = `#!/bin/sh
printf 'executed\n' > target-code-executed
exit 99
`

func TestWebGPTAutofixCleanBranchCreatesNoCommit(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, patch, err := fixture.prepare(t, "clean", fixture.branch, fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.ResultingHead != fixture.expected || result.Validation != "pass" || result.Publication != "unchanged" {
		t.Fatalf("result = %#v", result)
	}
	if info, statErr := os.Stat(patch); statErr != nil || info.Size() != 0 {
		t.Fatalf("clean patch = %v, %v", info, statErr)
	}
	fixture.requireTargetCodeNotExecuted(t)
	fixture.requireRemoteHead(t, fixture.expected)
}

func TestWebGPTAutofixFixableChangePublishesOneCommit(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	prepared, patch, err := fixture.prepare(t, "fixable", fixture.branch, fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	if !prepared.Changed || prepared.Validation != "pass" || prepared.Publication != "prepared" {
		t.Fatalf("prepared result = %#v", prepared)
	}
	fixture.requireTargetCodeNotExecuted(t)
	fixture.requireRemoteHead(t, fixture.expected)
	published, err := fixture.publish(t, fixture.branch, fixture.expected, patch)
	if err != nil {
		t.Fatal(err)
	}
	if !published.Changed || published.Validation != "pass" || published.Publication != "pushed" || published.ResultingHead == fixture.expected {
		t.Fatalf("published result = %#v", published)
	}
	fixture.requireRemoteHead(t, published.ResultingHead)
	if parent := fixture.remoteParent(t, published.ResultingHead); parent != fixture.expected {
		t.Fatalf("autofix commit parent = %s, want %s", parent, fixture.expected)
	}
}

func TestWebGPTAutofixRejectsInvalidAndStaleTargets(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, _, err := fixture.prepare(t, "clean", "main", fixture.expected)
	if err == nil || result.Error != "invalid_target_branch" {
		t.Fatalf("invalid target result = %#v, err = %v", result, err)
	}

	writeWebGPTAutofixFile(t, fixture.root, "fixture.txt", "remote moved\n", 0o644)
	runWebGPTAutofixGit(t, fixture.root, "add", "fixture.txt")
	runWebGPTAutofixGit(t, fixture.root, "commit", "-m", "advance remote")
	runWebGPTAutofixGit(t, fixture.root, "push", "origin", "HEAD:refs/heads/"+fixture.branch)
	advanced := fixture.remoteHead(t)
	runWebGPTAutofixGit(t, fixture.root, "reset", "--hard", fixture.expected)

	result, _, err = fixture.prepare(t, "clean", fixture.branch, fixture.expected)
	if err == nil || result.Error != "stale_expected_head" {
		t.Fatalf("stale result = %#v, err = %v", result, err)
	}
	fixture.requireRemoteHead(t, advanced)
}

func TestWebGPTAutofixLeavesNonFixableViolationUnpublished(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, _, err := fixture.prepare(t, "nonfixable", fixture.branch, fixture.expected)
	if err == nil || result.Validation != "fail" || result.Publication != "not_published" || result.Error != "lint_validation_failed" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	fixture.requireRemoteHead(t, fixture.expected)
}

func TestWebGPTAutofixRejectsValidationMutation(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	result, _, err := fixture.prepare(t, "mutatingcheck", fixture.branch, fixture.expected)
	if err == nil || result.Validation != "fail" || result.Publication != "not_published" || result.Error != "validation_mutated_target" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	fixture.requireRemoteHead(t, fixture.expected)
}

func TestWebGPTAutofixPublicationRaceDoesNotOverwrite(t *testing.T) {
	fixture := newWebGPTAutofixFixture(t)
	_, patch, err := fixture.prepare(t, "fixable", fixture.branch, fixture.expected)
	if err != nil {
		t.Fatal(err)
	}
	competitor := fixture.prepareCompetingCommit(t)
	fixture.resetForPublish(t)
	fixture.installPrePushRace(t, competitor)
	result, err := fixture.publishCurrent(t, fixture.branch, fixture.expected, patch)
	if err == nil || result.Publication != "rejected" || result.Error != "publication_rejected" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	fixture.requireRemoteHead(t, competitor)
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
	writeWebGPTAutofixFile(t, root, "commentlint", hostileTargetCommentlint, 0o755)
	runWebGPTAutofixGit(t, root, "add", ".")
	runWebGPTAutofixGit(t, root, "commit", "-m", "base")
	runWebGPTAutofixGit(t, root, "branch", "-M", branch)
	runWebGPTAutofixGit(t, root, "remote", "add", "origin", remote)
	runWebGPTAutofixGit(t, root, "push", "-u", "origin", branch)
	return webGPTAutofixFixture{
		base: base, root: root, control: control, remote: remote, branch: branch,
		expected: runWebGPTAutofixGit(t, root, "rev-parse", "HEAD"),
	}
}

func (fixture webGPTAutofixFixture) prepare(t *testing.T, mode, target, expected string) (webGPTAutofixResult, string, error) {
	t.Helper()
	patchPath := filepath.Join(fixture.base, "autofix.patch")
	resultPath := filepath.Join(fixture.base, "prepare-result.json")
	_ = os.Remove(patchPath)
	_ = os.Remove(resultPath)
	command := exec.Command("sh", filepath.Join(fixture.control, "web-gpt-autofix.sh"), "prepare", target, expected, patchPath, resultPath)
	command.Dir = fixture.root
	command.Env = append(os.Environ(), "AUTOFIX_TEST_MODE="+mode, "WEB_GPT_AUTOFIX_CONTROL_ROOT="+fixture.control)
	output, err := command.CombinedOutput()
	return readWebGPTAutofixResult(t, resultPath, output), patchPath, err
}

func (fixture webGPTAutofixFixture) publish(t *testing.T, target, expected, patchPath string) (webGPTAutofixResult, error) {
	t.Helper()
	fixture.resetForPublish(t)
	return fixture.publishCurrent(t, target, expected, patchPath)
}

func (fixture webGPTAutofixFixture) publishCurrent(t *testing.T, target, expected, patchPath string) (webGPTAutofixResult, error) {
	t.Helper()
	resultPath := filepath.Join(fixture.base, "publish-result.json")
	_ = os.Remove(resultPath)
	command := exec.Command("sh", filepath.Join(fixture.control, "web-gpt-autofix.sh"), "publish", target, expected, patchPath, resultPath)
	command.Dir = fixture.root
	output, err := command.CombinedOutput()
	return readWebGPTAutofixResult(t, resultPath, output), err
}

func readWebGPTAutofixResult(t *testing.T, resultPath string, output []byte) webGPTAutofixResult {
	t.Helper()
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatalf("result missing: %v; output: %s", err, output)
	}
	var result webGPTAutofixResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decode result: %v: %s", err, data)
	}
	return result
}

func (fixture webGPTAutofixFixture) resetForPublish(t *testing.T) {
	t.Helper()
	runWebGPTAutofixGit(t, fixture.root, "reset", "--hard", fixture.expected)
	runWebGPTAutofixGit(t, fixture.root, "clean", "-fd")
}

func (fixture webGPTAutofixFixture) requireTargetCodeNotExecuted(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(fixture.root, "target-code-executed")); !os.IsNotExist(err) {
		t.Fatalf("target commentlint executed: %v", err)
	}
}

func (fixture webGPTAutofixFixture) requireRemoteHead(t *testing.T, want string) {
	t.Helper()
	if got := fixture.remoteHead(t); got != want {
		t.Fatalf("remote head = %s, want %s", got, want)
	}
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
	fixture.resetForPublish(t)
	writeWebGPTAutofixFile(t, fixture.root, "fixture.txt", "competitor\n", 0o644)
	runWebGPTAutofixGit(t, fixture.root, "add", "fixture.txt")
	runWebGPTAutofixGit(t, fixture.root, "commit", "-m", "competitor")
	competitor := runWebGPTAutofixGit(t, fixture.root, "rev-parse", "HEAD")
	runWebGPTAutofixGit(t, fixture.root, "push", "origin", "HEAD:refs/heads/autofix-competitor")
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
