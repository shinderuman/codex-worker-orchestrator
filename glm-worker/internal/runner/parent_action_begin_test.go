package runner

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentActionBeginRecoverySurvivesPreCallGuardRejection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "accepted")
	promptDir := t.TempDir()
	writeInstructionGuardFile(t, promptDir, "WORKER.md", "system")
	markerPath := filepath.Join(t.TempDir(), "model-call-marker")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nprintf '%s\\n' model-called >\"" + markerPath + "\"\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}

	base := NewClaudeRunner(config.AppConfig{
		RepoRoot:        root,
		RepoShort:       "guarded",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
	}, st)
	if _, err := base.prepareInstructionSurfaceGuard(); err != nil {
		t.Fatal(err)
	}
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "parent-rotated")

	guarded := NewInstructionSurfaceGuardRunner(base)
	_, err := guarded.Run(state.WorkerRole, "worker-decision", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output"))
	var guardErr *InstructionSurfaceGuardError
	if !errors.As(err, &guardErr) || guardErr.Stage != "before-call-mismatch" {
		t.Fatalf("guarded run error = %#v", err)
	}
	if _, statErr := os.Lstat(markerPath); !os.IsNotExist(statErr) {
		t.Fatalf("model command ran before admission: %v", statErr)
	}

	target, recoverErr := st.RecoverParentActionBeginFromState()
	if recoverErr != nil {
		t.Fatal(recoverErr)
	}
	if target != state.TaskStatusWaitingDecision || st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("recovered state: target=%s status=%s", target, st.TaskStatus())
	}
}

func TestParentActionBeginRecoverySurvivesRunnerSetupFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "accepted")
	promptDir := t.TempDir()
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}

	base := NewClaudeRunner(config.AppConfig{
		RepoRoot:        root,
		RepoShort:       "guarded",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
	}, st)
	guarded := NewInstructionSurfaceGuardRunner(base)
	if _, err := guarded.Run(state.WorkerRole, "worker-decision", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output")); err == nil {
		t.Fatal("runner setup unexpectedly succeeded without WORKER.md")
	}

	target, err := st.RecoverParentActionBeginFromState()
	if err != nil {
		t.Fatal(err)
	}
	if target != state.TaskStatusWaitingDecision || st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("recovered state after setup failure: target=%s status=%s", target, st.TaskStatus())
	}
}

func TestParentActionBeginRecordIsCommittedBeforeModelInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	root := t.TempDir()
	writeInstructionGuardFile(t, root, "AGENTS.local.md", "accepted")
	promptDir := t.TempDir()
	writeInstructionGuardFile(t, promptDir, "WORKER.md", "system")
	markerPath := filepath.Join(t.TempDir(), "begin-state-marker")
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	commandScript := "#!/bin/sh\nif [ -e \"$BEGIN_PATH\" ]; then\n  printf '%s\\n' present >\"" + markerPath + "\"\nelse\n  printf '%s\\n' absent >\"" + markerPath + "\"\nfi\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"runner output\"}'\n"
	if err := os.WriteFile(commandPath, []byte(commandScript), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEGIN_PATH", st.Path("parent-action-begin.json"))

	base := NewClaudeRunner(config.AppConfig{
		RepoRoot:        root,
		RepoShort:       "guarded",
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
		EnvAllowlist:    []string{"BEGIN_PATH"},
	}, st)
	guarded := NewInstructionSurfaceGuardRunner(base)
	if _, err := guarded.Run(state.WorkerRole, "worker-decision", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "output")); err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(marker) != "absent\n" {
		t.Fatalf("model observed begin record = %q", marker)
	}
	if st.TaskStatus() != state.TaskStatusActive {
		t.Fatalf("status = %s want active", st.TaskStatus())
	}
	if _, err := st.RecoverParentActionBeginFromState(); err == nil {
		t.Fatal("recovery remained available after model admission")
	}
}
