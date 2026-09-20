package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteReopenUsesCanonicalAdmissionAndReturnsNextAction(t *testing.T) {
	fixture := newCompleteFixture(t)
	var stdout bytes.Buffer
	if err := execute(fixture.cfg, []string{
		"reopen",
		"--origin", state.ParentOriginCodexReview,
		"--cause", state.ParentCauseProductionWiring,
	}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output reopenOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("reopen output is not JSON: %v: %s", err, stdout.String())
	}
	if output.Status != "reopened" || output.TaskStatus != string(state.TaskStatusWaitingSolReview) || output.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("reopen output = %#v", output)
	}
	if !stringSliceContains(output.AllowedActions, string(state.ParentActionFix)) || stringSliceContains(output.AllowedActions, string(state.ParentActionComplete)) {
		t.Fatalf("reopen allowed actions = %#v", output.AllowedActions)
	}
}

func TestExecuteReopenRejectsNonAwaitingStateAndNonCanonicalOptions(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := fixture.st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := execute(fixture.cfg, []string{"reopen"}, &bytes.Buffer{}, io.Discard); err == nil || !strings.Contains(err.Error(), "not admitted") {
		t.Fatalf("reopen outside awaiting completion = %v", err)
	}

	fixture = newCompleteFixture(t)
	if err := execute(fixture.cfg, []string{"reopen", "--accepted-scope", "current-diff"}, &bytes.Buffer{}, io.Discard); err == nil || !strings.Contains(err.Error(), reopenUsage) {
		t.Fatalf("reopen accepted non-canonical option = %v", err)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
