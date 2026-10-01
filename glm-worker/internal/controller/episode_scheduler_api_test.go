package controller

import "testing"

func TestResolveFindingWithProjectAuthorityReplaysBlockingEpisode(t *testing.T) {
	fixture := newFindingAcceptanceFixture(t)
	finding := observeAcceptanceFinding(t, fixture, "canonical-blocker", "reviewer-a")
	target := fixture.child
	decision := FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &target,
		BlockingBoundary: "root waits for child",
	}

	first, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, decision)
	if err != nil {
		t.Fatal(err)
	}
	if first.Episode == nil || first.NextTaskRef == nil || !first.NextTaskRef.Equal(target) ||
		first.Intent != FindingIntentOpenBlockerEpisode {
		t.Fatalf("first canonical blocking result = %#v", first)
	}

	replay, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, decision)
	if err != nil {
		t.Fatal(err)
	}
	if replay.Episode == nil || replay.Episode.RevisionID != first.Episode.RevisionID ||
		replay.NextTaskRef == nil || !replay.NextTaskRef.Equal(target) ||
		replay.Intent != FindingIntentOpenBlockerEpisode {
		t.Fatalf("replayed canonical blocking result = %#v", replay)
	}

	scheduled, err := fixture.store.ScheduleEpisodeWithProjectAuthority(first.Episode.EpisodeID, first.Episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if scheduled.NextTaskRef == nil || !scheduled.NextTaskRef.Equal(target) ||
		scheduled.Intent != FindingIntentStartBlockerTask {
		t.Fatalf("canonical schedule = %#v", scheduled)
	}
}
