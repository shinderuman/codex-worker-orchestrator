package harnesslint

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

type webGPTValidationResult struct {
	TestedBranch    string `json:"tested_branch"`
	TestedSHA       string `json:"tested_sha"`
	ControlSHA      string `json:"control_sha"`
	Mode            string `json:"mode"`
	Result          string `json:"result"`
	Scope           string `json:"scope"`
	ArtifactLocator string `json:"artifact_locator"`
	Error           string `json:"error"`
}

type webGPTValidationFixture struct {
	base       string
	target     string
	control    string
	remote     string
	bin        string
	log        string
	branch     string
	targetSHA  string
	controlSHA string
}

const fakeFocusedHarnesslint = `#!/bin/sh
set -eu
printf 'harnesslint %s\n' "$*" >> "$VALIDATION_LOG"
[ "${1:-}" = "--controlled-check" ]
[ "${HARNESSLINT_REPO_ROOT:-}" = "$EXPECTED_TARGET_ROOT" ]
[ "${HARNESSLINT_CONTROL_ROOT:-}" = "$EXPECTED_CONTROL_ROOT" ]
`

const fakeFocusedGo = `#!/bin/sh
set -eu
printf 'go %s\n' "$*" >> "$VALIDATION_LOG"
case "${VALIDATION_MUTATION:-}" in
	vet-fail)
		case " $*" in
			*" vet ./..."*) exit 47 ;;
		esac
		;;
	target-dirty)
		printf 'mutated\n' >> "$EXPECTED_TARGET_ROOT/fixture.txt"
		;;
	control-dirty)
		printf 'mutated\n' >> "$EXPECTED_CONTROL_ROOT/quality-tools.yml"
		;;
	target-head)
		git -C "$EXPECTED_TARGET_ROOT" commit --allow-empty -m post-validation-target-mutation >/dev/null
		;;
	control-head)
		git -C "$EXPECTED_CONTROL_ROOT" commit --allow-empty -m post-validation-control-mutation >/dev/null
		;;
esac
`

const fakeFocusedAwk = `#!/bin/sh
set -eu
"$VALIDATION_REAL_AWK" "$@"
if [ "${VALIDATION_MUTATION:-}" = control-disappears-before-cd ] && [ "${1:-}" = '-F: ' ]; then
	printf 'control-root-removed-before-cd\n' >> "$VALIDATION_LOG"
	mv "$EXPECTED_CONTROL_ROOT" "$EXPECTED_CONTROL_ROOT.removed"
fi
`

func TestWebGPTValidationRunsBoundedModes(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	cases := []struct {
		mode  string
		scope string
		want  []string
	}{
		{mode: "repository-lint", want: []string{"harnesslint --controlled-check"}},
		{mode: "go-package-test", scope: "./internal/workflow", want: []string{"go -C " + fixture.target + "/glm-worker test ./internal/workflow"}},
		{mode: "full-go-test", want: []string{"go -C " + fixture.target + "/glm-worker test ./..."}},
		{mode: "build-vet", want: []string{"go -C " + fixture.target + "/glm-worker vet ./...", "go -C " + fixture.target + "/glm-worker build ./..."}},
	}
	for _, item := range cases {
		t.Run(item.mode, func(t *testing.T) {
			if err := os.WriteFile(fixture.log, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			result, output, err := fixture.run(t, item.mode, item.scope, fixture.targetSHA)
			if err != nil {
				t.Fatalf("validation failed: %v: %s", err, output)
			}
			if result.Result != "pass" || result.TestedBranch != fixture.branch || result.TestedSHA != fixture.targetSHA || result.ControlSHA != fixture.controlSHA || result.Mode != item.mode || result.Scope != item.scope || result.ArtifactLocator != "web-gpt-validation-result-1" || result.Error != "" {
				t.Fatalf("result = %#v", result)
			}
			data, readErr := os.ReadFile(fixture.log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			logText := strings.TrimSpace(string(data))
			for _, want := range item.want {
				if !strings.Contains(logText, want) {
					t.Fatalf("validation log %q missing %q", logText, want)
				}
			}
		})
	}
}

func TestWebGPTValidationVetFailureDoesNotProceedToBuild(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	result, output, err := fixture.runWithMutation(t, "build-vet", "", fixture.targetSHA, "vet-fail")
	if err == nil || result.Result != "fail" || result.Error != "validation_failed" {
		t.Fatalf("vet error did not fail validation: result=%#v err=%v output=%s", result, err, output)
	}
	log, readErr := os.ReadFile(fixture.log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(log), " vet ./...") || strings.Contains(string(log), " build ./...") {
		t.Fatalf("vet failure did not stop before build: %s", log)
	}
}

func TestWebGPTValidationRejectsMissingControlRootWithoutRunningGate(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	result, output, err := fixture.runWithMutation(t, "repository-lint", "", fixture.targetSHA, "missing-control-root")
	if err == nil || result.Result != "rejected" || result.Error != "invalid_control_root" {
		t.Fatalf("missing control root was admitted: result=%#v err=%v output=%s", result, err, output)
	}
	log, readErr := os.ReadFile(fixture.log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(log) != 0 {
		t.Fatalf("control root rejection executed a gate: %s", log)
	}
}

func TestWebGPTValidationControlRootDisappearsBeforeGateChangeDirectory(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	result, output, err := fixture.runWithMutation(t, "repository-lint", "", fixture.targetSHA, "control-disappears-before-cd")
	if err == nil || result.Result != "fail" || result.Error != "post_validation_control_head_unavailable" {
		t.Fatalf("disappeared control root did not fail closed: result=%#v err=%v output=%s", result, err, output)
	}
	log, err := os.ReadFile(fixture.log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), "control-root-removed-before-cd") {
		t.Fatalf("control root disappearance was not reached: %s", log)
	}
	if strings.Contains(string(log), "harnesslint") {
		t.Fatalf("failed control directory change executed a gate: %s", log)
	}
}

func TestWebGPTValidationRejectsUnsafeScopeBeforeExecution(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	if err := os.WriteFile(fixture.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	result, _, err := fixture.run(t, "go-package-test", "./../control;touch-pwn", fixture.targetSHA)
	if err == nil || result.Result != "rejected" || result.Error != "invalid_package_scope" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
	data, readErr := os.ReadFile(fixture.log)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(data) != 0 {
		t.Fatalf("unsafe scope executed a command: %s", data)
	}
}

func TestWebGPTValidationRejectsStaleExpectedHead(t *testing.T) {
	fixture := newWebGPTValidationFixture(t)
	writeFocusedValidationFile(t, fixture.target, "advance.txt", "advance\n", 0o644)
	runFocusedValidationGit(t, fixture.target, "add", "advance.txt")
	runFocusedValidationGit(t, fixture.target, "commit", "-m", "advance")
	runFocusedValidationGit(t, fixture.target, "push", "origin", "HEAD:refs/heads/"+fixture.branch)
	runFocusedValidationGit(t, fixture.target, "reset", "--hard", fixture.targetSHA)
	result, _, err := fixture.run(t, "full-go-test", "", fixture.targetSHA)
	if err == nil || result.Result != "rejected" || result.Error != "stale_expected_head" {
		t.Fatalf("result = %#v, err = %v", result, err)
	}
}

func TestWebGPTValidationFailsClosedOnPostExecutionMutation(t *testing.T) {
	cases := []struct {
		name     string
		mutation string
		wantErr  string
	}{
		{name: "target tracked content", mutation: "target-dirty", wantErr: "post_validation_target_dirty"},
		{name: "target head", mutation: "target-head", wantErr: "post_validation_target_head_changed"},
		{name: "control tracked content", mutation: "control-dirty", wantErr: "post_validation_control_dirty"},
		{name: "control head", mutation: "control-head", wantErr: "post_validation_control_head_changed"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			fixture := newWebGPTValidationFixture(t)
			result, output, err := fixture.runWithMutation(t, "full-go-test", "", fixture.targetSHA, item.mutation)
			if err == nil {
				t.Fatalf("validation unexpectedly passed: %#v: %s", result, output)
			}
			if result.Result != "fail" || result.Error != item.wantErr {
				t.Fatalf("result = %#v, want fail/%s: %s", result, item.wantErr, output)
			}
		})
	}
}

func newWebGPTValidationFixture(t *testing.T) webGPTValidationFixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	control := filepath.Join(base, "control")
	remote := filepath.Join(base, "remote.git")
	bin := filepath.Join(base, "bin")
	logPath := filepath.Join(base, "validation.log")
	branch := "web-gpt/validation-test"
	for _, path := range []string{target, control, bin} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	runFocusedValidationGit(t, base, "init", "--bare", remote)
	initializeFocusedValidationRepo(t, target)
	writeFocusedValidationFile(t, target, "fixture.txt", "target\n", 0o644)
	writeFocusedValidationFile(t, target, "glm-worker/go.mod", "module example.invalid/target\n\ngo 1.25.0\n", 0o644)
	writeFocusedValidationFile(t, target, "harnesslint", "#!/bin/sh\nprintf 'target-harnesslint-executed\\n' >> \"$VALIDATION_LOG\"\n", 0o755)
	runFocusedValidationGit(t, target, "add", ".")
	runFocusedValidationGit(t, target, "commit", "-m", "target")
	runFocusedValidationGit(t, target, "branch", "-M", branch)
	runFocusedValidationGit(t, target, "remote", "add", "origin", remote)
	runFocusedValidationGit(t, target, "push", "-u", "origin", branch)
	targetSHA := runFocusedValidationGit(t, target, "rev-parse", "HEAD")

	initializeFocusedValidationRepo(t, control)
	writeFocusedValidationFile(t, control, "quality-tools.yml", "go: 1.25.4\n", 0o644)
	writeFocusedValidationFile(t, control, "harnesslint", fakeFocusedHarnesslint, 0o755)
	writeFocusedValidationFile(t, control, "web-gpt-validate.sh", readWebGPTAutofixContractFile(t, "../../../web-gpt-validate.sh"), 0o755)
	runFocusedValidationGit(t, control, "add", ".")
	runFocusedValidationGit(t, control, "commit", "-m", "control")
	controlSHA := runFocusedValidationGit(t, control, "rev-parse", "HEAD")

	writeFocusedValidationFile(t, bin, "go", fakeFocusedGo, 0o755)
	writeFocusedValidationFile(t, bin, "awk", fakeFocusedAwk, 0o755)
	if err := os.WriteFile(logPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return webGPTValidationFixture{
		base: base, target: target, control: control, remote: remote, bin: bin, log: logPath,
		branch: branch, targetSHA: targetSHA, controlSHA: controlSHA,
	}
}

func initializeFocusedValidationRepo(t *testing.T, root string) {
	t.Helper()
	runFocusedValidationGit(t, root, "init")
	runFocusedValidationGit(t, root, "config", "user.email", "test@example.com")
	runFocusedValidationGit(t, root, "config", "user.name", "tester")
}

func (fixture webGPTValidationFixture) run(t *testing.T, mode, scope, expected string) (webGPTValidationResult, []byte, error) {
	t.Helper()
	return fixture.runWithMutation(t, mode, scope, expected, "")
}

func (fixture webGPTValidationFixture) runWithMutation(t *testing.T, mode, scope, expected, mutation string) (webGPTValidationResult, []byte, error) {
	t.Helper()
	realAwk, err := exec.LookPath("awk")
	if err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(fixture.base, "result-"+strings.ReplaceAll(mode, "/", "-")+".json")
	_ = os.Remove(resultPath)
	command := exec.Command("sh", filepath.Join(fixture.control, "web-gpt-validate.sh"), fixture.branch, expected, fixture.controlSHA, mode, scope, resultPath, "web-gpt-validation-result-1")
	command.Dir = fixture.target
	controlRoot := fixture.control
	if mutation == "missing-control-root" {
		controlRoot = filepath.Join(fixture.base, "absent-control-root")
	}
	command.Env = append(os.Environ(),
		"PATH="+fixture.bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"WEB_GPT_VALIDATION_CONTROL_ROOT="+controlRoot,
		"VALIDATION_LOG="+fixture.log,
		"VALIDATION_MUTATION="+mutation,
		"VALIDATION_REAL_AWK="+realAwk,
		"EXPECTED_TARGET_ROOT="+fixture.target,
		"EXPECTED_CONTROL_ROOT="+fixture.control,
	)
	output, err := command.CombinedOutput()
	data, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatalf("validation result missing: %v: %s", readErr, output)
	}
	var fields map[string]json.RawMessage
	if jsonErr := json.Unmarshal(data, &fields); jsonErr != nil {
		t.Fatalf("decode validation result fields: %v: %s", jsonErr, data)
	}
	for _, key := range []string{"tested_branch", "tested_sha", "control_sha", "mode", "result", "scope", "artifact_locator", "error"} {
		value, ok := fields[key]
		if !ok || string(value) == "null" {
			t.Fatalf("validation result is missing required string %q: %s", key, data)
		}
	}
	var result webGPTValidationResult
	if jsonErr := json.Unmarshal(data, &result); jsonErr != nil {
		t.Fatalf("decode validation result: %v: %s", jsonErr, data)
	}
	return result, output, err
}

func runFocusedValidationGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeFocusedValidationFile(t *testing.T, root, path, content string, mode os.FileMode) {
	t.Helper()
	absolute := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := execfile.Write(absolute, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
