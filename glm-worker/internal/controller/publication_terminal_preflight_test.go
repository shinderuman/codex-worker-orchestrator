package controller

import (
	"reflect"
	"strings"
	"testing"
)

func TestTerminalCandidateRejectsInvalidSuccessorBeforeAcceptance(t *testing.T) {
	f, policy := configurePublicationTestFixture(t, findingAcceptanceFixtureForChild(t, "# child\n\n## Contract\n\nchild target\n\n## Dependencies\n\nnone\n"))
	before := f.source.Head
	if _, err := f.store.PreviewCandidateTree(f.source, policy); err == nil || !strings.Contains(err.Error(), "External feasibility") {
		t.Fatalf("invalid successor preview: %v", err)
	}
	if _, err := f.store.AcceptExecutionCandidate(f.source, CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "invalid terminal metadata", Policy: policy}); err == nil || !strings.Contains(err.Error(), "External feasibility") {
		t.Fatalf("invalid successor acceptance: %v", err)
	}
	after, err := f.store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("preflight changed Controller authority")
	}
	if remote := observeTestRemote(t, f.store.identity.PrimaryRoot, policy); remote != before.IntegrationTip {
		t.Fatal("preflight published invalid metadata")
	}
}
