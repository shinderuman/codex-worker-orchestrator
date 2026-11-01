package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindingMutationSourceGenerationAnchorsCurrentProject(t *testing.T) {
	t.Parallel()
	fixture, _, _, _, finding := findingSyncRevisionFixture(t)
	_ = fixture
	before := findingSourceAuthority{
		Task: finding.SourceSemanticTaskRef, Project: finding.ProjectSnapshotID,
		Generation: finding.SourceControllerGeneration + 1,
	}
	step := findingAuthorityProgressionStep{
		sourceGeneration: finding.SourceControllerGeneration,
		targetWorkspaceSnapshot: "later-snapshot",
		anchor: &findingAuthorityAnchor{
			task: finding.SourceSemanticTaskRef, workspace: finding.SourceWorkspaceSnapshotID,
		},
	}
	if !findingAuthorityProgressionAnchors(step, finding, before) {
		t.Fatal("validated mutation from observation generation could not anchor its Finding")
	}
	otherProject := before
	otherProject.Project = "different-project"
	if findingAuthorityProgressionAnchors(step, finding, otherProject) {
		t.Fatal("mutation was anchored in another project")
	}
	step.anchor.workspace = "other-workspace"
	if findingAuthorityProgressionAnchors(step, finding, before) {
		t.Fatal("mutation with another workspace anchored the Finding")
	}
	step.anchor.workspace = finding.SourceWorkspaceSnapshotID
	step.anchor.task.ContractDigest = "another-contract"
	if findingAuthorityProgressionAnchors(step, finding, before) {
		t.Fatal("mutation with another Task anchored the Finding")
	}
	step.anchor.task = finding.SourceSemanticTaskRef
	step.fromTransition = true
	if findingAuthorityProgressionAnchors(step, finding, before) {
		t.Fatal("transition without project evidence anchored the Finding")
	}
	step.anchor.project = finding.ProjectSnapshotID
	if !findingAuthorityProgressionAnchors(step, finding, before) {
		t.Fatal("valid finalized transition project anchor was refused")
	}
}

func TestFindingProgressionScansIgnoreTemporaryFilesButRejectBadJSON(t *testing.T) {
	t.Parallel()
	fixture, _, source, _, finding := findingSyncRevisionFixture(t)
	drifted := progressFindingAuthorityGeneration(t, fixture.store, source)
	_, _ = syncRevisedTaskAuthority(t, fixture.store, drifted, "\n## Amendment\n\nscan recovery\n")
	for _, directory := range []string{"transitions", "mutations"} {
		root := filepath.Join(fixture.store.dir, directory)
		for _, filename := range []string{".controller-abandoned.tmp", "notes", "not-json.json.tmp"} {
			if err := os.WriteFile(filepath.Join(root, filename), []byte("{invalid"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Mkdir(filepath.Join(root, "ignored-directory.json"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err != nil {
		t.Fatalf("stale atomic-write files blocked valid finding disposition: %v", err)
	}
	for _, directory := range []string{"transitions", "mutations"} {
		path := filepath.Join(fixture.store.dir, directory, "broken.json")
		if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err == nil {
			t.Fatalf("corrupt canonical %s JSON was silently ignored", directory)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err != nil {
		t.Fatalf("valid immutable records were not recoverable after stale-file scan: %v", err)
	}
}
