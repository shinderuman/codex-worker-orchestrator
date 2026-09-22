package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type DuplicateParentProjectionError = parentevidence.DuplicateProjectionError

type parentReadDecision = parentevidence.ReadDecision

type parentEvidenceReadScope struct {
	inner parentevidence.ReadScope
}

type parentReadRenderResult struct {
	bytes   int
	outcome string
	reason  string
}

const parentEvidenceBatchCommand = parentevidence.BatchCommand

const parentEvidenceUnchangedReason = parentevidence.UnchangedReason

const (
	parentReadServe     = parentevidence.ReadServe
	parentReadDuplicate = parentevidence.ReadDuplicate
)

func parentEvidenceLeaseActive(st *state.StateStore) bool {
	return parentevidence.LeaseActive(st)
}

func parentEvidenceStatusHasLease(status state.TaskStatus) bool {
	return parentevidence.StatusHasLease(status)
}

func captureParentEvidenceReadScope(st *state.StateStore) (parentEvidenceReadScope, error) {
	scope, err := parentevidence.CaptureReadScope(st)
	return parentEvidenceReadScope{inner: scope}, err
}

func validateParentEvidenceReadScope(st *state.StateStore, scope parentEvidenceReadScope) error {
	return parentevidence.ValidateReadScope(st, scope.inner)
}

func parentEvidenceDigest(value any) (string, int) {
	return parentevidence.Digest(value)
}

func parentEvidenceStringDigest(parts ...string) string {
	return parentevidence.StringDigest(parts...)
}

func parentEvidenceStorePresent(st *state.StateStore) bool {
	return parentevidence.StorePresent(st)
}

func recordParentEvidence(st *state.StateStore, record state.ParentEvidenceRecord) {
	parentevidence.Record(st, record)
}

func decideParentRead(st *state.StateStore, surface, digest string) (parentReadDecision, state.ParentEvidenceLedgerEntry, error) {
	return parentevidence.DecideRead(st, surface, digest)
}

func withParentEvidenceLedgerLock(st *state.StateStore, body func() error) error {
	return parentevidence.WithLedgerLock(st, body)
}

func withParentEvidenceReadScopeLock(st *state.StateStore, scope parentEvidenceReadScope, body func() error) error {
	return parentevidence.WithReadScopeLock(st, scope.inner, body)
}

func writeMeasuredJSON(stdout io.Writer, value any) (int, error) {
	return parentevidence.WriteMeasuredJSON(stdout, value)
}

func saveParentEvidenceLedger(st *state.StateStore, surface, digest, origin, ownerCallID string) error {
	return parentevidence.SaveLedger(st, surface, digest, origin, ownerCallID)
}

func finishParentReadInScope(st *state.StateStore, scope parentEvidenceReadScope, surface, digest string, render func() (int, error)) error {
	return parentevidence.FinishReadInScope(st, scope.inner, surface, digest, render)
}

func finishParentReadInScopeResult(st *state.StateStore, scope parentEvidenceReadScope, surface, digest string, render func() (parentReadRenderResult, error)) error {
	return parentevidence.FinishReadInScopeResult(st, scope.inner, surface, digest, func() (parentevidence.RenderResult, error) {
		result, err := render()
		return parentevidence.RenderResult{Bytes: result.bytes, Outcome: result.outcome, Reason: result.reason}, err
	})
}
