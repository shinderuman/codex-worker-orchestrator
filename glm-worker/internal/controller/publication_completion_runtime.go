package controller

import (
	"fmt"
	"reflect"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) completionRuntime(op ExecutionOperation) (*state.StateStore, string, error) {
	attempt, err := s.loadAttempt(op.Terminal.Record.AttemptID)
	if err != nil {
		return nil, "", err
	}
	if err := s.validateUnresolvedAttemptFindings(Admission{Attempt: attempt}); err != nil {
		return nil, "", err
	}
	return s.boundAttemptRuntime(Admission{Attempt: attempt})
}

func (s *Store) prepareCompletionRuntime(op ExecutionOperation) (string, *state.ParentCompletionOutcome, error) {
	runtime, taskID, err := s.completionRuntime(op)
	if err != nil || runtime == nil {
		return taskID, nil, err
	}
	outcome, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return "", nil, err
	}
	plan, err := runtime.ParentActionPlan()
	if err != nil {
		return "", nil, err
	}
	if runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion || plan.RequiredAction != state.ParentActionComplete || outcome == nil || outcome.Terminal != state.SessionRotationTerminalAccept {
		return "", nil, fmt.Errorf("completion runtime lacks an accepted terminal acknowledgement plan")
	}
	return taskID, outcome, nil
}

func (s *Store) acknowledgeCompletionRuntime(op ExecutionOperation, phase TransitionState) error {
	runtime, err := s.boundCompletionRuntime(op, phase)
	if err != nil || runtime == nil {
		return err
	}
	if runtime.TaskStatus() == state.TaskStatusAwaitingParentCompletion {
		if _, err := runtime.CompleteParentAwaiting(nil); err != nil {
			return err
		}
	}
	return validateCompletedRuntime(runtime)
}

func (s *Store) boundCompletionRuntime(op ExecutionOperation, phase TransitionState) (*state.StateStore, error) {
	ready, err := s.loadCompletionReadiness(op, phase)
	if err != nil {
		return nil, err
	}
	runtime, taskID, err := s.completionRuntime(op)
	if err != nil {
		return nil, err
	}
	if taskID != ready.RuntimeTaskID || (runtime == nil) != (ready.RuntimeCompletion == nil) {
		return nil, fmt.Errorf("completion Runtime identity changed after readiness")
	}
	if runtime == nil {
		return nil, nil
	}
	outcome, err := runtime.CurrentParentCompletionOutcome()
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(outcome, ready.RuntimeCompletion) {
		return nil, fmt.Errorf("completion Runtime outcome changed after readiness")
	}
	plan, err := runtime.ParentActionPlan()
	if err != nil {
		return nil, err
	}
	if runtime.TaskStatus() == state.TaskStatusAwaitingParentCompletion && plan.RequiredAction == state.ParentActionComplete {
		return runtime, nil
	}
	return runtime, validateCompletedRuntime(runtime)
}

func validateCompletedRuntime(runtime *state.StateStore) error {
	plan, err := runtime.ParentActionPlan()
	if err != nil {
		return err
	}
	if runtime.TaskStatus() != state.TaskStatusComplete || plan.RequiredAction != state.ParentActionNone {
		return fmt.Errorf("completion Runtime has unfinished parent actions")
	}
	return nil
}

func (s *Store) verifyCompletionRuntime(op ExecutionOperation, phase TransitionState) error {
	runtime, err := s.boundCompletionRuntime(op, phase)
	if err != nil || runtime == nil {
		return err
	}
	return validateCompletedRuntime(runtime)
}

func validateCompletionRuntimePlan(ready CompletionReadiness) error {
	if (ready.RuntimeTaskID == "") != (ready.RuntimeCompletion == nil) {
		return fmt.Errorf("completion Runtime outcome binding is missing")
	}
	if ready.RuntimeCompletion != nil && (ready.RuntimeCompletion.Terminal != state.SessionRotationTerminalAccept || (ready.RuntimeCompletion.Risk != "LOW" && ready.RuntimeCompletion.Risk != "HIGH")) {
		return fmt.Errorf("completion Runtime plan is not an accepted outcome")
	}
	return nil
}
