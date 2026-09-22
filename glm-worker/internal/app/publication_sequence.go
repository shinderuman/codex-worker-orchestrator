package app

import (
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/publicationsequence"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type PublicationActionSpec = publicationsequence.PublicationActionSpec

type PublicationSequenceFailure = publicationsequence.PublicationSequenceFailure

type PublicationSequence = publicationsequence.PublicationSequence

func ProjectPublicationSequence(repoRoot string, st *state.StateStore) PublicationSequence {
	return publicationsequence.ProjectPublicationSequence(repoRoot, st)
}
