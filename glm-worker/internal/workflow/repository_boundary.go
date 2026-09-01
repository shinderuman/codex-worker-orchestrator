package workflow

import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"

func (w *Workflow) captureRepositoryBoundary() (state.GitSnapshot, error) {
	capture := w.captureBoundarySnapshot
	if capture == nil {
		capture = state.CaptureRepositoryBoundarySnapshot
	}
	return capture(w.config.RepoRoot)
}

func (w *Workflow) attachStopRepositoryBoundary(checkpoint *state.ResumeCheckpoint) error {
	snapshot, err := w.captureRepositoryBoundary()
	if err != nil {
		return err
	}
	checkpoint.StopGitSnapshot = &snapshot
	checkpoint.StopParentFiles = snapshot.ParentFiles
	return nil
}
