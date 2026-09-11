package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type ParentReviewState struct {
	Version int                    `json:"version"`
	TaskID  string                 `json:"task_id"`
	Open    *ParentReviewOpenState `json:"open,omitempty"`
	Review  *ParentReviewBinding   `json:"review,omitempty"`
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

func (s *StateStore) openParentReviewState(status string, risk string, producer ParentReviewProducer) error {
	if !validParentReviewPacketStatus(status) {
		return fmt.Errorf("parent review packet statusが不正です: %s", status)
	}
	state, err := s.loadParentReviewState()
	if err != nil {
		return err
	}
	state.Open = &ParentReviewOpenState{
		PacketStatus: status,
		Role:         producer.Role,
		ModelAlias:   producer.Model,
		Risk:         risk,
	}
	state.Review = nil
	return s.writeParentReviewState(state)
}

func (s *StateStore) resolveParentReviewState(kind, origin, cause string) (ParentReviewOpenState, bool, error) {
	if !parentOutcomeKinds[kind] {
		return ParentReviewOpenState{}, false, fmt.Errorf("unknown parent outcome kind: %s", kind)
	}
	if kind == ParentOutcomeFix {
		if err := validateParentFixDeclaration(origin, cause); err != nil {
			return ParentReviewOpenState{}, false, err
		}
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
	state.Open = nil
	state.Review = nil
	if err := s.writeParentReviewState(state); err != nil {
		return ParentReviewOpenState{}, false, err
	}
	return resolved, true, nil
}
