package app

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBundleCurrentTaskCapturesFreshDiffLiveStateAndProvenance(t *testing.T) {
	cfg, st := newBundleTestState(t)
	runBundleGit(t, cfg.RepoRoot, "init", "-q")
	runBundleGit(t, cfg.RepoRoot, "config", "user.email", "test@example.com")
	runBundleGit(t, cfg.RepoRoot, "config", "user.name", "Test")

	activeTask := "IMPLEMENTATION_TASKS/014-test.md"
	for path, content := range map[string]string{
		"IMPLEMENTATION_PLAN.local.md": "plan\n",
		"IMPLEMENTATION_RULES.md":      "rules\n",
		"IMPLEMENTATION_HISTORY.md":    "history\n",
		activeTask:                     "task\n",
		"tracked.txt":                  "before\n",
	} {
		writeBundleFile(t, filepath.Join(cfg.RepoRoot, filepath.FromSlash(path)), content)
	}
	runBundleGit(t, cfg.RepoRoot, "add", ".")
	runBundleGit(t, cfg.RepoRoot, "commit", "-qm", "baseline")
	head := strings.TrimSpace(runBundleGit(t, cfg.RepoRoot, "rev-parse", "HEAD"))

	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"baseline-head":   head,
		"baseline-status": "clean",
		"active-task":     activeTask,
	} {
		if err := st.Write(name, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"baseline-index.patch", "baseline-worktree.patch", "baseline-untracked"} {
		writeBundleFile(t, st.Path(name), "")
	}

	st.RecordModelCall(state.WorkerRole, "opus")
	if err := st.AppendTaskEvent(state.TaskEventRecord{
		TaskID:    taskID,
		CallID:    "call-active",
		SessionID: "session-active",
		Role:      string(state.WorkerRole),
		Phase:     "worker-new",
		Kind:      "assistant",
	}); err != nil {
		t.Fatal(err)
	}
	writeClaudeTranscript(t, cfg, "project-a", "session-active", "active transcript\n")
	if err := st.WriteTaskLiveStatus(taskID, state.TaskLiveStatus{
		UpdatedAt:           time.Now().UTC(),
		LastEventAt:         time.Now().UTC(),
		LastModelActivityAt: time.Now().UTC(),
		Tools:               []state.TaskLiveTool{{ToolID: "tool-1", Purpose: "test"}},
	}); err != nil {
		t.Fatal(err)
	}

	writeBundleFile(t, st.Path("review-current-task.patch"), "stale previous-task diff\n")
	writeBundleFile(t, filepath.Join(st.Path("."), "quality-gate", "run.log"), "pass\n")
	writeBundleFile(t, filepath.Join(cfg.RepoRoot, "tracked.txt"), "after\n")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.InFlightModelCalls != 1 {
		t.Fatalf("in-flight calls = %d, want 1", output.InFlightModelCalls)
	}
	if output.EvidenceStatus != "complete" {
		t.Fatalf("evidence status = %s missing=%v", output.EvidenceStatus, output.Missing)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	freshDiff := archive["current-state/snapshot/task-diff.patch"]
	if !bytes.Contains(freshDiff, []byte("+after")) {
		t.Fatalf("fresh diff missing current tracked change:\n%s", freshDiff)
	}
	if _, ok := archive["task/events/"+taskID+".live.json"]; !ok {
		t.Fatal("current live status is missing")
	}

	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Format != bundleFormat || manifest.InFlightModelCalls != 1 {
		t.Fatalf("manifest = %#v", manifest)
	}
	for _, want := range []string{
		"current-state/state/review-current-task.patch",
		"current-state/diagnostics/quality-gate/run.log",
	} {
		if !slices.Contains(manifest.Unattributed, want) {
			t.Fatalf("unattributed missing %s: %v", want, manifest.Unattributed)
		}
	}
}

func TestBundleHistoricalTaskNeverReportsInFlightCalls(t *testing.T) {
	task := bundleTask{Current: false, Stats: state.TaskStats{ModelCalls: 3}}
	if got := bundleInFlightModelCalls(nil, task); got != 0 {
		t.Fatalf("historical in-flight calls = %d, want 0", got)
	}
}

func runBundleGit(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
	return string(output)
}
