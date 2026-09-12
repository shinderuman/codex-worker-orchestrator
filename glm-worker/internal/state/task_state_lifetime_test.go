package state

import (
	"os"
	"testing"
)

func TestTaskBoundStatePoliciesDriveFreshTaskCleanup(t *testing.T) {
	seen := make(map[string]taskBoundStateLifetime)
	cleanup := make(map[string]bool)
	for _, name := range newTaskTransitionStateFileNames() {
		if cleanup[name] {
			t.Fatalf("duplicate fresh-task cleanup state %s", name)
		}
		cleanup[name] = true
	}
	for _, policy := range taskBoundStatePolicies {
		if _, ok := seen[policy.name]; ok {
			t.Fatalf("duplicate task-bound state policy %s", policy.name)
		}
		seen[policy.name] = policy.lifetime
		if cleanup[policy.name] != (policy.lifetime == taskBoundStateFreshTaskClear) {
			t.Fatalf("task-bound state policy and cleanup disagree for %s", policy.name)
		}
	}
	for name, lifetime := range map[string]taskBoundStateLifetime{
		baselineUntrackedFile:                taskBoundStateFreshTaskClear,
		poCStartSnapshotFile:                 taskBoundStateFreshTaskClear,
		QualitySurfaceBaselineStateFile:      taskBoundStateFreshTaskClear,
		RepositoryHarnessActivationStateFile: taskBoundStateFreshTaskClear,
		InstructionSurfaceBaselineStateFile:  taskBoundStateTaskIDBound,
	} {
		if got, ok := seen[name]; !ok || got != lifetime {
			t.Fatalf("task-bound state policy %s = %d, present=%t want=%d", name, got, ok, lifetime)
		}
	}
}

func TestFreshTaskClearsUnboundStateAndRetainsSelfBoundAndHistoricalState(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SavePoCStartSnapshot(GitSnapshot{Head: "old-head", IndexDigest: "old-index", WorktreeDigest: "old-worktree"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(baselineUntrackedFile), []byte("old-untracked\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(QualitySurfaceBaselineStateFile, "old-quality"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(RepositoryHarnessActivationStateFile, "1"); err != nil {
		t.Fatal(err)
	}
	instructionBaseline := oldTaskID + " old-instruction-digest"
	if err := st.Write(InstructionSurfaceBaselineStateFile, instructionBaseline); err != nil {
		t.Fatal(err)
	}
	st.RecordParentEvidence(ParentEvidenceRecord{Surface: ParentEvidenceSurfaceStatus, Outcome: ParentEvidenceOutcomeProjected, TaskID: oldTaskID})

	newTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if newTaskID == oldTaskID {
		t.Fatalf("task identity was reused: %s", newTaskID)
	}
	if _, err := st.LoadPoCStartSnapshot(); !os.IsNotExist(err) {
		t.Fatalf("prior task PoC snapshot remains visible: %v", err)
	}
	for _, name := range []string{baselineUntrackedFile, QualitySurfaceBaselineStateFile, RepositoryHarnessActivationStateFile} {
		if st.Exists(name) {
			t.Fatalf("prior task state survived fresh-task cleanup: %s", name)
		}
	}
	if got := st.ReadOr(InstructionSurfaceBaselineStateFile, ""); got != instructionBaseline {
		t.Fatalf("task-ID-bound instruction baseline = %q want %q", got, instructionBaseline)
	}
	records, err := st.ReadParentEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].TaskID != oldTaskID {
		t.Fatalf("historical parent evidence changed across fresh task: %#v", records)
	}
}
