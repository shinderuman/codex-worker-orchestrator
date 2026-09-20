package parentactioncmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompleteAllowsSameTaskReopenLineageAfterTaskFileHandover(t *testing.T) {
	fixture, oldCandidate := prepareReopenedCompletionFixture(t)
	lineage, err := fixture.st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if lineage.TaskID != oldCandidate.TaskID || lineage.BaseHead != oldCandidate.BaseHead || lineage.CommitOID != oldCandidate.CommitOID {
		t.Fatalf("lineage = %#v old candidate = %#v", lineage, oldCandidate)
	}
	if candidate.CommitOID == oldCandidate.CommitOID || candidate.BaseHead != oldCandidate.CommitOID {
		t.Fatalf("reopen reused old candidate: old=%#v new=%#v", oldCandidate, candidate)
	}
	if entry := strings.TrimSpace(pushBindingGitOutput(t, fixture.repo, "ls-tree", candidate.BaseHead, "--", "IMPLEMENTATION_TASKS/active.md")); entry != "" {
		t.Fatalf("reopened candidate base unexpectedly tracks old task: %q", entry)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed || output.Failure != nil {
		t.Fatalf("reopened completion output = %#v", output)
	}
}

func TestCompleteRejectsMissingReopenLineageWhenCandidateBaseNoLongerTracksTask(t *testing.T) {
	fixture, _ := prepareReopenedCompletionFixture(t)
	if err := os.Remove(fixture.st.Path("publication-reopen-lineage.json")); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "reopen lineage is unavailable")
}

func TestCompleteRejectsWrongTaskReopenLineage(t *testing.T) {
	fixture, _ := prepareReopenedCompletionFixture(t)
	lineage, err := fixture.st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	lineage.TaskID = "00000000-0000-4000-8000-000000000001"
	writeReopenLineageFixture(t, fixture, lineage)

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "reopen lineage task identity")
}

func TestCompleteRejectsUnrelatedReopenLineageCommit(t *testing.T) {
	fixture, _ := prepareReopenedCompletionFixture(t)
	lineage, err := fixture.st.LoadPublicationReopenLineage()
	if err != nil {
		t.Fatal(err)
	}
	lineage.CommitOID = createSiblingReopenLineageCommit(t, fixture.repo, lineage)
	writeReopenLineageFixture(t, fixture, lineage)

	output := runCompleteCommand(t, fixture)
	assertCompletionOwnerFailure(t, output, "does not descend from reopen lineage commit")
}

func prepareReopenedCompletionFixture(t *testing.T) (*completeFixture, state.PublicationCandidate) {
	t.Helper()
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	ensureCompleteFixturePublicationAuthority(t, fixture)
	oldCandidate, err := fixture.st.LoadPublicationCandidate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.RecordPublicationInvalidatingFinding(
		oldCandidate.CommitOID,
		oldCandidate.SnapshotID,
		state.ParentOriginCodexReview,
		state.ParentCauseProductionWiring,
	); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.ReopenAcceptedParentCompletion(); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, fixture.repo, "escaped-fix.txt", "fixed\n")
	runFinalizationGit(t, fixture.repo, "add", "escaped-fix.txt")
	runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "escaped defect fix")

	if err := fixture.st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	accepted, err := fixture.st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("reopened accept = %v err=%v", accepted, err)
	}
	ensureCompleteFixturePublicationAuthority(t, fixture)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	return fixture, oldCandidate
}

func writeReopenLineageFixture(t *testing.T, fixture *completeFixture, lineage state.PublicationReopenLineage) {
	t.Helper()
	data, err := json.Marshal(lineage)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.Write("publication-reopen-lineage.json", string(data)); err != nil {
		t.Fatal(err)
	}
}

func createSiblingReopenLineageCommit(t *testing.T, repo string, lineage state.PublicationReopenLineage) string {
	t.Helper()
	tree := strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", lineage.CommitOID+"^{tree}"))
	command := exec.Command("git", "-C", repo, "commit-tree", tree, "-p", lineage.BaseHead)
	command.Stdin = strings.NewReader("unrelated sibling lineage\n")
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	commit := strings.TrimSpace(string(output))
	if len(commit) != 40 {
		t.Fatalf("unexpected sibling commit OID %q", commit)
	}
	return commit
}
