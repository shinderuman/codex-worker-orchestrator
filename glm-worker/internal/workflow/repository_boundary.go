package workflow

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func (w *Workflow) captureRepositoryBoundary() (state.GitSnapshot, error) {
	active, err := w.repositoryHarnessActive()
	if err != nil {
		return state.GitSnapshot{}, err
	}
	if !active {
		capture := w.captureSnapshot
		if capture == nil {
			capture = state.CaptureGitSnapshot
		}
		return capture(w.config.RepoRoot)
	}
	capture := w.captureBoundarySnapshot
	if capture == nil {
		capture = state.CaptureRepositoryBoundarySnapshot
	}
	return capture(w.config.RepoRoot)
}

func (w *Workflow) attachStopRepositoryBoundary(checkpoint *state.ResumeCheckpoint) error {
	snapshot, err := w.captureRepositoryBoundary()
	checkpoint.SetStopRepositoryBoundary(snapshot)
	return err
}
