package controller

import (
	"fmt"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func WorkflowSealedMetadata(cfg config.AppConfig) (map[string][]byte, bool, error) {
	present, err := RepositoryPresent(cfg.RepoRoot)
	if err != nil || !present {
		return nil, false, err
	}
	exists, err := Exists(cfg)
	if err != nil || !exists {
		return nil, false, err
	}
	store, err := Open(cfg)
	if err != nil {
		return nil, false, err
	}
	return store.workflowSealedMetadata()
}

func (s *Store) workflowSealedMetadata() (map[string][]byte, bool, error) {
	head, err := s.LoadHead()
	if err != nil || head.ExecutionTaskRef == nil || head.RootTaskRef == nil {
		return nil, false, err
	}
	if _, pending, err := s.PreparedCompletionAuthority(head); pending || err != nil {
		return nil, false, err
	}
	sealed, err := s.candidateOwnsTaskAuthority(head)
	if err != nil || !sealed {
		return nil, false, err
	}
	candidate, err := s.LoadAcceptedCandidate(*head.AcceptedCandidateRef)
	if err != nil {
		return nil, false, err
	}
	metadata, err := s.readSealedTaskMetadata(candidate)
	return metadata, err == nil, err
}

func (s *Store) readSealedTaskMetadata(candidate AcceptedCandidate) (map[string][]byte, error) {
	metadata := make(map[string][]byte, 3)
	for _, path := range []string{"IMPLEMENTATION_RULES.md", "IMPLEMENTATION_PLAN.local.md", candidate.TaskRef.TaskPath} {
		data, err := runGitBinary(s.identity.PrimaryRoot, nil, "show", candidate.ImplementationTree+":"+path)
		if err != nil {
			return nil, fmt.Errorf("read sealed Task metadata %s: %w", path, err)
		}
		metadata[path] = data
	}
	if digestBytes(metadata[candidate.TaskRef.TaskPath]) != candidate.TaskRef.ContractDigest {
		return nil, fmt.Errorf("sealed Task metadata differs from candidate contract")
	}
	return metadata, nil
}
