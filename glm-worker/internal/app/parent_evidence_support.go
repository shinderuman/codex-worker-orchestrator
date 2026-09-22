package app

import (
	"io"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentevidence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type DuplicateParentProjectionError = parentevidence.DuplicateProjectionError

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

func captureParentEvidenceReadScope(st *state.StateStore) (parentEvidenceReadScope, error) {
	scope, err := parentevidence.CaptureReadScope(st)
	return parentEvidenceReadScope{inner: scope}, err
}

func parentEvidenceDigest(value any) (string, int) {
	digest, bytes := parentevidence.Digest(value)
	return digest, bytes
}

func parentEvidenceStringDigest(parts ...string) string {
	digest := parentevidence.StringDigest(parts...)
	return digest
}

func writeMeasuredJSON(stdout io.Writer, value any) (int, error) {
	written, err := parentevidence.WriteMeasuredJSON(stdout, value)
	return written, err
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
