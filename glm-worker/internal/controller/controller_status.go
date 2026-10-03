package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type ControllerStatusReport struct {
	SchemaVersion      int                          `json:"schema_version"`
	RepositoryIdentity string                       `json:"repository_identity"`
	PrimaryRoot        string                       `json:"primary_root"`
	CanonicalActive    bool                         `json:"canonical_active"`
	Head               RepositoryControllerHead     `json:"head"`
	Live               *ControllerLiveStatus        `json:"live,omitempty"`
	LiveError          string                       `json:"live_error,omitempty"`
	Episode            *ControllerEpisodeStatus     `json:"episode,omitempty"`
	Schedule           *ControllerScheduleStatus    `json:"schedule,omitempty"`
	Suspensions        []ControllerSuspensionStatus `json:"suspensions,omitempty"`
	FailureReason      string                       `json:"failure_reason,omitempty"`
}

type ControllerLiveStatus struct {
	AttemptID     string          `json:"attempt_id"`
	LeaseID       string          `json:"lease_id"`
	WorkspaceID   string          `json:"workspace_id"`
	WorkspaceRoot string          `json:"workspace_root,omitempty"`
	ExecutionTask SemanticTaskRef `json:"execution_task"`
	RootTask      SemanticTaskRef `json:"root_task"`
	Purpose       string          `json:"purpose"`
	AttemptState  AttemptState    `json:"attempt_state"`
}

type ControllerEpisodeStatus struct {
	EpisodeID         string       `json:"episode_id"`
	Revision          uint64       `json:"revision"`
	State             EpisodeState `json:"state,omitempty"`
	RootTaskPath      string       `json:"root_task_path,omitempty"`
	ScopeRootTaskPath string       `json:"scope_root_task_path,omitempty"`
	Error             string       `json:"error,omitempty"`
}

type ControllerScheduleStatus struct {
	Intent      FindingIntentKind `json:"intent,omitempty"`
	NextTaskRef *SemanticTaskRef  `json:"next_task_ref,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type ControllerSuspensionStatus struct {
	SnapshotID      string `json:"snapshot_id,omitempty"`
	AttemptID       string `json:"attempt_id,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	TaskPath        string `json:"task_path,omitempty"`
	RootTaskPath    string `json:"root_task_path,omitempty"`
	EpisodeID       string `json:"episode_id,omitempty"`
	EpisodeRevision uint64 `json:"episode_revision,omitempty"`
	Corrupt         bool   `json:"corrupt,omitempty"`
	File            string `json:"file,omitempty"`
}

func (s *Store) ProjectStatus() (ControllerStatusReport, error) {
	head, err := s.LoadHead()
	if err != nil {
		return ControllerStatusReport{}, err
	}
	report := ControllerStatusReport{
		SchemaVersion:      controllerSchemaVersion,
		RepositoryIdentity: s.identity.LineageID,
		PrimaryRoot:        s.identity.PrimaryRoot,
		CanonicalActive:    !IsPristine(head),
		Head:               head,
	}
	if head.Status == ControllerStatusActive && (head.LiveAttemptID != "" || head.LiveLeaseID != "") {
		live, err := s.projectLiveStatus(head)
		if err != nil {
			report.LiveError = err.Error()
		} else {
			report.Live = live
		}
	}
	if head.ActiveEpisodeID != "" {
		report.Episode = s.projectEpisodeStatus(head)
		if report.Episode.Error == "" {
			report.Schedule = s.projectScheduleStatus(head)
		}
	}
	report.Suspensions = s.projectSuspensions()
	if head.FailureID != "" {
		report.FailureReason = s.projectFailureReason(head.FailureID)
	}
	return report, nil
}

func (s *Store) projectLiveStatus(head RepositoryControllerHead) (*ControllerLiveStatus, error) {
	if head.LiveAttemptID == "" || head.LiveLeaseID == "" {
		return nil, fmt.Errorf("live controller head has partial attempt/lease authority")
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return nil, err
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return nil, err
	}
	if attempt.AttemptID != lease.AttemptID {
		return nil, fmt.Errorf("live attempt/lease identity is inconsistent: attempt=%s lease-attempt=%s", attempt.AttemptID, lease.AttemptID)
	}
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil {
		return nil, fmt.Errorf("live controller head is missing task authority")
	}
	return &ControllerLiveStatus{
		AttemptID:     attempt.AttemptID,
		LeaseID:       lease.LeaseID,
		WorkspaceID:   lease.WorkspaceID,
		WorkspaceRoot: s.projectWorkspaceRoot(lease.WorkspaceID),
		ExecutionTask: attempt.SemanticTaskRef,
		RootTask:      attempt.RootTaskRef,
		Purpose:       lease.Purpose,
		AttemptState:  attempt.AttemptState,
	}, nil
}

func (s *Store) projectWorkspaceRoot(workspaceID string) string {
	primary, err := ResolveWorkspaceIdentity(s.identity.PrimaryRoot, s.identity)
	if err == nil && primary.ID == workspaceID {
		return primary.Root
	}
	lane := s.laneWorkspaceRoot()
	if lane == "" {
		return ""
	}
	resolved, err := ResolveWorkspaceIdentity(lane, s.identity)
	if err == nil && resolved.ID == workspaceID {
		return resolved.Root
	}
	return ""
}

func (s *Store) laneWorkspaceRoot() string {
	base, err := canonicalPath(s.dir)
	if err != nil {
		return ""
	}
	return filepath.Join(base, s.identity.LineageID[:16]+"-lane")
}

func (s *Store) projectEpisodeStatus(head RepositoryControllerHead) *ControllerEpisodeStatus {
	status := &ControllerEpisodeStatus{EpisodeID: head.ActiveEpisodeID, Revision: head.ActiveEpisodeRevision}
	revision, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.State = revision.State
	status.RootTaskPath = revision.RootTaskRef.TaskPath
	status.ScopeRootTaskPath = revision.ScopeRootTaskRef.TaskPath
	return status
}

func (s *Store) projectScheduleStatus(head RepositoryControllerHead) *ControllerScheduleStatus {
	revision, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		return &ControllerScheduleStatus{Error: err.Error()}
	}
	if head.PendingTransitionID != "" {
		return &ControllerScheduleStatus{Reason: "controller has a pending transition"}
	}
	schedule, err := s.scheduleEpisodeAgainstProject(revision)
	if err != nil {
		return &ControllerScheduleStatus{Error: err.Error()}
	}
	return &ControllerScheduleStatus{Intent: schedule.Intent, NextTaskRef: schedule.NextTaskRef, Reason: schedule.Reason}
}

func (s *Store) projectSuspensions() []ControllerSuspensionStatus {
	entries, err := os.ReadDir(filepath.Join(s.dir, "suspensions"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return []ControllerSuspensionStatus{{Corrupt: true, File: "suspensions", SnapshotID: ""}}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	statuses := make([]ControllerSuspensionStatus, 0, len(names))
	for _, name := range names {
		var snapshot SuspensionSnapshot
		if err := readJSON(filepath.Join(s.dir, "suspensions", name), &snapshot); err != nil {
			statuses = append(statuses, ControllerSuspensionStatus{Corrupt: true, File: name})
			continue
		}
		statuses = append(statuses, ControllerSuspensionStatus{
			SnapshotID:      snapshot.SnapshotID,
			AttemptID:       snapshot.AttemptID,
			WorkspaceID:     snapshot.WorkspaceID,
			TaskPath:        snapshot.SemanticTaskRef.TaskPath,
			RootTaskPath:    snapshot.RootTaskRef.TaskPath,
			EpisodeID:       snapshot.EpisodeID,
			EpisodeRevision: snapshot.EpisodeRevision,
		})
	}
	return statuses
}

func (s *Store) projectFailureReason(failureID string) string {
	var record FailureRecord
	if err := readJSON(s.failurePath(failureID), &record); err != nil {
		return fmt.Sprintf("failure record %s is unreadable: %v", failureID, err)
	}
	return record.Reason
}
