package parentactioncmd

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteRecordPublicationFindingRejectsUnprovenSameTaskScope(t *testing.T) {
	fixture := newCompleteFixture(t)
	ensureCompleteFixturePublicationAuthority(t, fixture)

	err := execute(fixture.cfg, []string{
		actionRecordPublicationFinding,
		"--origin", state.ParentOriginCodexReview,
		"--cause", state.ParentCauseProductionWiring,
	}, &bytes.Buffer{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), sameTaskScopeRegistrationHint) {
		t.Fatalf("unproven publication finding error = %v", err)
	}
	finding, findingErr := fixture.st.CurrentPublicationInvalidatingFinding()
	if findingErr != nil || finding != nil {
		t.Fatalf("rejected publication finding persisted state: finding=%#v err=%v", finding, findingErr)
	}
	plan, planErr := fixture.st.ParentActionPlan()
	if planErr != nil {
		t.Fatal(planErr)
	}
	if plan.RequiredAction != state.ParentActionComplete || plan.Allows(state.ParentActionReopen) {
		t.Fatalf("rejected publication finding changed plan = %#v", plan)
	}
}

func TestExecuteRecordPublicationFindingRejectsParentSelectedTarget(t *testing.T) {
	fixture := newCompleteFixture(t)
	ensureCompleteFixturePublicationAuthority(t, fixture)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}

	err = execute(fixture.cfg, []string{
		actionRecordPublicationFinding,
		"--candidate-oid", candidate.CommitOID,
		"--snapshot-id", candidate.SnapshotID,
	}, &bytes.Buffer{}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), publicationFindingUsage) {
		t.Fatalf("parent-selected target options = %v", err)
	}
	plan, err := fixture.st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.RequiredAction != state.ParentActionComplete || plan.Allows(state.ParentActionReopen) {
		t.Fatalf("rejected parent target changed plan = %#v", plan)
	}
}

func TestExecuteRecordPublicationFindingRejectsMalformedOptions(t *testing.T) {
	fixture := newCompleteFixture(t)
	if err := execute(fixture.cfg, []string{actionRecordPublicationFinding, "--origin"}, &bytes.Buffer{}, io.Discard); err == nil || !strings.Contains(err.Error(), publicationFindingUsage) {
		t.Fatalf("malformed publication finding options = %v", err)
	}
}
