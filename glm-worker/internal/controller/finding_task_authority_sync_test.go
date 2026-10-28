package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func findingSyncRevisionFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, Admission, SemanticTaskRef, FindingRecord) {
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
	finding, err := fixture.store.ObserveFinding(source, FindingObservationInput{Producer: "independent-reviewer", ProofClass: FindingProofUnverified, ProblemKey: "canonical-reviewer-invalid-decision-orphans-active-task-without-admitted-resume"})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, policy, source, finding.SourceSemanticTaskRef, finding
}

func progressFindingAuthorityGeneration(t *testing.T, store *Store, admission Admission) Admission {
	t.Helper()
	bound, err := store.BindModelCall(admission)
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, bound.Workspace.Root, "result.txt", "model-side result before task authority sync\n")
	after, err := CaptureWorkspaceSnapshot(bound.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := store.RecordAdmittedMutation(bound, "model:worker:finding-authority-progression", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	return completed
}

func syncRevisedTaskAuthority(t *testing.T, store *Store, admission Admission, amendment string) (Admission, SemanticTaskRef) {
	t.Helper()
	repo := store.Identity().PrimaryRoot
	taskPath := admission.Attempt.SemanticTaskRef.TaskPath
	taskData, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(taskPath)))
	if err != nil {
		t.Fatal(err)
	}
	revisedTask := append(append([]byte(nil), taskData...), []byte(amendment)...)
	writeSuspensionTestFile(t, repo, taskPath, string(revisedTask))
	revised := SemanticTaskRef{TaskPath: taskPath, ContractDigest: digestBytes(revisedTask)}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":"+TaskAuthoritySyncProducer+":"+snapshot.ID, true, []byte(`{"retained":true}`))
	if err != nil {
		t.Fatal(err)
	}
	seedFinalValidationNoneObligation(t, store, admission)
	result, err := store.SyncTaskAuthority(admission, snapshot.ID, before, revised)
	if err != nil {
		t.Fatal(err)
	}
	if result.Admission == nil {
		t.Fatal("task authority sync produced no live authority")
	}
	return *result.Admission, revised
}

func TestFindingDispositionAfterTaskAuthoritySyncsBindsCurrentAuthority(t *testing.T) {
	t.Parallel()
	fixture, policy, source, observedRef, finding := findingSyncRevisionFixture(t)
	drifted := progressFindingAuthorityGeneration(t, fixture.store, source)
	syncedOnce, revisedOnce := syncRevisedTaskAuthority(t, fixture.store, drifted, "\n## Amendment\n\nfirst authority sync before disposition\n")
	if revisedOnce.Equal(observedRef) {
		t.Fatal("fixture did not revise the observed Task authority")
	}
	driftedAgain := progressFindingAuthorityGeneration(t, fixture.store, syncedOnce)
	final, revisedTwice := syncRevisedTaskAuthority(t, fixture.store, driftedAgain, "\n## Amendment\n\nsecond authority sync before disposition\n")
	if revisedTwice.Equal(revisedOnce) {
		t.Fatal("fixture did not revise the synced Task authority again")
	}

	project, err := fixture.store.LoadProjectSnapshot(final.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blocker := findProjectTask(project, "IMPLEMENTATION_TASKS/revision-blocker.md")
	if blocker.Empty() {
		t.Fatal("independent finding target missing from the synced project authority")
	}

	result, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:          FindingDecisionIndependentNonBlocking,
		TargetTaskRef: &blocker,
	})
	if err != nil {
		t.Fatalf("post-sync disposition rejected the continued same-attempt finding: %v", err)
	}
	if result.Disposition == nil || result.Disposition.Kind != FindingDispositionIndependentNonBlocking {
		t.Fatalf("disposition = %#v", result.Disposition)
	}
	if result.Disposition.ControllerGeneration != final.Head.ControllerGeneration {
		t.Fatalf("disposition bound generation %d, want current %d", result.Disposition.ControllerGeneration, final.Head.ControllerGeneration)
	}
	immutable, err := fixture.store.LoadFinding(finding.FindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !immutable.SourceSemanticTaskRef.Equal(observedRef) || immutable.SourceControllerGeneration != finding.SourceControllerGeneration {
		t.Fatalf("finding observation record was rewritten: %#v", immutable)
	}

	runtimeCfg := WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), final.Attempt.SemanticTaskRef)
	runtime := state.AttachStateStore(runtimeCfg)
	if err := runtime.Write("task.id", "finding-sync-acceptance"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          final.Attempt.AttemptID,
		TaskPath:           final.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: final.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}

	writeSuspensionTestFile(t, final.Workspace.Root, "result.txt", "revised implementation\n")
	laneSnapshot, err := CaptureWorkspaceSnapshot(final.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fixture.store.RecordAdmittedMutation(final, "lane edit after sync disposition", "success", laneSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := fixture.store.AcceptExecutionCandidate(sealed, CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "post-sync result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, sealed, policy)})
	if err != nil {
		t.Fatalf("same-Task candidate acceptance rejected the dispositioned finding: %v", err)
	}
	if accepted.CandidateRef == nil {
		t.Fatalf("candidate acceptance produced no candidate: %#v", accepted)
	}
}

func TestFindingDispositionAfterTaskAuthoritySyncRejectsBrokenContinuity(t *testing.T) {
	t.Parallel()
	fixture, _, source, _, finding := findingSyncRevisionFixture(t)
	drifted := progressFindingAuthorityGeneration(t, fixture.store, source)
	synced, revised := syncRevisedTaskAuthority(t, fixture.store, drifted, "\n## Amendment\n\ncontinuity rejection fixture\n")
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
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: revised, SourceControllerGeneration: synced.Head.ControllerGeneration - 1},
			want:    "finding source controller generation is stale",
		},
		{
			name:    "observation generation without progression",
			finding: FindingRecord{SourceAttemptID: source.Attempt.AttemptID, SourceSemanticTaskRef: finding.SourceSemanticTaskRef, SourceControllerGeneration: finding.SourceControllerGeneration + 1, ProjectSnapshotID: finding.ProjectSnapshotID, SourceWorkspaceSnapshotID: finding.SourceWorkspaceSnapshotID},
			want:    "neither current live execution nor accepted terminal history",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := fixture.store.findingResolutionAuthority(tc.finding); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("sync continuity rejection = %v, want %q", err, tc.want)
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
}

func TestFindingSourceRevisionRejectsUnfinalizedSyncTransition(t *testing.T) {
	t.Parallel()
	fixture, _, source, _, finding := findingSyncRevisionFixture(t)
	drifted := progressFindingAuthorityGeneration(t, fixture.store, source)
	synced, _ := syncRevisedTaskAuthority(t, fixture.store, drifted, "\n## Amendment\n\nunfinalized sync fixture\n")
	refs, err := fixture.store.publishedTaskSourceRevisionRefs(synced.Attempt.SemanticTaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("sync source revision refs = %#v", refs)
	}
	record, err := fixture.store.LoadTaskSourceRevision(refs[0])
	if err != nil {
		t.Fatal(err)
	}
	phase, err := fixture.store.loadTransitionState(record.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	phase.Phase = TransitionPhasePrepared
	if err := fixture.store.writeTransitionState(phase); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err == nil || !strings.Contains(err.Error(), "finding source revision lacks its finalized same-attempt transition") {
		t.Fatalf("unfinalized sync transition was accepted: %v", err)
	}
	if _, err := fixture.store.LoadFindingDisposition(finding.FindingID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unfinalized sync continuity committed a disposition: %v", err)
	}
}

func TestFindingSourceRevisionRejectsForeignKindTransition(t *testing.T) {
	t.Parallel()
	fixture, _, source, _, _ := findingSyncRevisionFixture(t)
	bound, err := fixture.store.BindModelCall(source)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(fixture.store.dir, "transitions"))
	if err != nil {
		t.Fatal(err)
	}
	var record TransitionRecord
	found := false
	for _, entry := range entries {
		var candidate TransitionRecord
		if err := readJSON(filepath.Join(fixture.store.dir, "transitions", entry.Name()), &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.Kind == executionModelCall && candidate.TargetAttemptID == source.Attempt.AttemptID && candidate.TargetLeaseID == bound.Lease.LeaseID {
			record = candidate
			found = true
			break
		}
	}
	if !found {
		t.Fatal("fixture produced no finalized model-call transition")
	}
	storedRef, stored, err := fixture.store.StoreTaskSourceRevision(TaskSourceRevisionRecord{
		SchemaVersion:             evidenceSchemaVersion,
		AttemptID:                 record.SourceAttemptID,
		PreviousTaskRef:           record.SourceExecutionTaskRef,
		RevisedTaskRef:            record.TargetExecutionTaskRef,
		PreviousProjectSnapshotID: record.ProjectSnapshotOld,
		RevisedProjectSnapshotID:  record.ProjectSnapshotNew,
		SourceSnapshotID:          record.WorkspaceSnapshotOld.ID,
		TransitionID:              record.TransitionID,
		ControllerGeneration:      record.CommittedGeneration,
		CreatedAt:                 time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.LoadTaskSourceRevision(storedRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.findingSourceRevisionTransition(stored, findingSourceAuthority{Task: record.TargetExecutionTaskRef, Project: record.ProjectSnapshotNew, Generation: record.TargetGeneration}); err == nil || !strings.Contains(err.Error(), "finding source revision lacks its finalized same-attempt transition") {
		t.Fatalf("foreign-kind transition owned a finding source revision: %v", err)
	}
}

func TestFindingTaskSyncRejectsCorruptMutationContinuity(t *testing.T) {
	t.Parallel()
	fixture, _, source, _, finding := findingSyncRevisionFixture(t)
	drifted := progressFindingAuthorityGeneration(t, fixture.store, source)
	_, _ = syncRevisedTaskAuthority(t, fixture.store, drifted, "\n## Amendment\n\nmutation continuity\n")
	entries, err := os.ReadDir(filepath.Join(fixture.store.dir, "mutations"))
	if err != nil {
		t.Fatal(err)
	}
	var original MutationRecord
	var path string
	for _, entry := range entries {
		candidatePath := filepath.Join(fixture.store.dir, "mutations", entry.Name())
		var candidate MutationRecord
		if err := readJSON(candidatePath, &candidate); err != nil {
			t.Fatal(err)
		}
		if candidate.AttemptID == source.Attempt.AttemptID && candidate.TargetGeneration == drifted.Head.ControllerGeneration {
			original, path = candidate, candidatePath
			break
		}
	}
	if path == "" {
		t.Fatal("model-call mutation was not recorded")
	}
	headBefore, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		corrupt func(*MutationRecord)
	}{
		{"nonadvancing generation", func(r *MutationRecord) { r.SourceGeneration = r.TargetGeneration }},
		{"generation gap", func(r *MutationRecord) { r.SourceGeneration-- }},
		{"broken source snapshot", func(r *MutationRecord) { r.Before.ID = "unrelated" }},
		{"broken target snapshot", func(r *MutationRecord) { r.After.ID = "unrelated" }},
		{"foreign lease", func(r *MutationRecord) { r.TargetLeaseID = r.SourceLeaseID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			changed := original
			tc.corrupt(&changed)
			if err := writeJSONAtomic(path, changed); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err == nil {
				t.Fatal("corrupt mutation continuity was accepted")
			}
			if _, err := fixture.store.LoadFindingDisposition(finding.FindingID); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected continuity committed a disposition: %v", err)
			}
			headAfter, err := fixture.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			assertFindingHeadUnchanged(t, headBefore, headAfter)
			if err := writeJSONAtomic(path, original); err != nil {
				t.Fatal(err)
			}
			if _, _, _, err := fixture.store.findingResolutionAuthority(finding); err != nil {
				t.Fatalf("restored real mutation must remain resolvable: %v", err)
			}
		})
	}
}
