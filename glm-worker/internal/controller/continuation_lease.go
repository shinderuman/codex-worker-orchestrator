package controller

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) prepareContinuationLease(
	source ExecutionLease,
	snapshot WorkspaceSnapshot,
	generation uint64,
) (ExecutionLease, error) {
	leaseID, err := state.NewUUID()
	if err != nil {
		return ExecutionLease{}, err
	}
	target := source
	target.LeaseID = leaseID
	target.ControllerGeneration = generation
	target.ExpectedBaseOID = snapshot.Head
	target.ExpectedWorkspaceSnapshotID = snapshot.ID
	target.InFlightCallID = ""
	target.CreatedAt = time.Now().UTC()
	if err := s.writeLease(target); err != nil {
		return ExecutionLease{}, err
	}
	return target, nil
}
