package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func recordFixturePublicationFinding(t *testing.T, fixture *completeFixture) state.PublicationCandidate {
	t.Helper()
	ensureCompleteFixturePublicationAuthority(t, fixture)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.RecordPublicationInvalidatingFinding(
		candidate.CommitOID,
		candidate.SnapshotID,
		state.ParentOriginCodexReview,
		state.ParentCauseProductionWiring,
	); err != nil {
		t.Fatal(err)
	}
	return candidate
}

func TestExecuteReopenUsesMachineRequiredAdmissionAndReturnsNextAction(t *testing.T) {
	fixture := newCompleteFixture(t)
	recordFixturePublicationFinding(t, fixture)

	var stdout bytes.Buffer
	if err := execute(fixture.cfg, []string{"reopen"}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output reopenOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("reopen output is not JSON: %v: %s", err, stdout.String())
	}
	if output.Status != "reopened" || output.TaskStatus != string(state.TaskStatusWaitingSolReview) || output.RequiredAction != string(state.ParentActionReview) {
		t.Fatalf("reopen output = %#v", output)
	}
	if !stringSliceContains(output.AllowedActions, string(state.ParentActionFix)) || stringSliceContains(output.AllowedActions, string(state.ParentActionComplete)) || stringSliceContains(output.AllowedActions, string(state.ParentActionReopen)) {
		t.Fatalf("reopen allowed actions = %#v", output.AllowedActions)
	}
}

func TestExecuteReopenRejectsWithoutFindingAndRejectsOptions(t *testing.T) {
	fixture := newCompleteFixture(t)
	ensureCompleteFixturePublicationAuthority(t, fixture)
	if err := execute(fixture.cfg, []string{"reopen"}, &bytes.Buffer{}, io.Discard); err == nil || !strings.Contains(err.Error(), "not machine-required") {
		t.Fatalf("reopen without finding = %v", err)
	}

	fixture = newCompleteFixture(t)
	recordFixturePublicationFinding(t, fixture)
	if err := execute(fixture.cfg, []string{"reopen", "--origin", state.ParentOriginCodexReview}, &bytes.Buffer{}, io.Discard); err == nil || !strings.Contains(err.Error(), reopenUsage) {
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
