package controller

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func reenteredParentSourceFixture(t *testing.T) (findingAcceptanceFixture, Admission) {
	t.Helper()
	f, _, head, candidate := acceptedAmendmentLane(t)
	reentered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	return f, *live.Admission
}

func TestParentSourceImportPreservesAttemptAndEvidenceAcrossRecovery(t *testing.T) {
	t.Parallel()
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "result.txt", "revised task result\n")
	writeSuspensionTestFile(t, repo, "parent-review-fix.txt", "new reviewed source\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", source.Attempt.AttemptID+":reentry-review-repair:"+snapshot.ID, true, []byte("{\"retained\":true}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportParentSource(source, "wrong-snapshot", before, source.Attempt.SemanticTaskRef); err == nil {
		t.Fatal("stale source snapshot admitted")
	}
	stale := source
	stale.Head.ControllerGeneration--
	if _, err := f.store.ImportParentSource(stale, snapshot.ID, before, stale.Attempt.SemanticTaskRef); err == nil {
		t.Fatal("stale generation admitted")
	}
	result, err := f.store.ImportParentSource(source, snapshot.ID, before, source.Attempt.SemanticTaskRef)
	if err != nil {
		t.Fatal(err)
	}
	live := result.Admission
	if live == nil || live.Attempt.AttemptID != source.Attempt.AttemptID || live.Workspace != source.Workspace || live.Lease.LeaseID == source.Lease.LeaseID {
		t.Fatal("source import replaced Task execution instead of its lease")
	}
	if live.Snapshot.Head != source.Snapshot.Head || live.Snapshot.IndexDigest != source.Snapshot.IndexDigest || live.Snapshot.RefDigest != source.Snapshot.RefDigest {
		t.Fatal("source import changed protected Git authority")
	}
	got, err := os.ReadFile(filepath.Join(live.Workspace.Root, "parent-review-fix.txt"))
	if err != nil || string(got) != "new reviewed source\n" {
		t.Fatal("source import lost implementation")
	}
	got, err = os.ReadFile(filepath.Join(live.Workspace.Root, "result.txt"))
	if err != nil || string(got) != "revised task result\n" {
		t.Fatal("existing implementation was not updated")
	}
	if _, err := f.store.RecoverExecutionOperation(result.TransitionID); err != nil {
		t.Fatal(err)
	}
	data, err := f.store.ReentryReviewRepairEvidence(result.TransitionID)
	if err != nil || string(data) != "{\"retained\":true}" {
		t.Fatalf("retained runtime evidence: %s %v", data, err)
	}
	if result.Head.EvidenceLedgerSequence != source.Head.EvidenceLedgerSequence+1 {
		t.Fatal("repair did not append evidence authority")
	}
	if _, err := f.store.LoadEvidenceHead(*source.Head.EvidenceHeadRef); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
		t.Fatal("old lease remained admitted")
	}
}

func TestParentSourceImportRejectsTaskMetadataChanges(t *testing.T) {
	t.Parallel()
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	path := filepath.Join(repo, source.Attempt.SemanticTaskRef.TaskPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("\nforeign authority\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.ImportParentSource(source, snapshot.ID, before, source.Attempt.SemanticTaskRef); err == nil {
		t.Fatal("changed Task contract admitted")
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, source.Head, after)
}

func TestParentSourceImportBindsNonActiveMetadataWithoutChangingTaskOrIndex(t *testing.T) {
	t.Parallel()
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "IMPLEMENTATION_RULES.md", "parent-managed metadata revision\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.ImportParentSource(source, snapshot.ID, before, source.Attempt.SemanticTaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.ProjectSnapshotID == source.Head.ProjectSnapshotID || !result.Admission.Attempt.SemanticTaskRef.Equal(source.Attempt.SemanticTaskRef) || result.Admission.Snapshot.IndexDigest != source.Snapshot.IndexDigest {
		t.Fatal("parent metadata revision lost Project or execution authority")
	}
	got, err := os.ReadFile(filepath.Join(source.Workspace.Root, "IMPLEMENTATION_RULES.md"))
	if err != nil || string(got) != "parent-managed metadata revision\n" {
		t.Fatal("metadata revision was not materialized")
	}
}

func TestParentSourceImportRecoversOnlyArchivedBeforeOrTargetBytes(t *testing.T) {
	t.Parallel()
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "result.txt", "updated result\n")
	writeSuspensionTestFile(t, repo, "a-added.txt", "added source\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", "before-review", true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	op, err := f.store.planParentSourceImport(source, snapshot.ID, before, source.Attempt.SemanticTaskRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.prepareExecutionOperation(&op, source.Head); err != nil {
		t.Fatal(err)
	}
	pending, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.store.PendingExecutionRecoveryTask(pending)
	if err != nil || !task.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatalf("pending recovery task: %#v %v", task, err)
	}
	foreign := pending
	foreign.LiveAttemptID = "foreign-attempt"
	if _, err := f.store.PendingExecutionRecoveryTask(foreign); err == nil {
		t.Fatal("foreign recovery authority admitted")
	}
	writeSuspensionTestFile(t, source.Workspace.Root, "a-added.txt", "added source\n")
	writeSuspensionTestFile(t, source.Workspace.Root, "result.txt", "foreign mutation\n")
	if _, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("foreign partial state admitted")
	}
	writeSuspensionTestFile(t, source.Workspace.Root, "result.txt", "task result\n")
	result, err := f.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Admission.Attempt.AttemptID != source.Attempt.AttemptID {
		t.Fatal("recovery replaced attempt")
	}
	got, err := os.ReadFile(filepath.Join(source.Workspace.Root, "result.txt"))
	if err != nil || string(got) != "updated result\n" {
		t.Fatal("recovery did not apply archived source")
	}
}

func rootLaneExecutionFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, Admission, SemanticTaskRef) {
	t.Helper()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, source)
	successor, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, policy, *successor.Admission, *retired.Head.RootTaskRef
}

func importRevisedRootSource(t *testing.T, store *Store, admission Admission, task SemanticTaskRef, amendment string) (ExecutionOperationResult, SemanticTaskRef) {
	t.Helper()
	repo := store.Identity().PrimaryRoot
	taskData, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(task.TaskPath)))
	if err != nil {
		t.Fatal(err)
	}
	revisedTask := append(append([]byte(nil), taskData...), []byte(amendment)...)
	writeSuspensionTestFile(t, repo, task.TaskPath, string(revisedTask))
	runControllerGit(t, repo, "add", ".")
	revised := SemanticTaskRef{TaskPath: task.TaskPath, ContractDigest: fmt.Sprintf("%x", sha256.Sum256(revisedTask))}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":reentry-review-repair:"+snapshot.ID, true, []byte(`{"retained":true}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ImportParentSource(admission, snapshot.ID, before, revised)
	if err != nil {
		t.Fatal(err)
	}
	return result, revised
}

func importSameContractSource(t *testing.T, store *Store, admission Admission, taskRef SemanticTaskRef, sourcePath, sourceData string) ExecutionOperationResult {
	t.Helper()
	repo := store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, sourcePath, sourceData)
	runControllerGit(t, repo, "add", ".")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":reentry-review-repair:"+snapshot.ID, true, []byte(`{"retained":true}`))
	if err != nil {
		t.Fatal(err)
	}
	result, err := store.ImportParentSource(admission, snapshot.ID, before, taskRef)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestParentSourceImportRepeatsSameContractRevision(t *testing.T) {
	t.Parallel()
	fixture, _, source, successorTask := rootLaneExecutionFixture(t)
	first, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\nfirst source revision\n")

	second := importSameContractSource(t, fixture.store, *first.Admission, revised, "parent-fix-2.txt", "second source change\n")
	live := second.Admission
	if live == nil || live.Attempt.AttemptID != source.Attempt.AttemptID || live.Workspace != source.Workspace {
		t.Fatalf("second source revision replaced Task execution: %#v", live)
	}
	if !live.Attempt.SemanticTaskRef.Equal(revised) || live.Lease.LeaseID == first.Admission.Lease.LeaseID {
		t.Fatalf("second source revision lost the contract or kept the lease: %#v", live.Attempt)
	}

	subject, published, err := fixture.store.publishedTaskSubjectRevision(second.Head, revised)
	if err != nil || !published {
		t.Fatalf("second source revision did not publish the Task index: %v", err)
	}
	revision, err := fixture.store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.SourceRevisions) != 2 || revision.PreviousRevision == nil {
		t.Fatalf("source revision history was not retained: %#v", revision)
	}
	firstRecord, err := fixture.store.LoadTaskSourceRevision(revision.SourceRevisions[0])
	if err != nil {
		t.Fatal(err)
	}
	secondRecord, err := fixture.store.LoadTaskSourceRevision(revision.SourceRevisions[1])
	if err != nil {
		t.Fatal(err)
	}
	transitions := map[string]bool{firstRecord.TransitionID: true, secondRecord.TransitionID: true}
	if !transitions[first.TransitionID] || !transitions[second.TransitionID] || first.TransitionID == second.TransitionID {
		t.Fatalf("source revision history lost its original transition authority: %s %s", firstRecord.TransitionID, secondRecord.TransitionID)
	}
	if firstRecord.SourceRevisionID == secondRecord.SourceRevisionID {
		t.Fatal("second source revision did not create a distinct record")
	}
}

func TestParentSourceImportTaskPublishesAndRetiresAfterRevision(t *testing.T) {
	t.Parallel()
	blockerPath := "IMPLEMENTATION_TASKS/revision-blocker.md"
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, blockerPath))
	edited := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	retiredRoot, _ := publishTerminalTestSource(t, fixture, policy, edited)
	mat, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retiredRoot.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	source := *mat.Admission
	successorTask := *retiredRoot.Head.RootTaskRef
	imported, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\nsource revision before publication\n")
	subject, published, err := fixture.store.publishedTaskSubjectRevision(imported.Head, revised)
	if err != nil || !published {
		t.Fatalf("source revision was not published: %v", err)
	}
	revisionBefore, err := fixture.store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}

	writeSuspensionTestFile(t, imported.Admission.Workspace.Root, "result.txt", "revised task implementation\n")
	laneSnapshot, err := CaptureWorkspaceSnapshot(imported.Admission.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fixture.store.RecordAdmittedMutation(*imported.Admission, "lane edit after source revision", "success", laneSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	retired, candidate := publishTerminalTestSource(t, fixture, policy, sealed)
	if retired.Head.RootTaskRef == nil || retired.Head.RootTaskRef.Equal(revised) || retired.Head.LiveAttemptID != "" {
		t.Fatalf("source-revised Task did not retire into its successor authority: %#v", retired.Head)
	}
	if !candidate.TaskRef.Equal(revised) {
		t.Fatalf("published candidate lost the revised contract: %#v", candidate.TaskRef)
	}

	publishedSubject, ok, err := fixture.store.publishedTaskSubjectRevision(retired.Head, revised)
	if err != nil || !ok {
		t.Fatalf("retired Task index is not published: %v", err)
	}
	revisionAfter, err := fixture.store.LoadTaskIndexRevision(publishedSubject)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisionAfter.SourceRevisions) != len(revisionBefore.SourceRevisions) || !evidenceRefsEqual(revisionAfter.SourceRevisions[0], revisionBefore.SourceRevisions[0]) {
		t.Fatalf("publication/retire dropped or rewrote the source revision history: %#v", revisionAfter)
	}
	if revisionAfter.SemanticStatus != taskStatusTerminal {
		t.Fatalf("retired Task semantic status = %q", revisionAfter.SemanticStatus)
	}
}

func TestParentSourceImportTaskSealsAfterRevision(t *testing.T) {
	t.Parallel()
	blockerPath := "IMPLEMENTATION_TASKS/revision-blocker.md"
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, blockerPath))
	edited := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, edited)
	successor, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	source := *successor.Admission
	successorTask := *retired.Head.RootTaskRef
	project, err := fixture.store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blocker := findProjectTask(project, blockerPath)
	if blocker.Empty() {
		t.Fatal("blocking target task missing from the committed project")
	}
	imported, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\nsource revision before suspension\n")
	subject, published, err := fixture.store.publishedTaskSubjectRevision(imported.Head, revised)
	if err != nil || !published {
		t.Fatalf("source revision was not published: %v", err)
	}
	revisionBefore, err := fixture.store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}

	writeSuspensionTestFile(t, imported.Admission.Workspace.Root, "result.txt", "lane implementation\n")
	laneSnapshot, err := CaptureWorkspaceSnapshot(imported.Admission.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fixture.store.RecordAdmittedMutation(*imported.Admission, "lane edit after source revision", "success", laneSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	finding, err := fixture.store.ObserveFinding(sealed, FindingObservationInput{Producer: "lifecycle-reviewer", ProofClass: FindingProofUnverified, ProblemKey: "source-revision-blocker"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := fixture.store.ResolveFindingWithProjectAuthority(finding.FindingID, FindingDecision{
		Kind:             FindingDecisionIndependentBlocking,
		TargetTaskRef:    &blocker,
		BlockingBoundary: successorTask.TaskPath + " is blocked until " + blocker.TaskPath + " is satisfied",
	})
	if err != nil || resolved.Episode == nil {
		t.Fatalf("blocking finding did not plan an episode: %#v %v", resolved.Episode, err)
	}
	suspended, err := fixture.store.SuspendExecution(sealed, resolved.Episode.EpisodeID, resolved.Episode.Revision)
	if err != nil {
		t.Fatalf("suspension rejected the source-revised Task: %v", err)
	}

	sealedSubject, ok, err := fixture.store.publishedTaskSubjectRevision(suspended.Head, revised)
	if err != nil || !ok {
		t.Fatalf("sealed Task index is not published: %v", err)
	}
	sealedRevision, err := fixture.store.LoadTaskIndexRevision(sealedSubject)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealedRevision.SourceRevisions) != len(revisionBefore.SourceRevisions) || !evidenceRefsEqual(sealedRevision.SourceRevisions[0], revisionBefore.SourceRevisions[0]) {
		t.Fatalf("seal transition dropped or rewrote the source revision history: %#v", sealedRevision)
	}
	if len(sealedRevision.AttemptSeals) == 0 {
		t.Fatalf("seal transition published no attempt seal: %#v", sealedRevision)
	}
}

func TestParentSourceImportRejectsUndeclaredSourceRevisionInjection(t *testing.T) {
	t.Parallel()
	fixture, _, source, successorTask := rootLaneExecutionFixture(t)
	imported, revised := importRevisedRootSource(t, fixture.store, source, successorTask, "\n## Amendment\n\nsource revision before injection\n")
	headBefore, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}

	rogue, _, err := fixture.store.StoreTaskSourceRevision(TaskSourceRevisionRecord{
		SchemaVersion: evidenceSchemaVersion, AttemptID: source.Attempt.AttemptID,
		PreviousTaskRef: revised, RevisedTaskRef: revised,
		PreviousProjectSnapshotID: "rogue-old-project", RevisedProjectSnapshotID: "rogue-new-project",
		SourceSnapshotID: "rogue-source-snapshot", TransitionID: "rogue-transition",
		ControllerGeneration: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	subject, ok, err := fixture.store.publishedTaskSubjectRevision(imported.Head, revised)
	if err != nil || !ok {
		t.Fatalf("published subject missing: %v", err)
	}
	published, err := fixture.store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}
	record := TransitionRecord{SchemaVersion: controllerSchemaVersion, TransitionID: "inject-transition", CommittedGeneration: imported.Head.ControllerGeneration + 5, TargetAttemptID: source.Attempt.AttemptID, ProjectSnapshotOld: "rogue-old-project", ProjectSnapshotNew: "rogue-new-project"}
	injected := published
	injected.PreviousRevision = &subject
	injected.SourceRevisions = append(append([]EvidenceObjectRef(nil), published.SourceRevisions...), rogue)
	injected.ControllerGeneration = record.CommittedGeneration
	injected.TransitionID = record.TransitionID
	injectedRef, _, err := fixture.store.StoreTaskIndexRevision(injected)
	if err != nil {
		t.Fatal(err)
	}

	if err := fixture.store.validatePublicationIndexAuthority(record, EvidencePublicationInput{TaskRevisionRefs: []EvidenceObjectRef{injectedRef}}); err == nil || !strings.Contains(err.Error(), "undeclared task source revision") {
		t.Fatalf("undeclared source revision injection admitted: %v", err)
	}
	if err := fixture.store.validatePublicationRefs(record, EvidencePublicationInput{SourceRevisionRefs: []EvidenceObjectRef{rogue}}); err == nil || !strings.Contains(err.Error(), "bound to different transition authority") {
		t.Fatalf("foreign-binding source revision admitted: %v", err)
	}
	rogue2, _, err := fixture.store.StoreTaskSourceRevision(TaskSourceRevisionRecord{
		SchemaVersion: evidenceSchemaVersion, AttemptID: "foreign-attempt",
		PreviousTaskRef: revised, RevisedTaskRef: revised,
		PreviousProjectSnapshotID: record.ProjectSnapshotOld, RevisedProjectSnapshotID: record.ProjectSnapshotNew,
		SourceSnapshotID: "rogue-source-snapshot", TransitionID: record.TransitionID,
		ControllerGeneration: record.CommittedGeneration, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.validatePublicationRefs(record, EvidencePublicationInput{SourceRevisionRefs: []EvidenceObjectRef{rogue2}}); err == nil || !strings.Contains(err.Error(), "bound to a different attempt") {
		t.Fatalf("foreign-attempt source revision admitted: %v", err)
	}
	headAfter, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, headBefore, headAfter)
}

func TestParentSourceImportRevisesRootAttemptContract(t *testing.T) {
	t.Parallel()
	fixture, _, source, successorTask := rootLaneExecutionFixture(t)
	store := fixture.store
	if source.Attempt.PredecessorAttemptID != "" || source.Attempt.ResumedFromSealID != "" || source.Attempt.AttemptSealID != "" {
		t.Fatalf("fixture must materialize a root execution attempt: %#v", source.Attempt)
	}
	if _, published, err := store.publishedTaskSubjectRevision(source.Head, source.Attempt.SemanticTaskRef); err != nil || published {
		t.Fatalf("fixture must not publish the Task index before the root revision: %v", err)
	}

	repo := store.Identity().PrimaryRoot
	taskData, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(successorTask.TaskPath)))
	if err != nil {
		t.Fatal(err)
	}
	revisedTask := append([]byte(nil), taskData...)
	revisedTask = append(revisedTask, []byte("\n## Amendment\n\nroot source revision\n")...)
	writeSuspensionTestFile(t, repo, successorTask.TaskPath, string(revisedTask))
	writeSuspensionTestFile(t, repo, "parent-fix.txt", "parent codex fix\n")
	revisedDigest := sha256.Sum256(revisedTask)
	revised := SemanticTaskRef{TaskPath: successorTask.TaskPath, ContractDigest: fmt.Sprintf("%x", revisedDigest)}
	if revised.Equal(successorTask) {
		t.Fatal("fixture did not revise the Task contract")
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.PutEvidenceObject("runtime-artifact", "application/json", source.Attempt.AttemptID+":reentry-review-repair:"+snapshot.ID, true, []byte(`{"retained":true}`))
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.ImportParentSource(source, snapshot.ID, before, revised)
	if err != nil {
		t.Fatal(err)
	}
	live := result.Admission
	if live == nil || live.Attempt.AttemptID != source.Attempt.AttemptID || live.Workspace != source.Workspace || live.Lease.LeaseID == source.Lease.LeaseID {
		t.Fatalf("root source revision replaced Task execution instead of its lease: %#v", live)
	}
	if !live.Attempt.SemanticTaskRef.Equal(revised) || live.Attempt.PredecessorAttemptID != "" || live.Attempt.ResumedFromSealID != "" || live.Attempt.AttemptSealID != "" {
		t.Fatalf("root source revision fabricated attempt lineage: %#v", live.Attempt)
	}
	if result.Head.RootTaskRef == nil || result.Head.ExecutionTaskRef == nil || !result.Head.RootTaskRef.Equal(revised) || !result.Head.ExecutionTaskRef.Equal(revised) {
		t.Fatalf("root source revision did not bind the revised contract: %#v", result.Head)
	}
	got, err := os.ReadFile(filepath.Join(live.Workspace.Root, "parent-fix.txt"))
	if err != nil || string(got) != "parent codex fix\n" {
		t.Fatalf("root source revision did not materialize the revised source: %q %v", got, err)
	}

	subject, published, err := store.publishedTaskSubjectRevision(result.Head, revised)
	if err != nil || !published {
		t.Fatalf("root source revision did not publish the Task index: %v", err)
	}
	revision, err := store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}
	if revision.PreviousRevision != nil || len(revision.SourceRevisions) != 1 || len(revision.AttemptSeals) != 0 || len(revision.Finalizations) != 0 {
		t.Fatalf("root Task index revision must be the first revision without seals or finalizations: %#v", revision)
	}
	sourceRevision, err := store.LoadTaskSourceRevision(revision.SourceRevisions[0])
	if err != nil {
		t.Fatal(err)
	}
	if sourceRevision.AttemptID != source.Attempt.AttemptID || !sourceRevision.PreviousTaskRef.Equal(successorTask) || !sourceRevision.RevisedTaskRef.Equal(revised) || sourceRevision.SourceSnapshotID != snapshot.ID || sourceRevision.TransitionID != result.TransitionID {
		t.Fatalf("root source revision record identity: %#v", sourceRevision)
	}

	if _, err := store.RecoverExecutionOperation(result.TransitionID); err != nil {
		t.Fatal(err)
	}
	retained, err := store.ReentryReviewRepairEvidence(result.TransitionID)
	if err != nil || string(retained) != `{"retained":true}` {
		t.Fatalf("retained runtime evidence: %s %v", retained, err)
	}
	if result.Head.EvidenceLedgerSequence != source.Head.EvidenceLedgerSequence+1 {
		t.Fatalf("root source revision did not append evidence authority exactly once: %d -> %d", source.Head.EvidenceLedgerSequence, result.Head.EvidenceLedgerSequence)
	}
	if _, err := store.LoadEvidenceHead(*source.Head.EvidenceHeadRef); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ImportParentSource(*live, "wrong-snapshot", before, revised); err == nil {
		t.Fatal("stale source snapshot admitted")
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertFindingHeadUnchanged(t, result.Head, after)
}

func TestParentSourceImportPreservesExistingTaskIndexLineage(t *testing.T) {
	t.Parallel()
	f, source := reenteredParentSourceFixture(t)
	repo := f.store.Identity().PrimaryRoot
	writeSuspensionTestFile(t, repo, "result.txt", "revised task result\n")
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	before, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", source.Attempt.AttemptID+":reentry-review-repair:"+snapshot.ID, true, []byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	previousHead, err := f.store.LoadEvidenceHead(*source.Head.EvidenceHeadRef)
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.ImportParentSource(source, snapshot.ID, before, source.Attempt.SemanticTaskRef)
	if err != nil {
		t.Fatal(err)
	}
	currentHead, err := f.store.LoadEvidenceHead(*result.Head.EvidenceHeadRef)
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range previousHead.TaskHeads {
		after, ok := evidenceSubjectHead(currentHead.TaskHeads, subject.SubjectID)
		if !ok {
			t.Fatalf("source import dropped the published Task subject %s", subject.SubjectID)
		}
		if evidenceRefsEqual(after.RevisionRef, subject.RevisionRef) {
			continue
		}
		revision, err := f.store.LoadTaskIndexRevision(after.RevisionRef)
		if err != nil || revision.PreviousRevision == nil || !evidenceRefsEqual(*revision.PreviousRevision, subject.RevisionRef) {
			t.Fatalf("source import broke the Task index chain for %s: %#v %v", subject.SubjectID, revision, err)
		}
	}
	subject, published, err := f.store.publishedTaskSubjectRevision(result.Head, source.Attempt.SemanticTaskRef)
	if err != nil || !published {
		t.Fatalf("source import did not publish the revised Task index: %v", err)
	}
	revision, err := f.store.LoadTaskIndexRevision(subject)
	if err != nil {
		t.Fatal(err)
	}
	if len(revision.SourceRevisions) != 1 {
		t.Fatalf("revised Task index must reference its source revision: %#v", revision)
	}
	if previous, ok, err := f.store.publishedTaskSubjectRevision(source.Head, source.Attempt.SemanticTaskRef); err == nil && ok {
		prior, err := f.store.LoadTaskIndexRevision(previous)
		if err != nil {
			t.Fatal(err)
		}
		if len(revision.AttemptSeals) != len(prior.AttemptSeals) || len(revision.Finalizations) != len(prior.Finalizations) {
			t.Fatalf("source revision dropped prior Task index records: %#v", revision)
		}
	}
}
