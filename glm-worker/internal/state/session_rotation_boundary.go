package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const pendingSessionRotationRetirementStateFile = "pending-session-rotation-retirement.json"
const pendingSessionRotationRetirementVersion = 1

type pendingSessionRotationRetirementStage struct {
	Version         int                                              `json:"version"`
	CallerThreadID  string                                           `json:"caller_thread_id"`
	Recommendations []pendingSessionRotationRetirementRecommendation `json:"recommendations"`
}

type pendingSessionRotationRetirementRecommendation struct {
	ParentThreadID string `json:"parent_thread_id"`
	DirectiveID    string `json:"directive_id"`
}

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

func (s *StateStore) StagePendingSessionRotationRecommendationRetirement(callerThreadID string) error {
	resume, err := s.AdmitNewTaskRotationBoundary(callerThreadID, "")
	if err != nil {
		return err
	}
	if resume {
		return fmt.Errorf("ordinary task start cannot resume a claimed session rotation")
	}
	rotations, err := s.IncompleteSessionRotations()
	if err != nil {
		return err
	}
	stage := pendingSessionRotationRetirementStage{
		Version:        pendingSessionRotationRetirementVersion,
		CallerThreadID: callerThreadID,
	}
	for _, rotation := range rotations {
		if rotation.State != SessionRotationStatePending {
			return fmt.Errorf("session rotation changed before ordinary task start: parent_thread_id=%s state=%s directive_id=%s", rotation.ParentThreadID, rotation.State, rotation.DirectiveID)
		}
		stage.Recommendations = append(stage.Recommendations, pendingSessionRotationRetirementRecommendation{
			ParentThreadID: rotation.ParentThreadID,
			DirectiveID:    rotation.DirectiveID,
		})
	}
	if len(stage.Recommendations) == 0 {
		return s.ClearPendingSessionRotationRecommendationRetirement()
	}
	data, err := json.MarshalIndent(stage, "", "  ")
	if err != nil {
		return fmt.Errorf("pending session rotation retirementをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(pendingSessionRotationRetirementStateFile), append(data, '\n'), 0o600)
}

func (s *StateStore) ClearPendingSessionRotationRecommendationRetirement() error {
	return s.Remove(pendingSessionRotationRetirementStateFile)
}

func (s *StateStore) loadPendingSessionRotationRecommendationRetirement() (*pendingSessionRotationRetirementStage, error) {
	data, err := os.ReadFile(s.Path(pendingSessionRotationRetirementStateFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pending session rotation retirementを読めません: %w", err)
	}
	var stage pendingSessionRotationRetirementStage
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&stage); err != nil {
		return nil, fmt.Errorf("pending session rotation retirementのschemaが不正です: %w", err)
	}
	if stage.Version != pendingSessionRotationRetirementVersion || len(stage.Recommendations) == 0 {
		return nil, fmt.Errorf("pending session rotation retirementが不正です")
	}
	seen := make(map[string]struct{}, len(stage.Recommendations))
	for _, recommendation := range stage.Recommendations {
		if !ValidUUIDFormat(recommendation.ParentThreadID) || !ValidGeneratedUUID(recommendation.DirectiveID) {
			return nil, fmt.Errorf("pending session rotation retirementのidentityが不正です")
		}
		if _, ok := seen[recommendation.ParentThreadID]; ok {
			return nil, fmt.Errorf("pending session rotation retirementの親threadが重複しています: %s", recommendation.ParentThreadID)
		}
		seen[recommendation.ParentThreadID] = struct{}{}
	}
	return &stage, nil
}

func (s *StateStore) validatePendingSessionRotationRecommendationRetirementBoundary() error {
	stage, err := s.loadPendingSessionRotationRecommendationRetirement()
	if err != nil || stage == nil {
		return err
	}
	resume, err := s.AdmitNewTaskRotationBoundary(stage.CallerThreadID, "")
	if err != nil {
		return err
	}
	if resume {
		return fmt.Errorf("ordinary task start cannot resume a claimed session rotation")
	}
	return nil
}

func (s *StateStore) pendingSessionRotationRetirementTransitionFiles() ([]string, error) {
	stage, err := s.loadPendingSessionRotationRecommendationRetirement()
	if err != nil || stage == nil {
		return nil, err
	}
	files := []string{pendingSessionRotationRetirementStateFile}
	for _, recommendation := range stage.Recommendations {
		files = append(files, filepath.Join(sessionRotationDirectory, recommendation.ParentThreadID+".json"))
	}
	return files, nil
}

func (s *StateStore) commitPendingSessionRotationRecommendationRetirement() error {
	stage, err := s.loadPendingSessionRotationRecommendationRetirement()
	if err != nil || stage == nil {
		return err
	}
	expected := make(map[string]string, len(stage.Recommendations))
	for _, recommendation := range stage.Recommendations {
		expected[recommendation.ParentThreadID] = recommendation.DirectiveID
	}
	rotations, err := s.IncompleteSessionRotations()
	if err != nil {
		return err
	}
	if len(rotations) != len(expected) {
		return fmt.Errorf("pending session rotation recommendations changed before ordinary task start")
	}
	for _, rotation := range rotations {
		directiveID, ok := expected[rotation.ParentThreadID]
		if !ok || rotation.State != SessionRotationStatePending || directiveID != rotation.DirectiveID {
			return fmt.Errorf("pending session rotation recommendations changed before ordinary task start")
		}
	}
	markers := make([]*SessionRotationMarker, 0, len(stage.Recommendations))
	for _, recommendation := range stage.Recommendations {
		marker, err := s.LoadSessionRotationMarker(recommendation.ParentThreadID)
		if err != nil {
			return err
		}
		if marker == nil || marker.State != SessionRotationStatePending || marker.Directive == nil || marker.Directive.DirectiveID != recommendation.DirectiveID {
			return fmt.Errorf("pending session rotation recommendation changed before ordinary task start: parent_thread_id=%s directive_id=%s", recommendation.ParentThreadID, recommendation.DirectiveID)
		}
		markers = append(markers, marker)
	}
	for _, marker := range markers {
		marker.State = ""
		marker.Directive = nil
		if err := s.writeSessionRotationMarker(marker); err != nil {
			return err
		}
	}
	return s.Remove(pendingSessionRotationRetirementStateFile)
}
