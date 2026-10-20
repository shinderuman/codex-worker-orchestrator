package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func promoteTerminalTestSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	seedFinalValidationNoneObligation(t, fixture.store, source)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "terminal source result", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy)})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	return promoted, candidate
}

func prepareTerminalTestSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	promoted, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	return prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
}

func prepareCompletionFixture(store *Store, input TerminalTaskInput) (ExecutionOperationResult, error) {
	op, head, err := store.planTerminalMetadata(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := store.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	phase, err := store.loadTransitionState(op.Transition.TransitionID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := store.prepareCandidateCompletionWrite(op, &phase); err != nil {
		return ExecutionOperationResult{}, err
	}
	head, err = store.LoadHead()
	return ExecutionOperationResult{Head: head, TransitionID: op.Transition.TransitionID, CandidateRef: &op.Terminal.Record.CandidateRef}, err
}

func prepareCompletionTestCandidate(t *testing.T, store *Store, promoted ExecutionOperationResult, candidate AcceptedCandidate) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	retired, err := prepareCompletionFixture(store, TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err = store.LoadAcceptedCandidate(*retired.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return retired, candidate
}

func publishTerminalTestSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	retired, _ := prepareTerminalTestSource(t, fixture, policy, source)
	published, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*published.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return published, candidate
}

func TestTerminalRootRetirementAdvancesPlanAndKeepsPublishedEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	if _, err := fixture.store.ObserveTerminalFinding(source.Attempt.AttemptID, published.Head.ProjectSnapshotID, FindingObservationInput{Producer: "test", ProofClass: FindingProofUnverified, ProblemKey: "before-retirement"}); err == nil {
		t.Fatal("publication alone became terminal completion")
	}
	input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
	retired, err := prepareCompletionFixture(fixture.store, input)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Head.RootTaskRef == nil || !retired.Head.RootTaskRef.Equal(candidate.RootTaskRef) || retired.Head.AcceptedCandidateRef == nil || retired.Head.PendingTransitionID == "" || retired.Head.MetadataLineageRef != nil {
		t.Fatal("normal successor authority was not exact")
	}
	if _, err := os.Lstat(filepath.Join(fixture.store.identity.PrimaryRoot, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("terminal Task file not retired")
	}
	if got := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "ls-files", "--", candidate.TaskRef.TaskPath); got != "" {
		t.Fatal("completed Task remains tracked")
	}
	plan, err := os.ReadFile(filepath.Join(fixture.store.identity.PrimaryRoot, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	active, err := taskcontract.ParsePlanSchedule(string(plan)).ActiveTask()
	if err != nil || active != fixture.child.TaskPath {
		t.Fatal("Plan successor mismatch")
	}
	if retired.Head.ObservedRemoteTip != candidate.BaseOID || observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy) != candidate.BaseOID {
		t.Fatal("local retirement changed remote authority")
	}
	candidate, err = fixture.store.LoadAcceptedCandidate(*retired.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if got := controllerGitOutput(t, fixture.store.identity.PrimaryRoot, "rev-list", "--count", candidate.BaseOID+".."+candidate.CommitOID); got != "1" {
		t.Fatal("one Task published more than one commit")
	}
	implementation, err := fixture.store.completionImplementation(candidate)
	if err != nil || implementation.CommitOID != candidate.CommitOID || implementation.ImplementationRef != nil {
		t.Fatal("completion implementation lineage was not preserved")
	}
	if _, err := fixture.store.LoadAcceptedCandidate(*candidate.ImplementationRef); err != nil {
		t.Fatal("implementation proof is unavailable after retirement")
	}
	if _, err := fixture.store.RecoverExecutionOperation(retired.TransitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareCompletionFixture(fixture.store, input); err == nil {
		t.Fatal("stale retirement reused current authority")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ObserveTerminalFinding(source.Attempt.AttemptID, retired.Head.ProjectSnapshotID, FindingObservationInput{Producer: "test", ProofClass: FindingProofUnverified, ProblemKey: "before-publication"}); err == nil {
		t.Fatal("local retirement became whole Task completion before publication")
	}
	completed, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ObserveTerminalFinding(source.Attempt.AttemptID, completed.Head.ProjectSnapshotID, FindingObservationInput{Producer: "test", ProofClass: FindingProofUnverified, ProblemKey: "after-retirement"}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalBlockerRetirementFulfillsMetadataAndResumesBoundRoot(t *testing.T) {
	t.Parallel()
	fixture := newFindingAcceptanceFixture(t, "IMPLEMENTATION_TASKS/unrelated.md")
	repo := fixture.source.Workspace.Root
	content, err := os.ReadFile(filepath.Join(repo, fixture.source.Attempt.SemanticTaskRef.TaskPath))
	if err != nil {
		t.Fatal(err)
	}
	content = append(content, []byte("\n## Fulfilled dependencies\n\nnone\n")...)
	content = replaceTerminalTestDependency(content, fixture.child.TaskPath)
	writeSuspensionTestFile(t, repo, fixture.source.Attempt.SemanticTaskRef.TaskPath, string(content))
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "bind root prerequisite")
	fixture.store, err = Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(repo, fixture.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	fixture.source, err = fixture.store.BootstrapExecution(authority.Task, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	seedFinalValidationNoneObligation(t, fixture.store, fixture.source)
	fixture, policy := configurePublicationTestFixture(t, fixture)
	fixture.source = publicationTestEdit(t, fixture, "root-only.txt", "unfinished root\n")
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	child, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *child.Admission
	source := publicationTestEdit(t, fixture, "child.txt", "blocker result\n")
	published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	if _, err := fixture.store.ScheduleEpisodeWithProjectAuthority(episode.EpisodeID, published.Head.ActiveEpisodeRevision); err == nil {
		t.Fatal("scheduler escaped integrated/metadata-finalized boundary")
	}
	retired, err := prepareCompletionFixture(fixture.store, TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if !retired.Head.RootTaskRef.Equal(*suspended.Head.RootTaskRef) || retired.Head.MetadataLineageRef != nil || retired.Head.PendingTransitionID == "" {
		t.Fatal("retirement lost focus or failed to bind exact new dependency contract")
	}
	if fixture.store.metadataTaskBindingProven(*suspended.Head.RootTaskRef, fixture.child) {
		t.Fatal("mapping guessed unrelated task identity")
	}
	rootContent, err := os.ReadFile(filepath.Join(repo, retired.Head.RootTaskRef.TaskPath))
	if err != nil {
		t.Fatal(err)
	}
	deps, err := taskcontract.ParseTaskDependencyState(rootContent)
	if err != nil || len(deps.Outstanding) != 0 || !reflect.DeepEqual(deps.Fulfilled, []string{candidate.TaskRef.TaskPath}) {
		t.Fatal("inbound dependency retirement mismatch")
	}
	candidate, err = fixture.store.LoadAcceptedCandidate(*retired.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	completed, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := fixture.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: completed.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: completed.Head.ActiveEpisodeRevision, SuspensionID: suspended.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || !resumed.Admission.Attempt.SemanticTaskRef.Equal(*completed.Head.RootTaskRef) || resumed.Admission.Attempt.PredecessorAttemptID != suspended.Suspension.AttemptID {
		t.Fatal("fresh root successor lacks exact terminal binding")
	}
	if _, err := os.Stat(filepath.Join(resumed.Admission.Workspace.Root, "root-only.txt")); err != nil {
		t.Fatal("retirement lost unfinished root data")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
}

func replaceTerminalTestDependency(content []byte, dependency string) []byte {
	return []byte(strings.Replace(string(content), "## Dependencies\n\nnone", "## Dependencies\n\n- `"+dependency+"`", 1))
}

func TestTerminalRetirementRecoveryClassifiesExactPartialAndFinalizedPhases(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"prepared-partial", "committed-phase-lost", "finalizing", "finalized-phase-lost"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "result.txt", "terminal result\n")
			published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
			input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
			op, head, err := fixture.store.planTerminalMetadata(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
				t.Fatal(err)
			}
			if mode == "prepared-partial" {
				if err := fixture.store.applyTerminalMetadataFile(op.Terminal.Files[0], op.Transition.Effects[1:3]); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := fixture.store.applyCandidateCompletion(op); err != nil {
					t.Fatal(err)
				}
				phase, err := fixture.store.loadTransitionState(op.Transition.TransitionID)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "committed-phase-lost":
					phase.Phase = TransitionPhasePrepared
				case "finalizing":
					phase.Phase = TransitionPhaseFinalizing
				case "finalized-phase-lost":
					if _, err := fixture.store.finalizeAuthorityTransitionLocked(op.Transition); err != nil {
						t.Fatal(err)
					}
					phase.Phase = TransitionPhaseFinalizing
				}
				if err := fixture.store.writeTransitionState(phase); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := Open(controllerTestConfig(fixture.store.identity.PrimaryRoot, filepath.Join(filepath.Dir(filepath.Dir(fixture.store.dir)), "sessions")))
			if err != nil {
				t.Fatal(err)
			}
			if reopened.dir != fixture.store.dir {
				t.Fatal("reopened controller identity changed")
			}
			result, err := reopened.RecoverExecutionOperation(op.Transition.TransitionID)
			if err != nil {
				t.Fatal(err)
			}
			_, phase, err := fixture.store.LoadTransition(op.Transition.TransitionID)
			if err != nil || phase.Phase != TransitionPhaseFinalized || result.Head.PendingTransitionID != "" {
				t.Fatal("recovery failed to finalize exact target")
			}
			if proven, err := fixture.store.terminalAttemptProven(source.Attempt); err != nil || !proven {
				t.Fatal("recovered publication did not prove whole Task completion")
			}
			completion, err := fixture.store.LoadAcceptedCandidate(*result.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: result.Head.ControllerGeneration, CandidateID: completion.CandidateID}); err == nil {
				t.Fatal("finalized Task admitted another publication operation")
			}
			proven, err := fixture.store.terminalAttemptProven(source.Attempt)
			if err != nil || !proven {
				t.Fatal("finalization proof missing after recovery")
			}
			if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalRetirementPartialMetadataWindowFailsBootstrapUntilJournalRecovery(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "terminal result\n")
	published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	op, head, err := fixture.store.planTerminalMetadata(TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.applyTerminalMetadataLocalRef(op); err != nil {
		t.Fatal(err)
	}
	for index := range op.Terminal.Files {
		if err := fixture.store.applyTerminalMetadataFile(op.Terminal.Files[index], op.Transition.Effects[1+index*2:3+index*2]); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := WorkflowAuthorityTask(fixture.store.config); err == nil {
		t.Fatal("partially retired Metadata admitted canonical bootstrap authority")
	}
	if _, err := os.Lstat(filepath.Join(fixture.store.identity.PrimaryRoot, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("partial retirement did not remove the completed Task Metadata")
	}
	recoveryHead, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if recoveryHead.PendingTransitionID != op.Transition.TransitionID {
		t.Fatalf("partial retirement lost its journal recovery staging: %#v", recoveryHead)
	}
	if _, phase, err := fixture.store.LoadTransition(op.Transition.TransitionID); err != nil || phase.Phase != TransitionPhasePrepared {
		t.Fatalf("partial retirement journal phase: %#v %v", phase, err)
	}
	reopened, err := Open(controllerTestConfig(fixture.store.identity.PrimaryRoot, filepath.Join(filepath.Dir(filepath.Dir(fixture.store.dir)), "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	if reopened.dir != fixture.store.dir {
		t.Fatal("reopened controller identity changed")
	}
	result, err := reopened.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Head.PendingTransitionID != "" || result.Head.ExecutionTaskRef != nil || result.Head.RootTaskRef == nil || !result.Head.RootTaskRef.Equal(fixture.child) {
		t.Fatalf("recovered retirement lost the pending publication authority: %#v", result.Head)
	}
	authority, canonical, err := WorkflowAuthorityTask(fixture.store.config)
	if err != nil || !canonical || !authority.Equal(fixture.child) {
		t.Fatalf("recovered retirement bootstrap authority = %#v canonical=%v err=%v", authority, canonical, err)
	}
	if result.Head.MetadataLineageRef == nil || result.Head.AcceptedCandidateRef != nil {
		t.Fatal("recovered completion did not close publication authority")
	}

	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalRetirementRejectsArbitraryTaskStaleProjectAndDirtyMetadata(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "terminal result\n")
	published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
	invalid := []TerminalTaskInput{input, input, input}
	invalid[0].ProjectSnapshotID = "different-project"
	invalid[1].TaskRef = fixture.child
	invalid[2].ExpectedGeneration--
	for _, denied := range invalid {
		if _, err := prepareCompletionFixture(fixture.store, denied); err == nil {
			t.Fatal("arbitrary/stale retirement admitted")
		}
	}
	writeSuspensionTestFile(t, fixture.store.identity.PrimaryRoot, candidate.TaskRef.TaskPath, "dirty owner metadata\n")
	if _, err := prepareCompletionFixture(fixture.store, input); err == nil {
		t.Fatal("dirty terminal metadata overwritten")
	}
	after, err := fixture.store.LoadHead()
	if err != nil || !reflect.DeepEqual(after, published.Head) {
		t.Fatal("preflight failure changed authority")
	}
	data, err := os.ReadFile(filepath.Join(fixture.store.identity.PrimaryRoot, candidate.TaskRef.TaskPath))
	if err != nil || string(data) != "dirty owner metadata\n" {
		t.Fatal("dirty owner data lost")
	}
}

func TestIntegratedBlockerBeforeRetirementReentersSameTaskWithFreshLease(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	child, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *child.Admission
	source := publicationTestEdit(t, fixture, "child.txt", "integrated child\n")
	published, candidate := promoteTerminalTestSource(t, fixture, policy, source)
	progress, err := fixture.store.LoadEpisodeRevision(episode.EpisodeID, published.Head.ActiveEpisodeRevision)
	if err != nil || taskPathSatisfied(progress.SatisfiedTaskRefs, candidate.TaskRef.TaskPath) {
		t.Fatal("integration alone fulfilled terminal metadata dependency")
	}
	reentered, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: published.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := fixture.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: reentered.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: progress.Revision, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || !resumed.Admission.Attempt.SemanticTaskRef.Equal(candidate.TaskRef) || resumed.Admission.Lease.LeaseID == source.Lease.LeaseID || resumed.Head.ObservedPrefix != candidate.BaseOID {
		t.Fatal("nonterminal integrated blocker lost additive reentry authority")
	}
}
