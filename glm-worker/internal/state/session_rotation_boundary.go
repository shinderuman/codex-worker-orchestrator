package state

import "fmt"

func (s *StateStore) AdmitNewTaskRotationBoundary(callerThreadID, claimID string) (bool, error) {
	if claimID != "" {
		return s.AdmitNewTaskRotation(callerThreadID, claimID)
	}
	if incomplete, err := s.incompleteSessionRotationTarget(callerThreadID); err != nil {
		return false, err
	} else if incomplete {
		return false, fmt.Errorf("session rotation開始の再試行にはrotation claimが必要です")
	}
	if err := s.rejectRetiredSessionRotationCaller(callerThreadID); err != nil {
		return false, err
	}

	rotations, err := s.IncompleteSessionRotations()
	if err != nil {
		return false, err
	}
	for _, rotation := range rotations {
		switch rotation.State {
		case SessionRotationStatePending:
			continue
		case SessionRotationStateBound:
			if rotation.BoundThreadID == callerThreadID {
				return false, fmt.Errorf("このthreadにbind済みのsession rotationはclaim付きstartで開始してください: claim_id=%s", rotation.ClaimID)
			}
			fallthrough
		case SessionRotationStateClaimed:
			return false, fmt.Errorf("claim済みsession rotationを持ち主threadのbindとclaim付きstartで完了してください: parent_thread_id=%s state=%s directive_id=%s", rotation.ParentThreadID, rotation.State, rotation.DirectiveID)
		default:
			return false, fmt.Errorf("未対応のsession rotation stateです: %s", rotation.State)
		}
	}
	return false, nil
}

func (s *StateStore) RetirePendingSessionRotationRecommendations() error {
	rotations, err := s.IncompleteSessionRotations()
	if err != nil {
		return err
	}
	for _, rotation := range rotations {
		if rotation.State != SessionRotationStatePending {
			continue
		}
		if err := s.retirePendingSessionRotationRecommendation(rotation); err != nil {
			return err
		}
	}
	return nil
}

func (s *StateStore) retirePendingSessionRotationRecommendation(rotation IncompleteSessionRotation) error {
	marker, err := s.LoadSessionRotationMarker(rotation.ParentThreadID)
	if err != nil {
		return err
	}
	if marker == nil || marker.State != SessionRotationStatePending || marker.Directive == nil || marker.Directive.DirectiveID != rotation.DirectiveID {
		return fmt.Errorf("pending session rotation recommendation changed before ordinary task start: parent_thread_id=%s directive_id=%s", rotation.ParentThreadID, rotation.DirectiveID)
	}
	marker.State = ""
	marker.Directive = nil
	return s.writeSessionRotationMarker(marker)
}
