package controller

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func prepareTestAuthorityTransition(s *Store, intent TransitionIntent) (TransitionRecord, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return TransitionRecord{}, err
	}
	defer func() { _ = lock.Close() }()
	if err := s.validateTransitionSource(intent); err != nil {
		return TransitionRecord{}, err
	}
	transitionID, err := state.NewUUID()
	if err != nil {
		return TransitionRecord{}, err
	}
	record := buildTransitionRecord(intent, transitionID)
	if err := s.persistPreparedTransition(record, intent.Source.Head); err != nil {
		return TransitionRecord{}, err
	}
	return record, nil
}
