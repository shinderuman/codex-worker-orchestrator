package controller

import (
	"fmt"
	"os"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ParentSourceRevision struct {
	Snapshot     WorkspaceSnapshot `json:"snapshot"`
	AuthorityOID string            `json:"authority_oid"`
	Archive      EvidenceObjectRef `json:"archive"`
}

func (s *Store) RebindParentSourceCandidate(input PublicationInput, sourceSnapshotID string) (ExecutionOperationResult, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	defer func() { _ = lock.Close() }()
	op, head, err := s.planParentSourceRebind(input, sourceSnapshotID)
	if err != nil {
		return ExecutionOperationResult{}, err
	}
	if err := s.prepareExecutionOperation(&op, head); err != nil {
		return ExecutionOperationResult{}, err
	}
	return s.recoverExecutionOperationLocked(op)
}

func (s *Store) planParentSourceRebind(input PublicationInput, sourceSnapshotID string) (ExecutionOperation, RepositoryControllerHead, error) {
	head, candidate, ref, err := s.publicationCandidateAuthority(input)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if candidate.State != candidatePrepared || head.ActiveEpisodeID != "" || head.RootTaskRef == nil || head.ExecutionTaskRef == nil {
		return ExecutionOperation{}, head, fmt.Errorf("parent source revision requires an unpromoted sealed root candidate")
	}
	remote, err := s.unpublishedRebindRemote(head, candidate)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	source, authority, err := s.captureParentSourceRevision(sourceSnapshotID, remote)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	if authority.Task.TaskPath != head.RootTaskRef.TaskPath || authority.Task.TaskPath != head.ExecutionTaskRef.TaskPath {
		return ExecutionOperation{}, head, fmt.Errorf("parent source revision changes the active Task path")
	}
	candidate, err = s.parentSourceCandidate(candidate, source, authority, remote)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	op, err := s.planCandidateRevision(head, candidate, ref, publicationRebind)
	if err != nil {
		return ExecutionOperation{}, head, err
	}
	approval := TaskAmendmentApproval{OldRootTaskRef: *head.RootTaskRef, OldExecutionTaskRef: *head.ExecutionTaskRef, NewRootTaskRef: authority.Task, NewExecutionTaskRef: authority.Task}
	applyRebindOperationPlan(&op, head, candidate, authority.Snapshot, remote, "", "", &approval)
	op.Publication.ParentSource = &source
	return op, head, nil
}

func (s *Store) parentSourceCandidate(candidate AcceptedCandidate, source ParentSourceRevision, authority CommittedTaskAuthority, remote string) (AcceptedCandidate, error) {
	tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", source.AuthorityOID+"^{tree}")
	if err != nil {
		return candidate, err
	}
	candidate.BaseOID, candidate.TreeOID = remote, tree
	candidate.CommitOID, err = createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, remote, candidate.Message)
	if err != nil {
		return candidate, err
	}
	candidate.ProjectSnapshotID = authority.ProjectSnapshotID
	candidate.SnapshotID = digestStrings("parent-source-revision", candidate.SnapshotID, source.Snapshot.ID, tree)
	candidate.EvidenceValid, candidate.Evidence = false, nil
	candidate.GitArchive, _, err = s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "candidate:"+candidate.CommitOID, []string{candidate.CommitOID})
	return candidate, err
}

func (s *Store) captureParentSourceRevision(snapshotID, remote string) (ParentSourceRevision, CommittedTaskAuthority, error) {
	repo := s.identity.PrimaryRoot
	snapshot, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	if snapshot.ID != snapshotID || snapshot.Head != remote {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, fmt.Errorf("parent source revision snapshot or remote HEAD differs")
	}
	tree, err := captureParentSourceTree(repo)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	commit, err := suspensionTreeCommit(repo, tree, remote)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	authority, err := ResolveCommittedTaskAuthorityAt(repo, commit)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	ref, _, err := s.CaptureGitObjectArchive(repo, "parent-source:"+snapshot.ID, []string{commit})
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	after, err := CaptureWorkspaceSnapshot(repo)
	if err != nil {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, err
	}
	if after != snapshot {
		return ParentSourceRevision{}, CommittedTaskAuthority{}, fmt.Errorf("parent source changed during capture")
	}
	return ParentSourceRevision{Snapshot: snapshot, AuthorityOID: commit, Archive: ref}, authority, nil
}

func captureParentSourceTree(repo string) (string, error) {
	entries, err := suspensionIndexEntries(repo)
	if err != nil {
		return "", err
	}
	entries, err = captureSuspensionWorktree(repo, entries)
	if err != nil {
		return "", err
	}
	parent, err := captureParentWorktreeEntries(repo)
	if err != nil {
		return "", err
	}
	entries = append(entries, parent...)
	index, err := newSuspensionIndex(repo, "")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(index) }()
	if err := writeSuspensionEntries(repo, index, entries); err != nil {
		return "", err
	}
	return suspensionGitText(repo, index, nil, "write-tree")
}

func captureParentWorktreeEntries(repo string) ([]workspaceTreeEntry, error) {
	args := append([]string{"ls-files", "--cached", "--others", "--exclude-standard", "-z", "--"}, state.ParentManagedRootPaths()...)
	data, err := runGitBinary(repo, nil, args...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var entries []workspaceTreeEntry
	for _, path := range strings.Split(string(data), "\x00") {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		entry, exists, err := captureSuspensionEntry(repo, workspaceTreeEntry{path: path})
		if err != nil {
			return nil, err
		}
		if exists {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (s *Store) validateParentSourceRevision(op ExecutionOperation, candidate AcceptedCandidate) error {
	source := op.Publication.ParentSource
	if source == nil {
		return nil
	}
	if op.Transition.Kind != publicationRebind || op.Publication.Project == nil || source.Snapshot.ID != workspaceSnapshotID(source.Snapshot) || source.Snapshot.Head != op.Publication.RemoteOID || source.AuthorityOID != op.Publication.Project.HeadOID {
		return fmt.Errorf("parent source revision authority is incomplete")
	}
	return s.validateParentSourceArchive(*source, op.Publication.RemoteOID, candidate.TreeOID)
}

func (s *Store) validateParentSourceArchive(source ParentSourceRevision, remote, candidateTree string) error {
	roots, err := s.VerifyGitObjectArchive(source.Archive)
	if err != nil {
		return err
	}
	if len(roots) != 1 || roots[0].OID != source.AuthorityOID || roots[0].Type != gitCommitObjectType {
		return fmt.Errorf("parent source revision archive differs")
	}
	parent, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", source.AuthorityOID+"^")
	if err != nil {
		return err
	}
	tree, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", source.AuthorityOID+"^{tree}")
	if err != nil {
		return err
	}
	if parent != remote || tree != candidateTree {
		return fmt.Errorf("parent source revision tree or base differs")
	}
	return nil
}

func (s *Store) projectMetadataBase(id string) (string, error) {
	project, err := s.LoadProjectSnapshot(id)
	return project.HeadOID, err
}

func (s *Store) operationPublicationRefs(op ExecutionOperation, candidate AcceptedCandidate, lineage EvidenceObjectRef, observation *EvidenceObjectRef) ([]EvidenceObjectRef, error) {
	refs, err := s.candidatePublicationRefs(candidate, lineage, observation)
	if err != nil {
		return nil, err
	}
	source := op.Publication.ParentSource
	if source == nil {
		return refs, nil
	}
	ref, err := s.putEvidenceJSON("parent-source-revision", source.Snapshot.ID, source)
	if err != nil {
		return nil, err
	}
	return canonicalEvidenceRefs(append(refs, ref, source.Archive)), nil
}
