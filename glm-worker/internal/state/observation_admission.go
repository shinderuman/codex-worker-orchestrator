package state

import "fmt"

func (s *StateStore) ObservationLifecycleAdmission() (ObservationExecutionAdmission, error) {
	if s.TaskStatus() != TaskStatusWaitingDecision || !s.Exists("pending-decision") {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation capability is only available at the pending Sol decision boundary")
	}
	open, err := s.CurrentParentReview()
	if err != nil {
		return ObservationExecutionAdmission{}, fmt.Errorf("pending decisionのparent review状態を読めません: %w", err)
	}
	if open == nil || open.PacketStatus != "NEEDS_SOL_DECISION" {
		return ObservationExecutionAdmission{}, fmt.Errorf("observation capability is only available while NEEDS_SOL_DECISION is pending")
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	round, err := s.ObservationExecutionRound()
	if err != nil {
		return ObservationExecutionAdmission{}, err
	}
	return ObservationExecutionAdmission{TaskID: taskID, Round: round}, nil
}

func (s *StateStore) ValidateObservationLifecycleAdmission(expected ObservationExecutionAdmission) error {
	current, err := s.ObservationLifecycleAdmission()
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("observation lifecycle admission changed: task=%s round=%d", current.TaskID, current.Round)
	}
	return nil
}
