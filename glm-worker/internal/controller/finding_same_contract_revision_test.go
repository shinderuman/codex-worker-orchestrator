package controller

import (
	"reflect"
	"strconv"
	"testing"
)

func TestFindingDispositionAfterSameContractSourceRevisions(t *testing.T) {
	t.Parallel()
	for _, revisions := range []int{1, 2} {
		t.Run(strconv.Itoa(revisions), func(t *testing.T) {
			t.Parallel()
			fixture, _, source, task, observed, finding := findingAfterRevisionFixture(t)
			for step := 0; step < revisions; step++ {
				imported := importSameContractSource(t, fixture.store, source, task, "parent-revision.txt", strconv.Itoa(step))
				source = *imported.Admission
			}
			if !source.Attempt.SemanticTaskRef.Equal(observed) {
				t.Fatal("fixture changed the active Task contract")
			}
			project, err := fixture.store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
			if err != nil {
				t.Fatal(err)
			}
			target := findProjectTask(project, "IMPLEMENTATION_TASKS/revision-blocker.md")
			result, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{Kind: FindingDecisionIndependentNonBlocking, TargetTaskRef: &target})
			if err != nil || result.Disposition == nil || result.Disposition.ControllerGeneration != source.Head.ControllerGeneration {
				t.Fatalf("same-contract continuation could not resolve its retained finding: %v", err)
			}
			retained, err := fixture.store.LoadFinding(finding.FindingID)
			if err != nil || !reflect.DeepEqual(retained, finding) {
				t.Fatalf("finding source identity was not retained: %v", err)
			}
		})
	}
}
