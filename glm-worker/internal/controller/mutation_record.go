package controller

import (
	"fmt"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) recordMutationLocked(admission Admission, command, outcome string, after WorkspaceSnapshot) (Admission, error) {
	current, err := s.AdmitMutation(admission.Lease.SemanticTaskRef, admission.Workspace, admission.Snapshot)
	if err != nil {
		return Admission{}, err
	}
	if current.Lease.LeaseID != admission.Lease.LeaseID {
		return Admission{}, fmt.Errorf("execution lease changed before mutation provenance commit")
	}
	mutationID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	leaseID, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	nextGeneration := current.Head.ControllerGeneration + 1
	nextLease := current.Lease
	nextLease.LeaseID = leaseID
	nextLease.ControllerGeneration = nextGeneration
	nextLease.ExpectedBaseOID = after.Head
	nextLease.ExpectedWorkspaceSnapshotID = after.ID
	nextLease.InFlightCallID = ""
	nextLease.CreatedAt = time.Now().UTC()
	record := MutationRecord{
		SchemaVersion:    controllerSchemaVersion,
		MutationID:       mutationID,
		AttemptID:        current.Attempt.AttemptID,
		SourceLeaseID:    current.Lease.LeaseID,
		TargetLeaseID:    nextLease.LeaseID,
		SourceGeneration: current.Head.ControllerGeneration,
		TargetGeneration: nextGeneration,
		Command:          command,
		Outcome:          outcome,
		Before:           current.Snapshot,
		After:            after,
		Surfaces:         changedSurfaces(current.Snapshot, after),
		CreatedAt:        time.Now().UTC(),
	}
	if err := writeJSONAtomic(s.mutationPath(mutationID), record); err != nil {
		return Admission{}, err
	}
	if err := s.writeLease(nextLease); err != nil {
		return Admission{}, err
	}
	nextHead := current.Head
	nextHead.ControllerGeneration = nextGeneration
	nextHead.LiveLeaseID = nextLease.LeaseID
	if err := s.writeHeadCAS(current.Head.ControllerGeneration, nextHead); err != nil {
		return Admission{}, err
	}
	return Admission{Head: nextHead, Attempt: current.Attempt, Lease: nextLease, Workspace: current.Workspace, Snapshot: after}, nil
}

func changedSurfaces(before, after WorkspaceSnapshot) []MutationSurface {
	set := map[MutationSurface]bool{}
	if before.WorktreeDigest != after.WorktreeDigest {
		set[MutationSurfaceSource] = true
	}
	if before.IndexDigest != after.IndexDigest {
		set[MutationSurfaceIndex] = true
	}
	if before.Head != after.Head {
		set[MutationSurfaceHead] = true
		set[MutationSurfaceHistory] = true
	}
	if before.RefDigest != after.RefDigest {
		set[MutationSurfaceRef] = true
	}
	result := make([]MutationSurface, 0, len(set))
	for surface := range set {
		result = append(result, surface)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}
