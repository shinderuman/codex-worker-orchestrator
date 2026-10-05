package workflow

import (
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (w *Workflow) reusableParentValidationPass(request packet.ParentValidationRequest, workingDir string) (qualitygate.RunRecord, bool) {
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return qualitygate.RunRecord{}, false
	}
	repository, err := filepath.EvalSymlinks(w.config.RepoRoot)
	if err != nil {
		return qualitygate.RunRecord{}, false
	}
	taskID := w.state.ReadOr("task.id", "")
	if taskID == "" {
		return qualitygate.RunRecord{}, false
	}
	latest, found := w.latestParentValidationRun(request.Form, repository, workingDir, taskID, snapshot)
	if !found || qualitygate.VerifyTerminalPass(latest) != nil {
		return qualitygate.RunRecord{}, false
	}
	return latest, true
}

func (w *Workflow) latestParentValidationRun(form, repository, workingDir, taskID string, snapshot state.GitSnapshot) (qualitygate.RunRecord, bool) {
	entries, err := os.ReadDir(w.state.Path(qualitygate.RunDirectory))
	if err != nil {
		return qualitygate.RunRecord{}, false
	}
	var latest qualitygate.RunRecord
	found := false
	for _, entry := range entries {
		if !entry.IsDir() || !qualitygate.ValidRunID(entry.Name()) {
			continue
		}
		record, err := qualitygate.Read(w.state, entry.Name())
		if err != nil || !sameParentValidationRunIdentity(record, form, repository, workingDir, taskID, snapshot.Head, snapshot.IndexDigest, snapshot.WorktreeDigest) {
			continue
		}
		if !found || latest.StartedAt.Before(record.StartedAt) {
			latest = record
			found = true
		}
	}
	return latest, found
}

func sameParentValidationRunIdentity(record qualitygate.RunRecord, form, repository, workingDir, taskID, head, indexDigest, worktreeDigest string) bool {
	recordRepository, err := filepath.EvalSymlinks(record.Repository)
	if err != nil {
		return false
	}
	recordWorkingDir, err := filepath.EvalSymlinks(record.WorkingDir)
	if err != nil {
		return false
	}
	return record.Form == form &&
		record.TaskID == taskID &&
		filepath.Clean(recordRepository) == filepath.Clean(repository) &&
		filepath.Clean(recordWorkingDir) == filepath.Clean(workingDir) &&
		record.Head == head &&
		record.IndexDigest == indexDigest &&
		record.WorktreeDigest == worktreeDigest
}
