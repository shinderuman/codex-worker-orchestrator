package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func amendHeldTaskMetadata(t *testing.T, store *Store, policy PublicationPolicy, marker string) string {
	t.Helper()
	root := store.identity.PrimaryRoot
	taskPath := filepath.Join("IMPLEMENTATION_TASKS", "root.md")
	current, err := os.ReadFile(filepath.Join(root, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, taskPath), []byte(string(current)+marker), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", taskPath)
	runControllerGit(t, root, "commit", "-qm", "parent metadata amendment")
	runControllerGit(t, root, "push", "origin", "HEAD:"+policy.RemoteRef)
	return controllerGitOutput(t, root, "rev-parse", "HEAD")
}

func amendHeldTaskMetadataWithSource(t *testing.T, store *Store, policy PublicationPolicy) string {
	t.Helper()
	root := store.identity.PrimaryRoot
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("external source change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", "source.txt")
	runControllerGit(t, root, "commit", "-qm", "mixed advancement")
	runControllerGit(t, root, "push", "origin", "HEAD:"+policy.RemoteRef)
	return controllerGitOutput(t, root, "rev-parse", "HEAD")
}

func amendmentRebindApproval(t *testing.T, store *Store, remote string) TaskAmendmentApproval {
	t.Helper()
	authority, err := ResolveCommittedTaskAuthorityAt(store.identity.PrimaryRoot, remote)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	return TaskAmendmentApproval{
		RemoteOID:           remote,
		OldRootTaskRef:      *head.RootTaskRef,
		NewRootTaskRef:      authority.Task,
		OldExecutionTaskRef: *head.ExecutionTaskRef,
		NewExecutionTaskRef: authority.Task,
	}
}

func acceptedAmendmentLane(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, RepositoryControllerHead, AcceptedCandidate) {
	t.Helper()
	f, p := newPublicationTestFixture(t)
	source := publicationTestEdit(t, f, "result.txt", "task result\n")
	accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "task result", Policy: p,
		Evidence: publicationTestEvidence(t, f.store, source, p),
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	return f, p, accepted.Head, c
}

func TestApprovedTaskAmendmentRebindCompletesLifecycle(t *testing.T) {
	t.Parallel()
	f, p, head, old := acceptedAmendmentLane(t)
	marker := "\n## Amendments\n\nparent-approved semantic delta\n"
	remote := amendHeldTaskMetadata(t, f.store, p, marker)
	authority, err := ResolveCommittedTaskAuthorityAt(f.store.identity.PrimaryRoot, remote)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Task.Equal(*head.RootTaskRef) || authority.Task.TaskPath != head.RootTaskRef.TaskPath {
		t.Fatalf("amendment did not change the held Task digest at the same path: %#v", authority.Task)
	}
	approval := amendmentRebindApproval(t, f.store, remote)
	rebound, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID, Amendment: &approval})
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.Head.RootTaskRef.Equal(authority.Task) || !rebound.Head.ExecutionTaskRef.Equal(authority.Task) {
		t.Fatalf("approved amendment did not update held authority: %#v", rebound.Head)
	}
	if rebound.Head.IntegrationTip != remote || rebound.Head.ObservedRemoteTip != remote || rebound.Head.ObservedPrefix != head.ObservedPrefix {
		t.Fatalf("approved amendment observation differs: %#v", rebound.Head)
	}
	c, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if c.State != candidatePrepared || c.EvidenceValid || len(c.Evidence) != 0 || c.BaseOID != remote {
		t.Fatalf("approved amendment revived candidate evidence: %#v", c)
	}
	if c.TaskRef.Equal(authority.Task) {
		t.Fatal("rebound candidate changed its sealed Task provenance")
	}
	preserved, err := f.store.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil || preserved.CommitOID != old.CommitOID || !preserved.TaskRef.Equal(old.TaskRef) {
		t.Fatalf("superseded candidate changed: %#v %v", preserved, err)
	}
	if _, err = f.store.BuildAttemptEvidenceBundle(old.SealRef); err != nil {
		t.Fatal(err)
	}
	entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if live.Admission == nil || !live.Admission.Attempt.SemanticTaskRef.Equal(authority.Task) || live.Admission.Attempt.SemanticTaskRef.TaskPath != old.TaskRef.TaskPath {
		t.Fatalf("materialization did not bind the amended authority: %#v", live.Admission)
	}
	if live.Admission.Attempt.PredecessorAttemptID != old.AttemptID {
		t.Fatal("materialization lost the predecessor attempt")
	}
	laneTask, err := os.ReadFile(filepath.Join(live.Admission.Workspace.Root, "IMPLEMENTATION_TASKS", "root.md"))
	if err != nil || !strings.Contains(string(laneTask), "parent-approved semantic delta") {
		t.Fatalf("amended contract did not reach the execution lane: %v", err)
	}
	f.source = *live.Admission
	final := publicationTestEdit(t, f, "result.txt", "amended task result\n")
	f.source = final
	published, candidate := publishTerminalTestSource(t, f, p, final)
	retired, err := f.store.RetireTerminalTask(TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	if retired.Head.ExecutionTaskRef != nil || retired.Head.RootTaskRef == nil || retired.Head.RootTaskRef.TaskPath != "IMPLEMENTATION_TASKS/finding-child.md" {
		t.Fatalf("terminal retirement did not advance to the successor: %#v", retired.Head)
	}
	if _, err = f.store.BuildAttemptEvidenceBundle(old.SealRef); err != nil {
		t.Fatal(err)
	}
}

func TestApprovedTaskAmendmentRebindRejectsUnapprovedShapes(t *testing.T) {
	cases := []struct {
		name     string
		advance  func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string
		approval func(t *testing.T, f findingAcceptanceFixture, head RepositoryControllerHead, remote string) *TaskAmendmentApproval
		message  string
	}{
		{
			name: "no approval",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\nunapproved delta\n")
			},
			approval: func(_ *testing.T, _ findingAcceptanceFixture, _ RepositoryControllerHead, _ string) *TaskAmendmentApproval {
				return nil
			},
			message: "external advancement changes root Task authority",
		},
		{
			name: "approved remote differs",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\nfirst delta\n")
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, head RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, remote)
				approval.RemoteOID = head.IntegrationTip
				return &approval
			},
			message: "approved Task amendment remote differs from the observed advancement",
		},
		{
			name: "remote advanced beyond approval",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\nfirst delta\n")
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\nsecond delta\n")
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, head RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, head.IntegrationTip)
				first, err := ResolveCommittedTaskAuthorityAt(f.store.identity.PrimaryRoot, remote+"~1")
				if err != nil {
					t.Fatal(err)
				}
				approval.RemoteOID = controllerGitOutput(t, f.store.identity.PrimaryRoot, "rev-parse", remote+"~1")
				approval.NewRootTaskRef = first.Task
				approval.NewExecutionTaskRef = first.Task
				return &approval
			},
			message: "approved Task amendment remote differs from the observed advancement",
		},
		{
			name: "old authority mismatch",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\ndelta\n")
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, _ RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, remote)
				approval.OldRootTaskRef.ContractDigest = strings.Repeat("a", 64)
				return &approval
			},
			message: "approved Task amendment old authority differs from the controller head",
		},
		{
			name: "new authority mismatch",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\ndelta\n")
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, _ RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, remote)
				approval.NewRootTaskRef.ContractDigest = strings.Repeat("b", 64)
				return &approval
			},
			message: "approved Task amendment new root authority differs from the committed Project",
		},
		{
			name: "task authority path change",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\ndelta\n")
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, _ RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, remote)
				approval.NewRootTaskRef.TaskPath = "IMPLEMENTATION_TASKS/finding-child.md"
				approval.NewExecutionTaskRef.TaskPath = "IMPLEMENTATION_TASKS/finding-child.md"
				return &approval
			},
			message: "approved Task amendment changes Task authority path",
		},
		{
			name: "non parent metadata advancement",
			advance: func(t *testing.T, f findingAcceptanceFixture, p PublicationPolicy) string {
				return amendHeldTaskMetadataWithSource(t, f.store, p)
			},
			approval: func(t *testing.T, f findingAcceptanceFixture, _ RepositoryControllerHead, remote string) *TaskAmendmentApproval {
				approval := amendmentRebindApproval(t, f.store, remote)
				return &approval
			},
			message: "external advancement changes non-parent metadata",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, p, head, old := acceptedAmendmentLane(t)
			remote := tc.advance(t, f, p)
			approval := tc.approval(t, f, head, remote)
			_, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID, Amendment: approval})
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("unapproved amendment shape admitted: %v", err)
			}
			after, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if after.PendingTransitionID != "" || !after.RootTaskRef.Equal(*head.RootTaskRef) || !after.ExecutionTaskRef.Equal(*head.ExecutionTaskRef) || after.IntegrationTip != head.IntegrationTip {
				t.Fatalf("rejected amendment changed controller authority: %#v", after)
			}
		})
	}
}

func TestAdoptExternalAdvancementStillRejectsHeldTaskAmendment(t *testing.T) {
	t.Parallel()
	f, p, head, _ := acceptedAmendmentLane(t)
	amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\ndelta\n")
	if _, err := f.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: head.ControllerGeneration, Policy: p}); err == nil {
		t.Fatal("adopt admitted a held Task amendment")
	}
	after, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.ControllerGeneration != head.ControllerGeneration || !after.RootTaskRef.Equal(*head.RootTaskRef) || !after.ExecutionTaskRef.Equal(*head.ExecutionTaskRef) || after.IntegrationTip != head.IntegrationTip {
		t.Fatalf("rejected adopt changed controller authority: %#v", after)
	}
}

func TestApprovedTaskAmendmentRebindPreservesExecutionAuthority(t *testing.T) {
	t.Parallel()
	f, p := newPublicationTestFixture(t)
	episode := planSuspensionTestEpisode(t, f)
	suspended, err := f.store.SuspendExecution(f.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	scheduled, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	f.source = *scheduled.Admission
	source := publicationTestEdit(t, f, "child.txt", "child task result\n")
	f.source = source
	accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "child task result", Policy: p,
		Evidence: publicationTestEvidence(t, f.store, source, p),
	})
	if err != nil {
		t.Fatal(err)
	}
	head := accepted.Head
	if !head.ExecutionTaskRef.Equal(f.child) || head.RootTaskRef.Equal(*head.ExecutionTaskRef) {
		t.Fatalf("episode execution authority was not split from the root: %#v", head)
	}
	old, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	remote := amendHeldTaskMetadata(t, f.store, p, "\n## Amendments\n\nroot amendment while a child executes\n")
	authority, err := ResolveCommittedTaskAuthorityAt(f.store.identity.PrimaryRoot, remote)
	if err != nil {
		t.Fatal(err)
	}
	if authority.Task.TaskPath != head.RootTaskRef.TaskPath || authority.Task.Equal(*head.RootTaskRef) {
		t.Fatalf("root amendment did not change the held root digest at the same path: %#v", authority.Task)
	}
	child := *head.ExecutionTaskRef
	approval := TaskAmendmentApproval{
		RemoteOID:           remote,
		OldRootTaskRef:      *head.RootTaskRef,
		NewRootTaskRef:      authority.Task,
		OldExecutionTaskRef: child,
		NewExecutionTaskRef: child,
	}
	rebound, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID, Amendment: &approval})
	if err != nil {
		t.Fatal(err)
	}
	if !rebound.Head.RootTaskRef.Equal(authority.Task) {
		t.Fatalf("approved amendment did not advance the root authority: %#v", rebound.Head)
	}
	if !rebound.Head.ExecutionTaskRef.Equal(child) {
		t.Fatalf("approved amendment reassigned the execution authority to the root: %#v", rebound.Head.ExecutionTaskRef)
	}
	c, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: c.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := f.store.CleanupExecution(CleanupExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, WorkspaceID: scheduled.Admission.Workspace.ID, SealRef: c.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration, EpisodeID: rebound.Head.ActiveEpisodeID, EpisodeRevision: rebound.Head.ActiveEpisodeRevision, SuspensionID: entered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	if live.Admission == nil || !live.Admission.Attempt.SemanticTaskRef.Equal(child) || !live.Admission.Attempt.RootTaskRef.Equal(authority.Task) {
		t.Fatalf("materialization did not preserve the amended split authority: %#v", live.Admission)
	}
	reassigned := approval
	reassigned.NewExecutionTaskRef = authority.Task
	if _, err = f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: c.CandidateID, Amendment: &reassigned}); err == nil {
		t.Fatal("execution reassignment to the root authority was admitted")
	}
}
