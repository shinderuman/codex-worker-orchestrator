package controller

import (
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
)

func (s *Store) RetireTerminalTask(input TerminalTaskInput) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planTerminalMetadata(input)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planTerminalMetadata(input TerminalTaskInput) (ExecutionOperation, RepositoryControllerHead, error) {
	head, c, candidateRef, err := s.publicationCandidateAuthority(PublicationInput{ExpectedGeneration: input.ExpectedGeneration, CandidateID: input.CandidateID})
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	project, err := s.terminalSourceProject(input, head, c)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	committed, err := repositoryprojectcommit.Load(s.identity.PrimaryRoot, head.IntegrationTip)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	blocker := !c.TaskRef.Equal(c.RootTaskRef)
	metadata, err := committed.RetireTerminalMetadata(c.TaskRef.TaskPath, blocker)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	return s.buildTerminalMetadataOperation(head, c, candidateRef, project, metadata)
}

func (s *Store) buildTerminalMetadataOperation(head RepositoryControllerHead, c AcceptedCandidate, candidateRef EvidenceObjectRef, project ProjectSnapshot, metadata repositoryprojectcommit.TerminalMetadata) (ExecutionOperation, RepositoryControllerHead, error) {
	record, err := executionTransition(head, terminalRetire)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	files, tree, err := s.terminalMetadataTree(head.IntegrationTip, metadata)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	commit, err := createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, head.IntegrationTip, "retire terminal Task metadata: "+c.TaskRef.TaskPath+"\n")
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	result, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, commit)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	archive, _, err := s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "terminal:"+record.TransitionID, []string{commit})
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	terminal := TerminalTaskRecord{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, TransitionID: record.TransitionID, ControllerGeneration: record.CommittedGeneration, TaskRef: c.TaskRef, AttemptID: c.AttemptID, CandidateRef: candidateRef, CandidateOID: c.CommitOID, SourceProject: project, ResultProject: result.Snapshot, GitArchive: archive, Previous: head.MetadataLineageRef, Bindings: metadataTaskBindings(project, result.Snapshot)}
	ref, err := s.putEvidenceJSON("terminal-task", record.TransitionID, terminal)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	payload := &TerminalMetadataOperation{Record: terminal, RecordRef: ref, Policy: c.Policy, Files: files, RootTaskRef: &result.Task}
	record.ProjectSnapshotNew = result.ProjectSnapshotID
	record.SourceRootTaskRef = c.RootTaskRef
	record.TargetRootTaskRef = result.Task
	record.SourceExecutionTaskRef = c.TaskRef
	record.TargetExecutionTaskRef = result.Task
	record.SourceAttemptID = c.AttemptID
	if err := s.planTerminalMetadataEpisode(head, payload, &record, c.TaskRef); err != nil {
		return ExecutionOperation{}, head, err
	}
	workspace, err := ResolveWorkspaceIdentity(s.identity.PrimaryRoot, s.identity)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	record.SourceWorkspaceID = workspace.ID
	record.Effects = terminalMetadataEffects(*payload)
	op := ExecutionOperation{Transition: record, Terminal: payload, SealRef: &c.SealRef, Workspace: &workspace}
	if err := s.verifyTerminalMetadataSource(op); err != nil {
		return ExecutionOperation{}, head, err
	}
	return op, head, nil
}

func (s *Store) terminalMetadataTree(base string, metadata repositoryprojectcommit.TerminalMetadata) ([]TerminalMetadataFile, string, error) {
	index, err := newSuspensionIndex(s.identity.PrimaryRoot, base)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = os.Remove(index) }()
	files := make([]TerminalMetadataFile, 0, len(metadata.Changes))
	for _, change := range metadata.Changes {
		newBytes := change.NewBytes
		if change.Delete {
			newBytes = nil
		}
		file, err := s.planTerminalMetadataFile(base, change.Path, index, newBytes)
		if err != nil {
			return nil, "", err
		}
		files = append(files, file)
	}
	tree, err := suspensionGitText(s.identity.PrimaryRoot, index, nil, "write-tree")
	return files, strings.TrimSpace(tree), err
}

func (s *Store) terminalSourceProject(input TerminalTaskInput, head RepositoryControllerHead, c AcceptedCandidate) (ProjectSnapshot, error) {
	if err := validateTerminalTaskInput(input, head, c); err != nil {
		return ProjectSnapshot{}, err
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	actual, err := ResolveCommittedTaskAuthorityAt(s.identity.PrimaryRoot, head.IntegrationTip)
	if err != nil || actual.ProjectSnapshotID != project.SnapshotID {
		return ProjectSnapshot{}, fmt.Errorf("terminal source ProjectSnapshot is stale: %w", err)
	}
	remote, err := s.observeCandidateRemote(head, c)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	local, _, err := readExecutionRef(s.identity.PrimaryRoot, c.Policy.LocalRef)
	if err != nil || local != head.IntegrationTip || remote != head.IntegrationTip {
		return ProjectSnapshot{}, fmt.Errorf("terminal source local/remote tip is stale")
	}

	return project, nil
}

func metadataTaskBindings(project, result ProjectSnapshot) []MetadataTaskBinding {
	var bindings []MetadataTaskBinding
	for _, old := range project.Tasks {
		if next, ok := findTaskRef(result.Tasks, old.TaskPath); ok && !next.Equal(old) {
			bindings = append(bindings, MetadataTaskBinding{Source: old, Result: next})
		}
	}
	return bindings
}

func (s *Store) planTerminalMetadataFile(base, path, index string, newBytes []byte) (TerminalMetadataFile, error) {
	old, err := runGitBinary(s.identity.PrimaryRoot, nil, "ls-tree", "-z", base, "--", path)
	if err != nil {
		return TerminalMetadataFile{}, err
	}
	entries, err := parseWorkspaceEntries(old, false)
	if err != nil || len(entries) != 1 || (entries[0].mode != "100644" && entries[0].mode != "100755") {
		return TerminalMetadataFile{}, fmt.Errorf("terminal metadata path is not one regular tracked file: %s", path)
	}
	data, err := readCommittedObject(s.identity.PrimaryRoot, base, path)
	if err != nil {
		return TerminalMetadataFile{}, err
	}
	file := TerminalMetadataFile{Path: path, Mode: entries[0].mode, OldOID: entries[0].oid, OldBytes: data}
	file.NewBytes = newBytes
	if file.NewBytes == nil {
		_, err = suspensionGit(s.identity.PrimaryRoot, index, nil, "update-index", "--force-remove", "--", path)
	} else {
		file.NewOID, err = suspensionGitText(s.identity.PrimaryRoot, index, file.NewBytes, "hash-object", "-w", "--stdin")
		if err == nil {
			err = writeSuspensionEntries(s.identity.PrimaryRoot, index, []workspaceTreeEntry{{mode: file.Mode, oid: file.NewOID, path: path}})
		}
	}
	if err != nil {
		return TerminalMetadataFile{}, err
	}
	return file, nil
}

func validateTerminalTaskInput(input TerminalTaskInput, head RepositoryControllerHead, c AcceptedCandidate) error {
	if input.ProjectSnapshotID != head.ProjectSnapshotID || !input.TaskRef.Equal(c.TaskRef) || c.State != candidateObserved || head.PendingTerminalTaskRef == nil || !head.PendingTerminalTaskRef.Equal(c.TaskRef) {
		return fmt.Errorf("terminal retirement requires exact integrated Task/candidate/project authority")
	}
	if head.LiveLeaseID != "" || head.IntegrationTip != head.ObservedRemoteTip {
		return fmt.Errorf("terminal retirement requires quiescent adopted integration authority")
	}
	if !c.EvidenceValid || (head.IntegrationTip != c.CommitOID && (c.DescendantTip != head.IntegrationTip || len(c.DescendantEvidence) == 0)) {
		return fmt.Errorf("terminal descendant lacks current validation evidence")
	}
	return nil
}

func (s *Store) planTerminalMetadataEpisode(head RepositoryControllerHead, payload *TerminalMetadataOperation, record *TransitionRecord, task SemanticTaskRef) error {
	if head.ActiveEpisodeID != "" {
		previous, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
		if err != nil {
			return err
		}
		next, err := progressedEpisodeRevision(previous, RepositoryControllerHead{RootTaskRef: payload.RootTaskRef, ControllerGeneration: head.ControllerGeneration}, payload.Record.ResultProject, EpisodeSatisfactionInput{SatisfiedTaskRef: task})
		if err != nil {
			return err
		}
		payload.Episode = &next
		record.TargetEpisodeRevision = next.Revision
	}
	return nil
}
