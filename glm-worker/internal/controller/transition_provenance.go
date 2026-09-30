package controller

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (s *Store) writeTransitionMutationProvenance(
	record TransitionRecord,
	targetLeaseID string,
	command string,
	outcome string,
	before WorkspaceSnapshot,
	after WorkspaceSnapshot,
) error {
	mutationID, err := state.NewUUID()
	if err != nil {
		return err
	}
	mutation := MutationRecord{
		SchemaVersion:    controllerSchemaVersion,
		MutationID:       mutationID,
		AttemptID:        record.SourceAttemptID,
		SourceLeaseID:    record.SourceLeaseID,
		TargetLeaseID:    targetLeaseID,
		SourceGeneration: record.SourceGeneration,
		TargetGeneration: record.TargetGeneration,
		Command:          command,
		Outcome:          outcome,
		Before:           before,
		After:            after,
		Surfaces:         changedSurfaces(before, after),
		CreatedAt:        time.Now().UTC(),
	}
	return writeJSONAtomic(s.mutationPath(mutationID), mutation)
}
