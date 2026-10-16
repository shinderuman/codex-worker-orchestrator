package controller

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
)

func (s *Store) completionMetadata(c AcceptedCandidate) (repositoryprojectcommit.TerminalMetadata, error) {
	committed, err := repositoryprojectcommit.Load(s.identity.PrimaryRoot, c.CommitOID)
	if err != nil {
		return repositoryprojectcommit.TerminalMetadata{}, err
	}
	return committed.RetireTerminalMetadata(c.TaskRef.TaskPath, !c.TaskRef.Equal(c.RootTaskRef))
}

func (s *Store) amendCandidateCompletion(c AcceptedCandidate, source EvidenceObjectRef) (AcceptedCandidate, error) {
	if c.State != candidatePromoted || c.ImplementationRef != nil || !c.EvidenceValid {
		return c, fmt.Errorf("completion amendment requires an unobserved validated implementation")
	}
	metadata, err := s.completionMetadata(c)
	if err != nil {
		return c, err
	}
	_, tree, err := s.terminalMetadataTree(c.CommitOID, metadata)
	if err != nil {
		return c, err
	}
	c.ImplementationRef = &source
	c.CommitOID, err = createAcceptedCandidateCommit(s.identity.PrimaryRoot, tree, c.BaseOID, c.Message)
	if err != nil {
		return c, err
	}
	c.TreeOID = tree
	c.GitArchive, _, err = s.CaptureGitObjectArchive(s.identity.PrimaryRoot, "candidate:"+c.CommitOID, []string{c.CommitOID})
	return c, err
}

func (s *Store) completionImplementation(c AcceptedCandidate) (AcceptedCandidate, error) {
	if c.ImplementationRef == nil {
		return c, nil
	}
	implementation, err := s.LoadAcceptedCandidate(*c.ImplementationRef)
	if err != nil {
		return implementation, err
	}
	if (c.State != candidatePromoted && c.State != candidateObserved) || implementation.ImplementationRef != nil || implementation.State != candidatePromoted || !implementation.EvidenceValid || !sameCompletionSubject(c, implementation) {
		return implementation, fmt.Errorf("completion implementation authority differs")
	}
	return implementation, s.validateCompletionTree(c, implementation)
}

func (s *Store) validateCompletionTree(c, implementation AcceptedCandidate) error {
	metadata, err := s.completionMetadata(implementation)
	if err != nil {
		return err
	}
	_, tree, err := s.terminalMetadataTree(implementation.CommitOID, metadata)
	if err != nil {
		return err
	}
	actual, err := gitTrimmed(s.identity.PrimaryRoot, "rev-parse", c.CommitOID+"^{tree}")
	if err != nil || c.TreeOID != tree || actual != tree {
		return fmt.Errorf("completion amendment contains changes outside generated terminal metadata: %w", err)
	}
	parent, err := gitTrimmed(s.identity.PrimaryRoot, "rev-list", "--parents", "-n", "1", c.CommitOID)
	if err != nil || parent != c.CommitOID+" "+implementation.BaseOID {
		return fmt.Errorf("completion amendment changed the implementation parent: %w", err)
	}
	return nil
}

func sameCompletionSubject(c, source AcceptedCandidate) bool {
	return c.AttemptID == source.AttemptID && c.TaskRef.Equal(source.TaskRef) && c.RootTaskRef.Equal(source.RootTaskRef) && c.ProjectSnapshotID == source.ProjectSnapshotID && c.EpisodeID == source.EpisodeID && c.EpisodeRevision == source.EpisodeRevision && c.BaseOID == source.BaseOID && c.SnapshotID == source.SnapshotID && c.Message == source.Message && c.Policy == source.Policy && evidenceRefsEqual(c.SealRef, source.SealRef) && reflect.DeepEqual(c.Evidence, source.Evidence)
}
