package state

import (
	"os"
	"strings"
	"testing"
)

func TestResumeCheckpointPersistsStopParentFilesOnlyInGitSnapshot(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parents := ParentFileStates{
		{Path: ParentRulesFile, Exists: true, SHA256: "rules-sha"},
		{Path: ParentPlanFile, Exists: true, SHA256: "plan-sha"},
	}
	checkpoint := ResumeCheckpoint{
		Stage:    ResumeStageReview,
		Phase:    "reviewer-1",
		Role:     ReviewerRole,
		Model:    "sonnet",
		StopKind: ResumeStopRateLimited,
	}
	checkpoint.SetStopRepositoryBoundary(GitSnapshot{
		Head:                          "head",
		IndexDigest:                   "index",
		WorktreeDigest:                "worktree",
		WorktreeDigestExcludingParent: "excluding-parent",
		ParentFiles:                   &parents,
	})

	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(st.Path(resumeStateFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\"stop_parent_files\"") {
		t.Fatalf("duplicate stop_parent_files was persisted: %s", data)
	}
	if !strings.Contains(string(data), "\"stop_git_snapshot\"") || !strings.Contains(string(data), "\"parent_files\"") {
		t.Fatalf("canonical stop snapshot parent files were not persisted: %s", data)
	}

	loaded, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StopGitSnapshot == nil || loaded.StopGitSnapshot.ParentFiles == nil || !SameParentFileStates(*loaded.StopGitSnapshot.ParentFiles, parents) {
		t.Fatalf("canonical stop parent files = %#v", loaded.StopGitSnapshot)
	}
	if loaded.StopParentFiles == nil || !SameParentFileStates(*loaded.StopParentFiles, parents) {
		t.Fatalf("derived stop parent files = %#v", loaded.StopParentFiles)
	}
}

func TestLoadResumeCheckpointRejectsLegacyDivergentStopParentFiles(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	doc := `{
  "version": 6,
  "stage": "reviewer",
  "phase": "reviewer-1",
  "role": "reviewer",
  "model": "sonnet",
  "prompt": "review",
  "request": "request",
  "stop_kind": "rate-limited",
  "report_only": false,
  "stop_git_snapshot": {
    "version": 1,
    "head": "head",
    "index_digest": "index",
    "worktree_digest": "worktree",
    "parent_files": [{"path":"IMPLEMENTATION_RULES.md","exists":true,"sha256":"canonical"}]
  },
  "stop_parent_files": [{"path":"IMPLEMENTATION_RULES.md","exists":true,"sha256":"legacy"}]
}`
	if err := os.WriteFile(st.Path(resumeStateFile), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil || !strings.Contains(err.Error(), "legacy stop_parent_files key") {
		t.Fatalf("legacy duplicate load error = %v", err)
	}
}

func TestSaveResumeCheckpointRejectsDivergentDerivedStopParentFiles(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	canonical := ParentFileStates{{Path: ParentRulesFile, Exists: true, SHA256: "canonical"}}
	divergent := ParentFileStates{{Path: ParentRulesFile, Exists: true, SHA256: "divergent"}}
	checkpoint := ResumeCheckpoint{
		Stage:           ResumeStageReview,
		Model:           "sonnet",
		StopKind:        ResumeStopRateLimited,
		StopGitSnapshot: &GitSnapshot{ParentFiles: &canonical},
		StopParentFiles: &divergent,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "diverge from canonical") {
		t.Fatalf("divergent derived state save error = %v", err)
	}
}

func TestSaveResumeCheckpointRejectsUnprovenStopParentConsumption(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parents := ParentFileStates{{Path: ParentRulesFile, Exists: true, SHA256: "canonical"}}
	checkpoint := ResumeCheckpoint{
		Stage:           ResumeStageReview,
		Model:           "sonnet",
		StopKind:        ResumeStopRateLimited,
		StopGitSnapshot: &GitSnapshot{ParentFiles: &parents},
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err == nil || !strings.Contains(err.Error(), "diverge from canonical") {
		t.Fatalf("unproven stop parent consumption save error = %v", err)
	}
}

func TestSaveResumeCheckpointConsumesDerivedStopParentFiles(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	parents := ParentFileStates{{Path: ParentRulesFile, Exists: true, SHA256: "canonical"}}
	checkpoint := ResumeCheckpoint{
		Stage:    ResumeStageReview,
		Model:    "sonnet",
		StopKind: ResumeStopRateLimited,
	}
	checkpoint.SetStopRepositoryBoundary(GitSnapshot{ParentFiles: &parents})
	checkpoint.StopParentFiles = nil
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StopGitSnapshot == nil || loaded.StopGitSnapshot.ParentFiles != nil || loaded.StopParentFiles != nil {
		t.Fatalf("consumed stop parent state = %#v %#v", loaded.StopGitSnapshot, loaded.StopParentFiles)
	}
}
