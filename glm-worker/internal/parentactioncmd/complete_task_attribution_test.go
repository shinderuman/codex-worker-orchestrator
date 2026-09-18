package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCompleteRejectsUnrelatedStaleTaskAttribution(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	ensureCompleteFixturePublicationAuthority(t, fixture)
	if err := fixture.st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.Write("active-task", "IMPLEMENTATION_TASKS/stale.md"); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := runComplete(fixture.cfg, &stdout); err != nil {
		t.Fatalf("runComplete: %v: %s", err, stdout.String())
	}
	var output completeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("complete output is not JSON: %v: %s", err, stdout.String())
	}
	if output.Completed || output.Status != completeStatusAwaiting {
		t.Fatalf("output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Reason != "completion_transition_invalid" {
		t.Fatalf("failure = %#v", output.Failure)
	}
}
