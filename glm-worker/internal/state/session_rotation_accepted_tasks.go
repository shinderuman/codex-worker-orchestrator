package state

import (
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) SessionRotationAcceptedTaskCount(threadID string) (int, bool, string, error) {
	markerPath := s.SessionRotationMarkerPath(threadID)
	marker, err := s.LoadSessionRotationMarker(threadID)
	if err != nil {
		return 0, false, markerPath, err
	}
	if marker != nil && marker.AcceptedTasks != nil {
		return *marker.AcceptedTasks, true, markerPath, nil
	}
	if sessionRotationMarkerHasTaskHistory(marker) {
		return 0, false, markerPath, nil
	}

	source, found, err := s.sessionRotationIssuedSourceForThread(threadID)
	if err != nil {
		return 0, false, markerPath, err
	}
	if found {
		return 0, true, source, nil
	}
	return 0, false, markerPath, nil
}

func sessionRotationMarkerHasTaskHistory(marker *SessionRotationMarker) bool {
	if marker == nil {
		return false
	}
	return marker.State != "" || marker.Directive != nil || marker.Claim != nil || marker.Issued != nil || marker.LastEvaluation != nil
}

func (s *StateStore) sessionRotationIssuedSourceForThread(threadID string) (string, bool, error) {
	entries, err := os.ReadDir(s.Path(sessionRotationDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("session rotation markersを読めません: %w", err)
	}
	found := ""
	for _, entry := range entries {
		sourceThreadID := sessionRotationMarkerThreadID(entry.Name())
		if sourceThreadID == "" || sourceThreadID == threadID {
			continue
		}
		sourceMarker, loadErr := s.LoadSessionRotationMarker(sourceThreadID)
		if loadErr != nil {
			return "", false, loadErr
		}
		if sourceMarker == nil || sourceMarker.State != SessionRotationStateIssued || sourceMarker.Issued == nil || sourceMarker.Issued.BoundThreadID != threadID {
			continue
		}
		if found != "" {
			return "", false, fmt.Errorf("session rotation accepted-task sourceが重複しています: %s", threadID)
		}
		found = s.SessionRotationMarkerPath(sourceThreadID)
	}
	return found, found != "", nil
}

func (marker *SessionRotationMarker) validateCreationOutcomeAndAcceptedTasks() error {
	if err := marker.validateCreationOutcome(); err != nil {
		return err
	}
	if marker.AcceptedTasks != nil && *marker.AcceptedTasks < 0 {
		return fmt.Errorf("session rotation markerのaccepted_tasksが不正です: %d", *marker.AcceptedTasks)
	}
	return nil
}

func sessionRotationAcceptedTasksForCommit(evaluation *SessionRotationEvaluation) *int {
	if evaluation.AcceptedTasksUnavailable {
		return nil
	}
	acceptedTasks := evaluation.AcceptedTasks
	return &acceptedTasks
}
