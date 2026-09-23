package harnesslint

import (
	"errors"
	"strings"
	"testing"
)

type qualityPassCall struct {
	dir  string
	name string
	args []string
}

type qualityPassRunner struct {
	calls         []qualityPassCall
	failFix       bool
	failPostCheck bool
	postViolation bool
}

var errQualityPassFix = errors.New("quality fixer failed")
var errQualityPassCheck = errors.New("post-fix validation failed")

func (r *qualityPassRunner) run(dir, name string, args ...string) (commandResult, error) {
	copied := append([]string(nil), args...)
	call := qualityPassCall{dir: dir, name: name, args: copied}
	r.calls = append(r.calls, call)
	fix := qualityPassFixCall(call)
	if r.failFix && fix && strings.HasSuffix(name, "commentlint") {
		return commandResult{}, errQualityPassFix
	}
	if r.failPostCheck && !fix && strings.HasSuffix(name, "commentlint") {
		return commandResult{}, errQualityPassCheck
	}
	if r.postViolation && !fix && strings.HasSuffix(name, "commentlint") {
		return commandResult{output: `{"status":"fail","violations":[{"path":"x.go","line":1,"column":1,"kind":"comment","message":"bad"}]}`, exitCode: 1}, nil
	}
	return commandResult{}, nil
}

func TestRunFixValidatesOnceAfterFixers(t *testing.T) {
	runner := &qualityPassRunner{}
	if _, err := run(fixtureRoot(t), true, runner); err != nil {
		t.Fatal(err)
	}
	lastFix := -1
	firstCheck := -1
	commentlintChecks := 0
	for index, call := range runner.calls {
		if qualityPassFixCall(call) {
			lastFix = index
			continue
		}
		if firstCheck < 0 {
			firstCheck = index
		}
		if strings.HasSuffix(call.name, "commentlint") {
			commentlintChecks++
		}
	}
	if firstCheck < 0 || firstCheck <= lastFix {
		t.Fatalf("post-fix validation did not start after all fixers: first_check=%d last_fix=%d calls=%+v", firstCheck, lastFix, runner.calls)
	}
	if commentlintChecks != 1 {
		t.Fatalf("post-fix validation pass count = %d", commentlintChecks)
	}
}

func TestRunFixPropagatesFixerFailure(t *testing.T) {
	runner := &qualityPassRunner{failFix: true}
	if _, err := run(fixtureRoot(t), true, runner); !errors.Is(err, errQualityPassFix) {
		t.Fatalf("fixer error = %v", err)
	}
}

func TestRunFixReturnsPostFixViolationReport(t *testing.T) {
	runner := &qualityPassRunner{postViolation: true}
	report, err := run(fixtureRoot(t), true, runner)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, violation := range report.Violations {
		if violation.Rule == "commentlint/comment" && violation.Path == "x.go" {
			found = true
			break
		}
	}
	if report.Status != "fail" || !found {
		t.Fatalf("post-fix violation report = %+v", report)
	}
}

func TestRunFixPropagatesPostFixValidationFailure(t *testing.T) {
	runner := &qualityPassRunner{failPostCheck: true}
	if _, err := run(fixtureRoot(t), true, runner); !errors.Is(err, errQualityPassCheck) {
		t.Fatalf("post-fix validation error = %v", err)
	}
}

func qualityPassFixCall(call qualityPassCall) bool {
	for _, arg := range call.args {
		if arg == "--fix" || arg == "-w" {
			return true
		}
	}
	return false
}
