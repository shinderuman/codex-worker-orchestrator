package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCompleteRejectsMissingCanonicalHandoverOwner(t *testing.T) {
	fixture := prepareCompletionHandoverFixture(t)
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.st.TaskAuthorityPathPath(taskID)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "canonical task authority unavailable")
}

func TestCompleteRejectsAmbiguousCanonicalHandoverOwner(t *testing.T) {
	fixture := prepareCompletionHandoverFixture(t)
	if err := fixture.st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.st.TaskAuthorityPathPath(taskID), []byte("IMPLEMENTATION_TASKS/active.md\nIMPLEMENTATION_TASKS/next.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "canonical task authority unavailable")
}

func TestCompleteRejectsPublicationCandidateTaskIdentityMismatch(t *testing.T) {
	fixture := prepareCompletionHandoverFixture(t)
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	candidate.TaskID = "00000000-0000-4000-8000-000000000001"
	data, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.Write("publication-candidate.json", string(data)); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "publication candidate task identity")
}

func TestCompleteAllowsExactCanonicalHandoverOwner(t *testing.T) {
	fixture := prepareCompletionHandoverFixture(t)
	if err := fixture.st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n")); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed || output.Failure != nil {
		t.Fatalf("output = %#v", output)
	}
}

func TestCompleteRejectsUnrelatedStaleTaskAttribution(t *testing.T) {
	fixture := prepareCompletionHandoverFixture(t)
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
	assertCompletionOwnerFailure(t, output, "does not match canonical task authority")
}

func prepareCompletionHandoverFixture(t *testing.T) *completeFixture {
	t.Helper()
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	ensureCompleteFixturePublicationAuthority(t, fixture)
	return fixture
}

func assertCompletionOwnerFailure(t *testing.T, output completeOutput, detail string) {
	t.Helper()
	if output.Completed || output.Status != completeStatusAwaiting {
		t.Fatalf("output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Stage != "metadata" || output.Failure.Reason != "completion_transition_invalid" {
		t.Fatalf("failure = %#v", output.Failure)
	}
	if !strings.Contains(output.Failure.Detail, detail) {
		t.Fatalf("failure detail = %q, want %q", output.Failure.Detail, detail)
	}
}
