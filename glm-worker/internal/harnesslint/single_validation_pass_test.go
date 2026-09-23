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
	failPostCheck bool
}

var errQualityPassCheck = errors.New("post-fix validation failed")

func (r *qualityPassRunner) run(dir, name string, args ...string) (commandResult, error) {
	copied := append([]string(nil), args...)
	r.calls = append(r.calls, qualityPassCall{dir: dir, name: name, args: copied})
	if r.failPostCheck && strings.HasSuffix(name, "commentlint") && !qualityPassFixCall(r.calls[len(r.calls)-1]) {
		return commandResult{}, errQualityPassCheck
	}
	return commandResult{}, nil
}

func TestRunFixValidatesExternalCommandsOnceAfterFixers(t *testing.T) {
	root := fixtureRoot(t)
	runner := &qualityPassRunner{}
	if _, err := run(root, true, runner); err != nil {
		t.Fatal(err)
	}
	lastFix := -1
	firstCheck := -1
	checks := map[string]int{}
	for index, call := range runner.calls {
		if qualityPassFixCall(call) {
			lastFix = index
			continue
		}
		if firstCheck < 0 {
			firstCheck = index
		}
		key := call.dir + "\x00" + call.name + "\x00" + strings.Join(call.args, "\x00")
		checks[key]++
	}
	if firstCheck < 0 || firstCheck <= lastFix {
		t.Fatalf("post-fix validation did not start after all fixers: first_check=%d last_fix=%d calls=%+v", firstCheck, lastFix, runner.calls)
	}
	for key, count := range checks {
		if count != 1 {
			t.Fatalf("external validation command %q ran %d times", key, count)
		}
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
