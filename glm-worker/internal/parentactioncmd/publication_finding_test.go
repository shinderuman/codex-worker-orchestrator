package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExecuteRecordPublicationFindingForcesReopen(t *testing.T) {
	fixture := newCompleteFixture(t)
	ensureCompleteFixturePublicationAuthority(t, fixture)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := execute(fixture.cfg, []string{
		actionRecordPublicationFinding,
		"--origin", state.ParentOriginCodexReview,
		"--cause", state.ParentCauseProductionWiring,
	}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}

	var output publicationFindingOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("publication finding output is not JSON: %v: %s", err, stdout.String())
	}
	if output.Status != "recorded" || output.RequiredAction != state.ParentActionReopen {
		t.Fatalf("publication finding output = %#v", output)
	}
	if !state.ValidGeneratedUUID(output.Finding.FindingID) || output.Finding.TaskID != candidate.TaskID || output.Finding.CandidateCommitOID != candidate.CommitOID || output.Finding.CandidateSnapshotID != candidate.SnapshotID || output.Finding.Disposition != state.PublicationFindingCorrectnessDefect {
		t.Fatalf("publication finding = %#v candidate=%#v", output.Finding, candidate)
	}
	if len(output.AllowedActions) != 1 || output.AllowedActions[0] != state.ParentActionReopen {
		t.Fatalf("publication finding allowed actions = %#v", output.AllowedActions)
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
