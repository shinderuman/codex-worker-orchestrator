package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type unsealedParentCompletion struct {
	Binding ControllerRuntimeBinding `json:"binding"`
	Review  ParentReviewState        `json:"review"`
}

func (s *StateStore) CaptureUnsealedParentCompletion(binding ControllerRuntimeBinding) ([]byte, error) {
	if err := s.admitUnsealedCompletionCapture(binding); err != nil {
		return nil, err
	}
	review, err := s.loadParentReviewState()
	if err != nil {
		return nil, err
	}
	if review.Completion == nil || review.Completion.Terminal != SessionRotationTerminalAccept {
		return nil, fmt.Errorf("unsealed completion reentry requires an accepted parent outcome")
	}
	return json.Marshal(unsealedParentCompletion{Binding: binding, Review: review})
}

func (s *StateStore) admitUnsealedCompletionCapture(binding ControllerRuntimeBinding) error {
	if s.TaskStatus() != TaskStatusAwaitingParentCompletion || s.Exists("pending-decision") {
		return fmt.Errorf("unsealed completion reentry requires accepted completion without pending decision")
	}
	if _, err := s.LoadResumeCheckpoint(); !errors.Is(err, ErrNoResumeCheckpoint) {
		return fmt.Errorf("unsealed completion reentry requires no resume checkpoint: %w", err)
	}
	if _, err := s.LoadPublicationCandidate(); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unsealed completion reentry cannot replace a publication candidate: %w", err)
	}
	current, err := s.LoadControllerRuntimeBinding()
	if err != nil {
		return err
	}
	if current != binding || binding.AttemptID == "" || binding.TaskPath == "" || binding.TaskContractDigest == "" {
		return fmt.Errorf("unsealed completion reentry runtime binding differs")
	}
	return nil
}

func (s *StateStore) ReopenUnsealedParentCompletion(source []byte) error {
	var before unsealedParentCompletion
	if err := json.Unmarshal(source, &before); err != nil {
		return err
	}
	current, err := s.CaptureUnsealedParentCompletion(before.Binding)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, source) {
		return fmt.Errorf("unsealed parent completion changed before reentry")
	}
	risk := reentryReviewRiskFloor(before.Review.Completion.Risk)
	if err := s.applyControllerReentryTransition(risk); err != nil {
		return err
	}
	s.recordReopenedParentOutcome(before.Review.TaskID, ParentOriginCodexReview, ParentCauseProductionWiring, risk)
	return nil
}
