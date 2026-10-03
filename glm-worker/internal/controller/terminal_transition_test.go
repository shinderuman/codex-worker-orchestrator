package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func publishTerminalTestSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
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
	published, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	return published, candidate
}

func TestTerminalRootRetirementAdvancesPlanAndKeepsPublishedEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "root result\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	if _, err := fixture.store.ObserveTerminalFinding(source.Attempt.AttemptID, published.Head.ProjectSnapshotID, FindingObservationInput{Producer: "test", ProofClass: FindingProofUnverified, ProblemKey: "before-retirement"}); err == nil {
		t.Fatal("publication alone became terminal completion")
	}
	input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
	retired, err := fixture.store.RetireTerminalTask(input)
	if err != nil {
		t.Fatal(err)
	}
	if retired.Head.RootTaskRef == nil || !retired.Head.RootTaskRef.Equal(fixture.child) || retired.Head.AcceptedCandidateRef != nil || retired.Head.PendingTerminalTaskRef != nil {
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
	if retired.Head.ObservedPrefix != candidate.CommitOID || retired.Head.IntegrationTip == candidate.CommitOID {
		t.Fatal("metadata retirement rewrote publication prefix")
	}
	if _, err := fixture.store.RecoverExecutionOperation(retired.TransitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RetireTerminalTask(input); err == nil {
		t.Fatal("stale retirement reused current authority")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.ObserveTerminalFinding(source.Attempt.AttemptID, retired.Head.ProjectSnapshotID, FindingObservationInput{Producer: "test", ProofClass: FindingProofUnverified, ProblemKey: "after-retirement"}); err != nil {
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
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	if _, err := fixture.store.ScheduleEpisodeWithProjectAuthority(episode.EpisodeID, published.Head.ActiveEpisodeRevision); err == nil {
		t.Fatal("scheduler escaped integrated/metadata-finalized boundary")
	}
	retired, err := fixture.store.RetireTerminalTask(TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if retired.Head.RootTaskRef.TaskPath != suspended.Head.RootTaskRef.TaskPath || retired.Head.RootTaskRef.Equal(*suspended.Head.RootTaskRef) {
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
	cleaned, err := fixture.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration, WorkspaceID: source.Workspace.ID, SealRef: candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: retired.Head.ActiveEpisodeRevision, SuspensionID: suspended.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || !resumed.Admission.Attempt.SemanticTaskRef.Equal(*retired.Head.RootTaskRef) || resumed.Admission.Attempt.PredecessorAttemptID != suspended.Suspension.AttemptID {
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
			published, candidate := publishTerminalTestSource(t, fixture, policy, source)
			input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
			op, head, err := fixture.store.planTerminalMetadata(input)
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
				t.Fatal(err)
			}
			if mode == "prepared-partial" {
				if err := fixture.store.applyTerminalMetadataFile(op.Terminal.Files[0], op.Transition.Effects[2:4]); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := fixture.store.applyTerminalMetadata(op); err != nil {
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

func TestTerminalRetirementRejectsArbitraryTaskStaleProjectAndDirtyMetadata(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "terminal result\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	input := TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef}
	invalid := []TerminalTaskInput{input, input, input}
	invalid[0].ProjectSnapshotID = source.Head.ProjectSnapshotID
	invalid[1].TaskRef = fixture.child
	invalid[2].ExpectedGeneration--
	for _, denied := range invalid {
		if _, err := fixture.store.RetireTerminalTask(denied); err == nil {
			t.Fatal("arbitrary/stale retirement admitted")
		}
	}
	writeSuspensionTestFile(t, fixture.store.identity.PrimaryRoot, candidate.TaskRef.TaskPath, "dirty owner metadata\n")
	if _, err := fixture.store.RetireTerminalTask(input); err == nil {
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

func TestTerminalRetirementRemoteRaceKeepsExactJournalAndPublication(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "published root\n")
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
	op, head, err := fixture.store.planTerminalMetadata(TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	remote := advancePublicationTestRemote(t, fixture, policy, "external.txt", "external advance\n")
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("retirement raced remote reported success")
	}
	after, err := fixture.store.LoadHead()
	if err != nil || after.PendingTransitionID != op.Transition.TransitionID || after.ProjectSnapshotID != head.ProjectSnapshotID || after.LiveLeaseID != "" {
		t.Fatal("retirement race lost source authority")
	}
	actual, err := fixture.store.observeTerminalFileEffects(op)
	if err != nil {
		t.Fatal(err)
	}
	for _, effect := range op.Transition.Effects[2:] {
		if actual[effect.Key()] != effect.ExpectedOld {
			t.Fatal("retirement race changed metadata")
		}
	}
	observed, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || observed != remote {
		t.Fatal("retirement rewrote raced remote")
	}
	if _, err := fixture.store.BuildAttemptEvidenceBundle(candidate.SealRef); err != nil {
		t.Fatal(err)
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
	published, candidate := publishTerminalTestSource(t, fixture, policy, source)
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
	if resumed.Admission == nil || !resumed.Admission.Attempt.SemanticTaskRef.Equal(candidate.TaskRef) || resumed.Admission.Lease.LeaseID == source.Lease.LeaseID || resumed.Head.ObservedPrefix != candidate.CommitOID {
		t.Fatal("nonterminal integrated blocker lost additive reentry authority")
	}
}
