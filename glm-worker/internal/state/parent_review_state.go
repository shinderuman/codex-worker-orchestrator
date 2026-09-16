package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentCompletionOutcome struct {
	Terminal string `json:"terminal"`
	Risk     string `json:"risk"`
}

type ParentReviewState struct {
	Version    int                      `json:"version"`
	TaskID     string                   `json:"task_id"`
	Open       *ParentReviewOpenState   `json:"open,omitempty"`
	Review     *ParentReviewBinding     `json:"review,omitempty"`
	Completion *ParentCompletionOutcome `json:"completion,omitempty"`
}

const (
	parentReviewStateFile    = "parent-review-state.json"
	parentReviewStateVersion = 1
)

func (s *StateStore) initializeParentReviewState(taskID string) error {
	return s.writeParentReviewState(ParentReviewState{
		Version: parentReviewStateVersion,
		TaskID:  taskID,
	})
}

func (s *StateStore) loadParentReviewState() (ParentReviewState, error) {
	data, err := os.ReadFile(s.Path(parentReviewStateFile))
	if err != nil {
		return ParentReviewState{}, err
	}
	var state ParentReviewState
	if err := json.Unmarshal(data, &state); err != nil {
		return ParentReviewState{}, fmt.Errorf("parent review stateを読めません: %w", err)
	}
	taskID, err := s.TaskID()
	if err != nil {
		return ParentReviewState{}, err
	}
	if state.Version != parentReviewStateVersion || state.TaskID == "" || state.TaskID != taskID {
		return ParentReviewState{}, fmt.Errorf("parent review stateのschemaが不正です")
	}
	if state.Open != nil && !validParentReviewPacketStatus(state.Open.PacketStatus) {
		return ParentReviewState{}, fmt.Errorf("parent review stateのpacket statusが不正です: %s", state.Open.PacketStatus)
	}
	if err := validateParentReviewBindingState(state); err != nil {
		return ParentReviewState{}, err
	}
	if err := validateParentCompletionState(state); err != nil {
		return ParentReviewState{}, err
	}
	return state, nil
}

func (s *StateStore) writeParentReviewState(state ParentReviewState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("parent review stateをJSON化できません: %w", err)
	}
	if err := writeFileAtomic(s.Path(parentReviewStateFile), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("parent review stateを書き込めません: %w", err)
	}
	return nil
}

func validParentReviewPacketStatus(status string) bool {
	switch packet.Status(status) {
	case packet.StatusNeedsSolDecision, packet.StatusNeedsSolReview, packet.StatusPass:
		return true
	default:
		return false
	}
}

func validateParentCompletionState(state ParentReviewState) error {
	if state.Completion == nil {
		return nil
	}
	if state.Open != nil || state.Review != nil {
		return fmt.Errorf("parent completion outcome exists with an open parent review")
	}
	if state.Completion.Terminal != SessionRotationTerminalAccept && state.Completion.Terminal != SessionRotationTerminalNoGo {
		return fmt.Errorf("parent completion outcome has invalid terminal %q", state.Completion.Terminal)
	}
	if state.Completion.Risk != string(packet.RiskLow) && state.Completion.Risk != string(packet.RiskHigh) {
		return fmt.Errorf("parent completion outcome has invalid risk %q", state.Completion.Risk)
	}
	return nil
}

func (s *StateStore) CurrentParentCompletionOutcome() (*ParentCompletionOutcome, error) {
	state, err := s.loadParentReviewState()
	if err != nil {
		return nil, err
	}
	if state.Completion == nil {
		return nil, nil
	}
	outcome := *state.Completion
	return &outcome, nil
}

func (s *StateStore) CurrentParentReview() (*ParentReviewOpenState, error) {
	taskID, err := s.Read("task.id")
	if errors.Is(err, os.ErrNotExist) || taskID == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return nil, err
	}
	if state.Open == nil {
		return nil, nil
	}
	open := *state.Open
	return &open, nil
}

func (s *StateStore) CurrentParentReviewLabel() (string, error) {
	open, err := s.CurrentParentReview()
	if err != nil {
		return "", err
	}
	if open == nil {
		return roundCommentNone, nil
	}
	return open.PacketStatus, nil
}

func (s *StateStore) OpenParentReviewLabel() string {
	label, err := s.CurrentParentReviewLabel()
	if err != nil {
		return "unavailable"
	}
	return label
}

func (s *StateStore) openParentReviewState(status string, risk string, producer ParentReviewProducer, markNonConvergence bool) error {
	if !validParentReviewPacketStatus(status) {
		return fmt.Errorf("parent review packet statusが不正です: %s", status)
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return err
	}
	state.Open = &ParentReviewOpenState{
		PacketStatus:                   status,
		Role:                           producer.Role,
		ModelAlias:                     producer.Model,
		Risk:                           risk,
		ParentValidationNonConvergence: markNonConvergence,
	}
	state.Review = nil
	state.Completion = nil
	return s.writeParentReviewState(state)
}

func (s *StateStore) FinishParentValidationNonConvergence(value packet.Result, producer ParentReviewProducer) error {
	if s.TaskStatus() != TaskStatusActive {
		return fmt.Errorf("parent validation non-convergence transition requires active task, got %s", s.TaskStatus())
	}
	if value.Status != packet.StatusNeedsSolReview {
		return fmt.Errorf("parent validation non-convergence transition requires %s, got %s", packet.StatusNeedsSolReview, value.Status)
	}
	review, err := s.snapshotLifecycleFile(parentReviewStateFile)
	if err != nil {
		return err
	}
	status, err := s.snapshotLifecycleFile("task.status")
	if err != nil {
		return err
	}
	if err := s.openParentReviewState(string(value.Status), string(value.Risk), producer, true); err != nil {
		return err
	}
	if err := s.FinishReview(TaskStatusWaitingSolReview); err != nil {
		return s.rollbackLifecycleFiles(err, review, status)
	}
	s.recordSolOutcomeStats(value, producer)
	return nil
}

func (s *StateStore) resolveParentReviewState(kind, origin, cause string) (ParentReviewOpenState, bool, error) {
	return s.resolveParentReviewStateWithCompletion(kind, origin, cause, "")
}

func (s *StateStore) resolveParentCompletionState(kind, terminal string) (ParentReviewOpenState, bool, error) {
	return s.resolveParentReviewStateWithCompletion(kind, "", "", terminal)
}

func (s *StateStore) resolveParentReviewStateWithCompletion(kind, origin, cause, terminal string) (ParentReviewOpenState, bool, error) {
	if err := validateParentOutcomeResolution(kind, origin, cause, terminal); err != nil {
		return ParentReviewOpenState{}, false, err
	}
	taskID, taskErr := s.Read("task.id")
	if errors.Is(taskErr, os.ErrNotExist) || taskID == "" {
		return ParentReviewOpenState{}, false, nil
	}
	if taskErr != nil {
		return ParentReviewOpenState{}, false, taskErr
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return ParentReviewOpenState{}, false, err
	}
	if state.Open == nil {
		return ParentReviewOpenState{}, false, nil
	}
	if kind == ParentOutcomeAccepted && state.Open.PacketStatus == string(packet.StatusNeedsSolDecision) {
		return ParentReviewOpenState{}, false, fmt.Errorf("pending Sol decision must be resolved with --decision before --accept")
	}
	resolved := *state.Open
	completion, err := parentCompletionOutcome(terminal, resolved.Risk)
	if err != nil {
		return ParentReviewOpenState{}, false, err
	}
	state.Open = nil
	state.Review = nil
	state.Completion = completion
	if err := s.writeParentReviewState(state); err != nil {
		return ParentReviewOpenState{}, false, err
	}
	return resolved, true, nil
}

func validateParentOutcomeResolution(kind, origin, cause, terminal string) error {
	if !parentOutcomeKinds[kind] {
		return fmt.Errorf("unknown parent outcome kind: %s", kind)
	}
	if kind == ParentOutcomeFix {
		if err := validateParentFixDeclaration(origin, cause); err != nil {
			return err
		}
	}
	if terminal == "" {
		return nil
	}
	if kind == ParentOutcomeAccepted && terminal == SessionRotationTerminalAccept {
		return nil
	}
	if kind == ParentOutcomeNoGo && terminal == SessionRotationTerminalNoGo {
		return nil
	}
	return fmt.Errorf("parent outcome %s cannot resolve completion terminal %s", kind, terminal)
}

func parentCompletionOutcome(terminal, risk string) (*ParentCompletionOutcome, error) {
	if terminal == "" {
		return nil, nil
	}
	outcome := &ParentCompletionOutcome{Terminal: terminal, Risk: risk}
	state := ParentReviewState{Completion: outcome}
	if err := validateParentCompletionState(state); err != nil {
		return nil, err
	}
	return outcome, nil
}
