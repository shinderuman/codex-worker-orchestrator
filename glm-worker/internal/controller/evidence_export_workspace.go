package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var errEvidenceExportWorkspaceUnresolved = errors.New("attempt lease workspace is not bound to a resolvable workspace of this repository")

func (s *Store) resolveAttemptWorkspace(lease ExecutionLease) (WorkspaceIdentity, error) {
	return s.resolveWorkspaceByID(lease.WorkspaceID)
}

func (s *Store) resolveWorkspaceByID(workspaceID string) (WorkspaceIdentity, error) {
	var observations []string
	for _, root := range s.attemptWorkspaceCandidates() {
		workspace, err := ResolveWorkspaceIdentity(root, s.identity)
		if err != nil {
			observations = append(observations, fmt.Sprintf("%s: %v", root, err))
			continue
		}
		if workspace.ID == workspaceID {
			return workspace, nil
		}
		observations = append(observations, fmt.Sprintf("%s: workspace %s", root, workspace.ID))
	}
	return WorkspaceIdentity{}, fmt.Errorf("%w: workspace %s; candidates: %s",
		errEvidenceExportWorkspaceUnresolved, workspaceID, strings.Join(observations, "; "))
}

func (s *Store) attemptWorkspaceForExport(head RepositoryControllerHead, attempt AttemptRecord) (WorkspaceIdentity, bool, error) {
	if attempt.AttemptID == head.LiveAttemptID {
		workspace, err := s.liveAttemptWorkspace(head, attempt)
		if err != nil {
			return WorkspaceIdentity{}, false, err
		}
		return workspace, true, nil
	}
	if attempt.AttemptSealID != "" {
		_, seal, err := s.findAttemptSeal(head, attempt)
		if err != nil {
			return WorkspaceIdentity{}, false, nil
		}
		workspace, err := s.resolveWorkspaceByID(seal.WorkspaceID)
		if errors.Is(err, errEvidenceExportWorkspaceUnresolved) {
			return WorkspaceIdentity{}, false, nil
		}
		if err != nil {
			return WorkspaceIdentity{}, false, err
		}
		return workspace, true, nil
	}
	lease, found, err := s.uniqueAttemptLease(attempt.AttemptID)
	if err != nil || !found {
		return WorkspaceIdentity{}, false, err
	}
	workspace, err := s.resolveAttemptWorkspace(lease)
	if errors.Is(err, errEvidenceExportWorkspaceUnresolved) {
		return WorkspaceIdentity{}, false, nil
	}
	if err != nil {
		return WorkspaceIdentity{}, false, err
	}
	return workspace, true, nil
}

func (s *Store) liveAttemptWorkspace(head RepositoryControllerHead, attempt AttemptRecord) (WorkspaceIdentity, error) {
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("evidence export live lease is unavailable: %w", err)
	}
	if lease.AttemptID != attempt.AttemptID {
		return WorkspaceIdentity{}, fmt.Errorf("evidence export live lease is bound to another attempt")
	}
	workspace, err := s.resolveAttemptWorkspace(lease)
	if err != nil {
		return WorkspaceIdentity{}, fmt.Errorf("evidence export attempt workspace: %w", err)
	}
	return workspace, nil
}

func (s *Store) attemptWorkspaceCandidates() []string {
	candidates := []string{s.config.RepoRoot}
	if lane, err := s.executionLaneRoot(); err == nil && lane != s.config.RepoRoot {
		candidates = append(candidates, lane)
	}
	return candidates
}

func (s *Store) uniqueAttemptLease(attemptID string) (ExecutionLease, bool, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "leases"))
	if errors.Is(err, os.ErrNotExist) {
		return ExecutionLease{}, false, nil
	}
	if err != nil {
		return ExecutionLease{}, false, err
	}
	var matches []ExecutionLease
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		lease, err := s.loadLease(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return ExecutionLease{}, false, err
		}
		if lease.AttemptID == attemptID {
			matches = append(matches, lease)
		}
	}
	if len(matches) != 1 {
		return ExecutionLease{}, false, nil
	}
	return matches[0], true, nil
}
