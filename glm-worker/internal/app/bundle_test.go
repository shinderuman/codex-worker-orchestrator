package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseBundleCommand(t *testing.T) {
	command, err := ParseCommand([]string{"bundle"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeBundle || command.Payload != "" {
		t.Fatalf("command = %#v", command)
	}

	command, err = ParseCommand([]string{"bundle", "task-123"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeBundle || command.Payload != "task-123" {
		t.Fatalf("command = %#v", command)
	}
	if _, err := ParseCommand([]string{"bundle", "task-123", "extra"}); err == nil {
		t.Fatal("extra argumentを受理しました")
	}
}

func TestBundleCurrentTaskIncludesTaskSessionsStateAndAuthority(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	st.RecordModelCall(state.ReviewerRole, "haiku")

	writeBundleModelCall(t, st, taskID, "session-worker", state.WorkerRole, "worker-new")
	writeBundleModelCall(t, st, taskID, "session-reviewer", state.ReviewerRole, "reviewer-1")
	writeBundleEvent(t, st, taskID, "session-worker")
	writeBundleFile(t, st.RoundLogPath(taskID), "round\n")
	artifactDir, err := st.PrepareArtifactDir()
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(artifactDir, "report.json"), "{}\n")

	writeClaudeTranscript(t, cfg, "project-a", "session-worker", "worker transcript\n")
	writeClaudeTranscript(t, cfg, "project-a", "session-reviewer", "reviewer transcript\n")
	writeClaudeTranscript(t, cfg, "project-a", "session-other", "must not be included\n")

	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/014-test.md")
	writeBundleFile(t, filepath.Join(st.Path("."), "quality-gate", "run.log"), "pass\n")

	beforeStats, err := os.ReadFile(st.Path("task-stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	beforeStatus := st.ReadOr("task.status", "")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.TaskID != taskID || output.TaskStatus != string(state.TaskStatusRateLimited) {
		t.Fatalf("output = %#v", output)
	}
	if output.EvidenceStatus != "complete" || len(output.Missing) != 0 {
		t.Fatalf("evidence = %s missing=%v", output.EvidenceStatus, output.Missing)
	}
	if !slices.Equal(output.ClaudeSessionIDs, []string{"session-reviewer", "session-worker"}) {
		t.Fatalf("sessions = %v", output.ClaudeSessionIDs)
	}
	if !filepath.IsAbs(output.ArchivePath) {
		t.Fatalf("archive path is not absolute: %s", output.ArchivePath)
	}
	expectedDir := filepath.Join(filepath.Dir(cfg.StateBase), "exports", cfg.RepoHash)
	if filepath.Dir(output.ArchivePath) != expectedDir {
		t.Fatalf("archive dir = %s, want %s", filepath.Dir(output.ArchivePath), expectedDir)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	for _, required := range []string{
		"manifest.json",
		"task/telemetry/" + taskID + ".jsonl",
		"task/events/" + taskID + ".jsonl",
		"task/rounds/" + taskID + ".jsonl",
		"task/task-stats.json",
		"task/artifacts/" + taskID + "/report.json",
		"claude-transcripts/session-worker.jsonl",
		"claude-transcripts/session-reviewer.jsonl",
		"current-state/state/task.id",
		"current-state/state/task.status",
		"current-state/status.json",
		"current-state/diagnostics/quality-gate/run.log",
		"current-state/repository-authority/IMPLEMENTATION_PLAN.local.md",
		"current-state/repository-authority/IMPLEMENTATION_RULES.md",
		"current-state/repository-authority/IMPLEMENTATION_HISTORY.md",
		"current-state/repository-authority/IMPLEMENTATION_TASKS/014-test.md",
	} {
		if _, ok := archive[required]; !ok {
			t.Errorf("archive missing %s", required)
		}
	}
	for name := range archive {
		if strings.Contains(name, "session-other") {
			t.Fatalf("unrelated transcript included: %s", name)
		}
	}
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if !manifest.CurrentTask || manifest.TaskID != taskID || manifest.EvidenceStatus != "complete" {
		t.Fatalf("manifest = %#v", manifest)
	}

	afterStats, err := os.ReadFile(st.Path("task-stats.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeStats, afterStats) || st.ReadOr("task.status", "") != beforeStatus {
		t.Fatal("bundle mutated task state")
	}
}

func TestBundleHistoricalTaskUsesTaskScopedEvidenceOnly(t *testing.T) {
	cfg, st := newBundleTestState(t)
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, oldTaskID, "session-old", state.WorkerRole, "worker-new")
	writeClaudeTranscript(t, cfg, "old-project", "session-old", "old\n")
	writeBundleFile(t, st.RoundLogPath(oldTaskID), "old-round\n")

	time.Sleep(time.Millisecond)
	newTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, newTaskID, "session-new", state.WorkerRole, "worker-new")
	writeClaudeTranscript(t, cfg, "new-project", "session-new", "new\n")
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: oldTaskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.TaskID != oldTaskID {
		t.Fatalf("task = %s", output.TaskID)
	}
	if !slices.Equal(output.ClaudeSessionIDs, []string{"session-old"}) {
		t.Fatalf("sessions = %v", output.ClaudeSessionIDs)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	if _, ok := archive["task/stats/"+oldTaskID+".json"]; !ok {
		t.Fatal("archived task stats are missing")
	}
	if _, ok := archive["claude-transcripts/session-old.jsonl"]; !ok {
		t.Fatal("old transcript is missing")
	}
	for name := range archive {
		if strings.HasPrefix(name, "current-state/") || strings.Contains(name, "session-new") || strings.Contains(name, newTaskID) {
			t.Fatalf("historical bundle contains current-task evidence: %s", name)
		}
	}
}

func TestBundleRateLimitedTaskReportsMissingTranscriptWithoutMissingCurrentStats(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(state.WorkerRole, "opus")
	writeBundleModelCall(t, st, taskID, "session-missing", state.WorkerRole, "worker-explicit-fix")
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvidenceStatus != "incomplete" {
		t.Fatalf("evidence status = %s", output.EvidenceStatus)
	}
	if !slices.Contains(output.Missing, "claude-transcripts/session-missing.jsonl") {
		t.Fatalf("missing = %v", output.Missing)
	}
	for _, missing := range output.Missing {
		if strings.Contains(missing, "task-stats") {
			t.Fatalf("current task stats were falsely reported missing: %v", output.Missing)
		}
	}
	archive := readBundleArchive(t, output.ArchivePath)
	if _, ok := archive["task/task-stats.json"]; !ok {
		t.Fatal("current task stats were not included")
	}
}

func TestBundleWithoutCurrentTaskSelectsNewestArchivedTask(t *testing.T) {
	cfg, st := newBundleTestState(t)
	first, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	second, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("task IDs unexpectedly match")
	}
	if err := st.Reset(); err != nil {
		t.Fatal(err)
	}

	task, err := selectBundleTask(st, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != second || task.Current {
		t.Fatalf("selected = %#v", task)
	}
	_ = cfg
}

func TestBundleAtomicallyReplacesDeterministicArchive(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	var first bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &first, nil); err != nil {
		t.Fatal(err)
	}
	var firstOutput bundleOutput
	if err := json.Unmarshal(first.Bytes(), &firstOutput); err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, st.Path("new-state.txt"), "second export\n")
	var second bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: taskID}, cfg, nil, &second, nil); err != nil {
		t.Fatal(err)
	}
	var secondOutput bundleOutput
	if err := json.Unmarshal(second.Bytes(), &secondOutput); err != nil {
		t.Fatal(err)
	}
	if firstOutput.ArchivePath != secondOutput.ArchivePath {
		t.Fatalf("archive path changed: %s != %s", firstOutput.ArchivePath, secondOutput.ArchivePath)
	}
	archive := readBundleArchive(t, secondOutput.ArchivePath)
	if string(archive["current-state/state/new-state.txt"]) != "second export\n" {
		t.Fatal("replacement archive does not contain second export state")
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(secondOutput.ArchivePath), ".bundle-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary bundle files remain: %v", matches)
	}
}

func newBundleTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	root := t.TempDir()
	repoRoot := filepath.Join(root, "repo")
	if err := os.MkdirAll(repoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.AppConfig{
		RepoRoot:        repoRoot,
		RepoHash:        strings.Repeat("a", 64),
		RepoShort:       strings.Repeat("a", 12),
		StateBase:       filepath.Join(root, ".glm-worker", "sessions"),
		ClaudeConfigDir: filepath.Join(root, ".claude"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func writeBundleModelCall(t *testing.T, st *state.StateStore, taskID, sessionID string, role state.SessionRole, phase string) {
	t.Helper()
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		SessionID:   sessionID,
		Role:        role,
		Phase:       phase,
		ModelAlias:  "opus",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "success",
		Runtime: &state.CallRuntime{
			WorkerRevision:      "fixture-revision",
			ClaudeVersion:       "2.1.226",
			ClaudeVersionSource: "session-transcript",
		},
	})
}

func writeBundleLegacyModelCall(t *testing.T, st *state.StateStore, taskID, sessionID string, role state.SessionRole, phase string) {
	t.Helper()
	now := time.Now().UTC()
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:      taskID,
		CallType:    state.CallTypeTask,
		SessionID:   sessionID,
		Role:        role,
		Phase:       phase,
		ModelAlias:  "opus",
		StartedAt:   now,
		CompletedAt: now,
		Outcome:     "success",
	})
}

func writeBundleEvent(t *testing.T, st *state.StateStore, taskID, sessionID string) {
	t.Helper()
	if err := st.AppendTaskEvent(state.TaskEventRecord{
		TaskID:    taskID,
		SessionID: sessionID,
		Role:      string(state.WorkerRole),
		Phase:     "worker-new",
		Kind:      "result",
	}); err != nil {
		t.Fatal(err)
	}
}

func writeClaudeTranscript(t *testing.T, cfg config.AppConfig, project, sessionID, content string) {
	t.Helper()
	writeBundleFile(t, filepath.Join(cfg.ClaudeConfigDir, "projects", project, sessionID+".jsonl"), content)
}

func writeBundleAuthority(t *testing.T, cfg config.AppConfig, st *state.StateStore, activeTask string) {
	t.Helper()
	writeBundleFile(t, filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md"), "plan\n")
	writeBundleFile(t, filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_RULES.md"), "rules\n")
	writeBundleFile(t, filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_HISTORY.md"), "history\n")
	writeBundleFile(t, filepath.Join(cfg.RepoRoot, filepath.FromSlash(activeTask)), "task\n")
	if err := st.Write("active-task", activeTask); err != nil {
		t.Fatal(err)
	}
}

func writeBundleFile(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readBundleArchive(t *testing.T, archivePath string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	result := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		entry, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		if _, err := data.ReadFrom(entry); err != nil {
			_ = entry.Close()
			t.Fatal(err)
		}
		if err := entry.Close(); err != nil {
			t.Fatal(err)
		}
		result[file.Name] = data.Bytes()
	}
	return result
}
