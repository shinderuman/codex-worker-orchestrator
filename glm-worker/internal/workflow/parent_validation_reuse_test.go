package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunParentValidationGateReusesCurrentTaskExactSnapshotPass(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	runID := strings.Repeat("a", 32)
	writeParentValidationReuseRun(t, w, qualitygate.RunRecord{
		ValidationRunID: runID,
		Form:            packet.ParentValidationGoTest,
		WorkingDir:      workingDir,
		TaskID:          taskID,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          qualitygate.StatusPass,
		ExitSource:      state.ValidationExitSourceTarget,
	})

	got, err := w.runParentValidationGate(packet.ParentValidationRequest{
		Form:       packet.ParentValidationGoTest,
		WorkingDir: "glm-worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.ValidationRunID != runID || got.Status != qualitygate.StatusPass {
		t.Fatalf("reused validation = %#v", got)
	}
}

func TestReusableParentValidationPassRequiresSameEvidenceIdentity(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	otherWorkingDir := filepath.Join(w.config.RepoRoot, "other")
	for _, dir := range []string{workingDir, otherWorkingDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	writeParentValidationReuseRun(t, w, qualitygate.RunRecord{
		ValidationRunID: strings.Repeat("b", 32),
		Form:            packet.ParentValidationGoTest,
		WorkingDir:      workingDir,
		TaskID:          taskID,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		StartedAt:       time.Now().UTC(),
		Status:          qualitygate.StatusPass,
		ExitSource:      state.ValidationExitSourceTarget,
	})

	request := packet.ParentValidationRequest{Form: packet.ParentValidationGoTest, WorkingDir: "glm-worker"}
	if _, ok := w.reusableParentValidationPass(request, workingDir); !ok {
		t.Fatal("exact current-task snapshot PASS was not reusable")
	}
	changed := fixedSnapshot
	changed.WorktreeDigest = "changed-worktree"
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return changed, nil }
	if _, ok := w.reusableParentValidationPass(request, workingDir); ok {
		t.Fatal("changed snapshot reused stale PASS")
	}
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }
	if _, ok := w.reusableParentValidationPass(packet.ParentValidationRequest{Form: packet.ParentValidationGoTestRace}, workingDir); ok {
		t.Fatal("different validation form reused PASS")
	}
	if _, ok := w.reusableParentValidationPass(request, otherWorkingDir); ok {
		t.Fatal("different validation working directory reused PASS")
	}
}

func TestReusableParentValidationPassDoesNotRevivePassBehindLaterFailure(t *testing.T) {
	st := newStateStoreT(t)
	w := newWorkflowT(t, st, &scriptedRunner{})
	workingDir := filepath.Join(w.config.RepoRoot, "glm-worker")
	if err := os.MkdirAll(workingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC()
	writeParentValidationReuseRun(t, w, qualitygate.RunRecord{
		ValidationRunID: strings.Repeat("c", 32),
		Form:            packet.ParentValidationGoTest,
		WorkingDir:      workingDir,
		TaskID:          taskID,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		StartedAt:       started,
		Status:          qualitygate.StatusPass,
		ExitSource:      state.ValidationExitSourceTarget,
	})
	writeParentValidationReuseRun(t, w, qualitygate.RunRecord{
		ValidationRunID: strings.Repeat("d", 32),
		Form:            packet.ParentValidationGoTest,
		WorkingDir:      workingDir,
		TaskID:          taskID,
		Head:            fixedSnapshot.Head,
		IndexDigest:     fixedSnapshot.IndexDigest,
		WorktreeDigest:  fixedSnapshot.WorktreeDigest,
		StartedAt:       started.Add(time.Second),
		Status:          qualitygate.StatusFail,
		ExitCode:        1,
		ExitSource:      state.ValidationExitSourceTarget,
	})

	if _, ok := w.reusableParentValidationPass(packet.ParentValidationRequest{Form: packet.ParentValidationGoTest}, workingDir); ok {
		t.Fatal("older PASS was reused despite newer same-identity failure")
	}
}

func writeParentValidationReuseRun(t *testing.T, w *Workflow, record qualitygate.RunRecord) {
	t.Helper()
	repository, err := filepath.EvalSymlinks(w.config.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	workingDir, err := filepath.EvalSymlinks(record.WorkingDir)
	if err != nil {
		t.Fatal(err)
	}
	record.Repository = repository
	record.WorkingDir = workingDir
	completed := record.StartedAt.Add(time.Second)
	record.CompletedAt = &completed
	if record.Status == qualitygate.StatusPass {
		record.ExitCode = 0
		logPath := w.state.Path(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID, qualitygate.RunLog))
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(logPath, []byte("pass\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		record.Log = logPath
	}
	raw, err := qualitygate.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	runPath := w.state.Path(qualitygate.RunRelativePath(record.ValidationRunID))
	if err := os.MkdirAll(filepath.Dir(runPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
