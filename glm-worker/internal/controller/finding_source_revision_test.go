package controller

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func findingAfterRevisionFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, Admission, SemanticTaskRef, SemanticTaskRef, FindingRecord) {
	t.Helper()
	blockerPath := "IMPLEMENTATION_TASKS/revision-blocker.md"
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, blockerPath))
	edited := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, edited)
	mat, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	source := *mat.Admission
	successorTask := *retired.Head.RootTaskRef
	finding, err := fixture.store.ObserveFinding(source, FindingObservationInput{Producer: "independent-reviewer", ProofClass: FindingProofUnverified, ProblemKey: "canonical-reviewer-invalid-decision-orphans-active-task-without-admitted-resume"})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, policy, source, successorTask, finding.SourceSemanticTaskRef, finding
}

func TestFindingDispositionAfterSourceRevisionBindsCurrentAuthority(t *testing.T) {
	t.Parallel()
	fixture, policy, source, successorTask, observedRef, finding := findingAfterRevisionFixture(t)
	imported, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\nfinding observed before source revision\n")
	if revised.Equal(observedRef) {
		t.Fatal("fixture did not revise the observed Task authority")
	}

	project, err := fixture.store.LoadProjectSnapshot(imported.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blocker := findProjectTask(project, "IMPLEMENTATION_TASKS/revision-blocker.md")
	if blocker.Empty() {
		t.Fatal("independent finding target missing from the revised project authority")
	}

	result, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:          FindingDecisionIndependentNonBlocking,
		TargetTaskRef: &blocker,
	})
	if err != nil {
		t.Fatalf("post-revision disposition rejected the continued same-attempt finding: %v", err)
	}
	if result.Disposition == nil || result.Disposition.Kind != FindingDispositionIndependentNonBlocking {
		t.Fatalf("disposition = %#v", result.Disposition)
	}
	if result.Disposition.ControllerGeneration != imported.Head.ControllerGeneration {
		t.Fatalf("disposition bound generation %d, want current %d", result.Disposition.ControllerGeneration, imported.Head.ControllerGeneration)
	}
	immutable, err := fixture.store.LoadFinding(finding.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !immutable.SourceSemanticTaskRef.Equal(observedRef) || immutable.SourceControllerGeneration != finding.SourceControllerGeneration {
		t.Fatalf("finding observation record was rewritten: %#v", immutable)
	}

	writeSuspensionTestFile(t, imported.Admission.Workspace.Root, "result.txt", "revised implementation\n")
	laneSnapshot, err := CaptureWorkspaceSnapshot(imported.Admission.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fixture.store.RecordAdmittedMutation(*imported.Admission, "lane edit after disposition", "success", laneSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := fixture.store.AcceptExecutionCandidate(sealed, CandidateAcceptanceInput{Message: "post-disposition result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, sealed, policy)})
	if err != nil {
		t.Fatalf("same-Task candidate acceptance rejected the dispositioned finding: %v", err)
	}
	if accepted.CandidateRef == nil {
		t.Fatalf("candidate acceptance produced no candidate: %#v", accepted)
	}
}

func TestFindingDispositionAfterSourceRevisionRejectsBrokenContinuity(t *testing.T) {
	t.Parallel()
	fixture, _, source, successorTask, _, finding := findingAfterRevisionFixture(t)
	imported, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\ncontinuity rejection fixture\n")
	headAfter, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		finding FindingRecord
		want    string
	}{
		{
			name:    "foreign attempt",
			finding: FindingRecord{SourceAttemptID: "00000000-0000-0000-0000-000000000000", SourceSemanticTaskRef: finding.SourceSemanticTaskRef, SourceControllerGeneration: finding.SourceControllerGeneration},
			want:    "read attempt",
		},
		{
			name:    "foreign task path",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/foreign.md", ContractDigest: finding.SourceSemanticTaskRef.ContractDigest}, SourceControllerGeneration: finding.SourceControllerGeneration},
			want:    "neither current live execution nor accepted terminal history",
		},
		{
			name:    "untraceable task digest",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: SemanticTaskRef{TaskPath: finding.SourceSemanticTaskRef.TaskPath, ContractDigest: strings.Repeat("ab", 32)}, SourceControllerGeneration: finding.SourceControllerGeneration},
			want:    "neither current live execution nor accepted terminal history",
		},
		{
			name:    "foreign project snapshot",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: finding.SourceSemanticTaskRef, SourceControllerGeneration: finding.SourceControllerGeneration, ProjectSnapshotID: "unrelated", SourceWorkspaceSnapshotID: finding.SourceWorkspaceSnapshotID},
			want:    "neither current live execution nor accepted terminal history",
		},
		{
			name:    "foreign workspace snapshot",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: finding.SourceSemanticTaskRef, SourceControllerGeneration: finding.SourceControllerGeneration, ProjectSnapshotID: finding.ProjectSnapshotID, SourceWorkspaceSnapshotID: "unrelated"},
			want:    "neither current live execution nor accepted terminal history",
		},
		{
			name:    "current digest with stale generation",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: revised, SourceControllerGeneration: imported.Head.ControllerGeneration - 1},
			want:    "finding source controller generation is stale",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, _, err := fixture.store.findingResolutionAuthority(tc.finding); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("continuity rejection = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := fixture.store.LoadFindingDisposition(finding.FindingID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected continuity committed a disposition: %v", err)
	}
	after, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, headAfter, after)
	if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err != nil {
		t.Fatalf("the real observed finding must remain resolvable after rejections: %v", err)
	}
	_ = revised
	_ = headAfter
}
