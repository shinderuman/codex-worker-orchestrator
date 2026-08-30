package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	codexTestParentThreadID   = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	codexTestParentSessionID  = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	codexTestGuardianThreadID = "01a04f5d-bbf3-7773-9792-61d5aa28e2f9"
	codexTestOtherThreadID    = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
)

func TestBundleAssociatesCodexParentRolloutByIdentity(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-guardian-"+codexTestGuardianThreadID+".jsonl",
		start.Add(-30*time.Minute), codexTestGuardianThreadID, codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-30 * time.Minute), start.Add(30 * time.Second)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/29/rollout-stale-guardian.jsonl",
		start.Add(-3*time.Hour), "01a04970-95a0-7b32-b967-aebc018402cd", codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-3 * time.Hour), start.Add(-2 * time.Hour)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/29/rollout-other-parent.jsonl",
		start.Add(-time.Hour), codexTestOtherThreadID, "", "user", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/29/rollout-nonguardian-child.jsonl",
		start.Add(-5*time.Minute), "01a04971-95a0-7b32-b967-aebc018402cd", codexTestParentThreadID, "subagent", false,
		[]time.Time{start.Add(-5 * time.Minute), start.Add(time.Minute)})
	writeCodexConfigToml(t, codexHome, true)
	writeCodexChatProcesses(t, codexHome, codexTestParentThreadID, codexTestOtherThreadID)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.TaskID != taskID || output.EvidenceStatus != "complete" {
		t.Fatalf("output = %#v", output)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	parentPath := "codex-parent/rollouts/" + codexTestParentThreadID + ".jsonl"
	guardianPath := "codex-parent/guardians/" + codexTestGuardianThreadID + ".jsonl"
	for _, required := range []string{parentPath, guardianPath, "codex-parent/runtime-settings.json", "codex-parent/process-manager/chat_processes.json"} {
		if _, ok := archive[required]; !ok {
			t.Errorf("archive missing %s", required)
		}
	}
	for name := range archive {
		if strings.HasPrefix(name, "codex-parent/") &&
			name != parentPath && name != guardianPath &&
			name != "codex-parent/runtime-settings.json" && name != "codex-parent/process-manager/chat_processes.json" {
			t.Errorf("unrelated codex evidence included: %s", name)
		}
	}
	if !strings.Contains(string(archive[parentPath]), codexTestParentThreadID) {
		t.Error("parent rollout content is wrong")
	}
	var runtimeSettings map[string]int64
	if err := json.Unmarshal(archive["codex-parent/runtime-settings.json"], &runtimeSettings); err != nil {
		t.Fatal(err)
	}
	if runtimeSettings["background_terminal_max_timeout"] != 21600000 || len(runtimeSettings) != 1 {
		t.Fatalf("runtime settings = %#v", runtimeSettings)
	}
	var processes []map[string]any
	if err := json.Unmarshal(archive["codex-parent/process-manager/chat_processes.json"], &processes); err != nil {
		t.Fatal(err)
	}
	if len(processes) != 1 || processes[0]["conversationId"] != codexTestParentThreadID {
		t.Fatalf("process rows = %#v", processes)
	}

	manifest := readBundleManifest(t, archive)
	if manifest.Format != bundleFormat {
		t.Fatalf("format = %s", manifest.Format)
	}
	statuses := codexEvidenceStatuses(t, manifest)
	wantStatuses := map[string]string{
		codexClassParentSession:     codexStatusIncluded,
		codexClassGuardianChild:     codexStatusIncluded,
		codexClassAppServerLogs:     codexStatusUnavailable,
		codexClassProcessProjection: codexStatusIncluded,
		codexClassRuntimeSettings:   codexStatusIncluded,
		codexClassAttachments:       codexStatusUnavailable,
	}
	for class, want := range wantStatuses {
		if statuses[class] != want {
			t.Errorf("%s status = %s, want %s", class, statuses[class], want)
		}
	}
	for _, source := range manifest.CodexEvidence {
		associated := source.Class != codexClassRuntimeSettings && source.Class != codexClassAttachments
		if associated && source.AssociationBasis != codexAssociationBasis {
			t.Errorf("%s basis = %q", source.Class, source.AssociationBasis)
		}
		if !associated && source.AssociationBasis != "" {
			t.Errorf("%s basis = %q", source.Class, source.AssociationBasis)
		}
	}
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if !parent.SpansTasks || !slices.Equal(parent.ThreadIDs, []string{codexTestParentThreadID}) {
		t.Fatalf("parent source = %#v", parent)
	}
	guardian := findCodexEvidence(t, manifest, codexClassGuardianChild)
	if guardian.SpansTasks || !slices.Equal(guardian.ThreadIDs, []string{codexTestGuardianThreadID}) {
		t.Fatalf("guardian source = %#v", guardian)
	}
	if !strings.Contains(guardian.Detail, "1 direct guardian children") {
		t.Fatalf("guardian detail = %s", guardian.Detail)
	}
}

func TestBundleCodexParentAmbiguousWhenDuplicateRolloutIdentity(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	_, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "archived_sessions/rollout-archived-"+codexTestParentThreadID+".jsonl",
		start.Add(-2*time.Hour), codexTestParentThreadID, "", "user", false,
		[]time.Time{start.Add(-2 * time.Hour), start.Add(-time.Hour)})

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	for name := range archive {
		if strings.HasPrefix(name, "codex-parent/") {
			t.Errorf("ambiguous association produced codex evidence: %s", name)
		}
	}
	manifest := readBundleManifest(t, archive)
	for _, source := range manifest.CodexEvidence {
		if source.Class == codexClassRuntimeSettings || source.Class == codexClassAttachments {
			continue
		}
		if source.Status != codexStatusAmbiguous {
			t.Errorf("%s status = %s, want %s", source.Class, source.Status, codexStatusAmbiguous)
		}
	}
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if !strings.Contains(parent.Detail, "2 rollouts share the stored parent thread ID") {
		t.Fatalf("parent detail = %s", parent.Detail)
	}
}

func TestBundleCodexHistoricalTaskWithoutStoredIdentity(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	time.Sleep(time.Millisecond)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

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
	archive := readBundleArchive(t, output.ArchivePath)
	for name := range archive {
		if strings.HasPrefix(name, "codex-parent/") {
			t.Errorf("historical task without identity produced codex evidence: %s", name)
		}
	}
	manifest := readBundleManifest(t, archive)
	statuses := codexEvidenceStatuses(t, manifest)
	if statuses[codexClassParentSession] != codexStatusMissing || statuses[codexClassGuardianChild] != codexStatusMissing {
		t.Fatalf("statuses = %#v", statuses)
	}
	if statuses[codexClassProcessProjection] != codexStatusUnavailable {
		t.Fatalf("process projection status = %s", statuses[codexClassProcessProjection])
	}
}

func TestBundleCodexArchivedTaskKeepsStoredIdentity(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-guardian-"+codexTestGuardianThreadID+".jsonl",
		start.Add(-30*time.Minute), codexTestGuardianThreadID, codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-30 * time.Minute), start.Add(30 * time.Second)})
	archived, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle, Payload: oldTaskID}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	parentPath := "codex-parent/rollouts/" + codexTestParentThreadID + ".jsonl"
	if _, ok := archive[parentPath]; !ok {
		t.Fatalf("archived identity no longer associates the parent rollout: %s", parentPath)
	}
	manifest := readBundleManifest(t, archive)
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusIncluded || !slices.Equal(parent.ThreadIDs, []string{codexTestParentThreadID}) {
		t.Fatalf("parent source = %#v", parent)
	}
	guardian := findCodexEvidence(t, manifest, codexClassGuardianChild)
	if guardian.Status != codexStatusIncluded || !slices.Equal(guardian.ThreadIDs, []string{codexTestGuardianThreadID}) {
		t.Fatalf("guardian source = %#v", guardian)
	}
	historical, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range historical {
		if entry.TaskID == oldTaskID {
			if entry.ParentCodexThreadID != archived.ParentCodexThreadID {
				t.Fatalf("archived identity = %#v, want %#v", entry.ParentCodexThreadID, archived.ParentCodexThreadID)
			}
			return
		}
	}
	t.Fatalf("archived task %s not found in stats history", oldTaskID)
}

func TestBundleCodexLogExtractionBoundedByThreadAndTaskWindow(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cfg, st, codexHome := newCodexBundleTestState(t)
	_, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-guardian-"+codexTestGuardianThreadID+".jsonl",
		start.Add(-30*time.Minute), codexTestGuardianThreadID, codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-30 * time.Minute), start.Add(30 * time.Second)})
	writeCodexLogDatabase(t, codexHome, start)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	parentLog := "codex-parent/logs/" + codexTestParentThreadID + ".jsonl"
	guardianLog := "codex-parent/logs/" + codexTestGuardianThreadID + ".jsonl"
	if _, ok := archive[parentLog]; !ok {
		t.Fatalf("archive missing %s", parentLog)
	}
	if _, ok := archive[guardianLog]; !ok {
		t.Fatalf("archive missing %s", guardianLog)
	}
	assertCodexLogRows(t, archive[parentLog], []codexLogRow{
		{TS: start.Unix(), TSNanos: 100, Level: "INFO", Target: "codex_core::parent", ThreadID: codexTestParentThreadID, EstimatedBytes: 42},
	})
	assertCodexLogRows(t, archive[guardianLog], []codexLogRow{
		{TS: start.Unix(), TSNanos: 200, Level: "DEBUG", Target: "codex_core::guardian", ThreadID: codexTestGuardianThreadID, EstimatedBytes: 7},
	})
	manifest := readBundleManifest(t, archive)
	logs := findCodexEvidence(t, manifest, codexClassAppServerLogs)
	if logs.Status != codexStatusIncluded || !strings.Contains(logs.Detail, "extracted 2 rows") {
		t.Fatalf("logs source = %#v", logs)
	}
}

func TestBundleCodexLogFailureLeavesNoPartialLogEntries(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	cfg, st, codexHome := newCodexBundleTestState(t)
	_, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-guardian-foreign.jsonl",
		start.Add(-30*time.Minute), "guardian-thread-not-a-uuid", codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-30 * time.Minute), start.Add(30 * time.Second)})
	writeCodexLogDatabase(t, codexHome, start)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	for name := range archive {
		if strings.HasPrefix(name, "codex-parent/logs/") {
			t.Errorf("partial log entry remains after extraction failure: %s", name)
		}
	}
	if _, ok := archive["codex-parent/rollouts/"+codexTestParentThreadID+".jsonl"]; !ok {
		t.Fatal("parent rollout is missing")
	}
	manifest := readBundleManifest(t, archive)
	logs := findCodexEvidence(t, manifest, codexClassAppServerLogs)
	if logs.Status != codexStatusUnavailable {
		t.Fatalf("logs source = %#v", logs)
	}
	if len(logs.ArchivePaths) != 0 || len(logs.ThreadIDs) != 0 {
		t.Fatalf("unavailable logs source lists collected evidence: %#v", logs)
	}
}

func TestBundleCodexDuplicateGuardianThreadIDIsAmbiguous(t *testing.T) {
	for _, order := range []string{"live-first", "archived-first"} {
		t.Run(order, func(t *testing.T) {
			cfg, st, codexHome := newCodexBundleTestState(t)
			_, err := st.StartNewTask()
			if err != nil {
				t.Fatal(err)
			}
			stats, err := st.CurrentTaskStats()
			if err != nil {
				t.Fatal(err)
			}
			start := stats.StartedAt.UTC()
			if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
				t.Fatal(err)
			}
			writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
				start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
				[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
			guardianRollouts := []string{
				"sessions/2026/08/30/rollout-guardian-" + codexTestGuardianThreadID + ".jsonl",
				"archived_sessions/rollout-archived-" + codexTestGuardianThreadID + ".jsonl",
			}
			if order == "archived-first" {
				guardianRollouts[0], guardianRollouts[1] = guardianRollouts[1], guardianRollouts[0]
			}
			for _, rel := range guardianRollouts {
				writeCodexRollout(t, codexHome, rel,
					start.Add(-30*time.Minute), codexTestGuardianThreadID, codexTestParentThreadID, "guardian_review", true,
					[]time.Time{start.Add(-30 * time.Minute), start.Add(30 * time.Second)})
			}
			writeCodexChatProcesses(t, codexHome, codexTestParentThreadID, codexTestGuardianThreadID)

			var stdout bytes.Buffer
			if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
				t.Fatal(err)
			}
			var output bundleOutput
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			archive := readBundleArchive(t, output.ArchivePath)
			for name := range archive {
				if strings.HasPrefix(name, "codex-parent/guardians/") {
					t.Errorf("ambiguous guardian was collected: %s", name)
				}
			}
			if _, ok := archive["codex-parent/rollouts/"+codexTestParentThreadID+".jsonl"]; !ok {
				t.Fatal("parent evidence is not maintained")
			}
			var processes []map[string]any
			if err := json.Unmarshal(archive["codex-parent/process-manager/chat_processes.json"], &processes); err != nil {
				t.Fatal(err)
			}
			if len(processes) != 1 || processes[0]["conversationId"] != codexTestParentThreadID {
				t.Fatalf("ambiguous guardian leaked into the thread set: %#v", processes)
			}
			manifest := readBundleManifest(t, archive)
			guardian := findCodexEvidence(t, manifest, codexClassGuardianChild)
			if guardian.Status != codexStatusAmbiguous || len(guardian.ThreadIDs) != 0 || len(guardian.ArchivePaths) != 0 {
				t.Fatalf("guardian source = %#v", guardian)
			}
			parent := findCodexEvidence(t, manifest, codexClassParentSession)
			if parent.Status != codexStatusIncluded {
				t.Fatalf("parent source = %#v", parent)
			}
			process := findCodexEvidence(t, manifest, codexClassProcessProjection)
			if process.Status != codexStatusIncluded || !slices.Equal(process.ThreadIDs, []string{codexTestParentThreadID}) {
				t.Fatalf("process source = %#v", process)
			}
		})
	}
}

func TestBundleCodexRuntimeSettingsStatuses(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeCodexConfigToml(t, codexHome, false)

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	manifest := readBundleManifest(t, readBundleArchive(t, output.ArchivePath))
	runtime := findCodexEvidence(t, manifest, codexClassRuntimeSettings)
	if runtime.Status != codexStatusMissing || !strings.Contains(runtime.Detail, "is not present in config.toml") {
		t.Fatalf("runtime source = %#v", runtime)
	}

	if err := os.Remove(filepath.Join(codexHome, "config.toml")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	manifest = readBundleManifest(t, readBundleArchive(t, output.ArchivePath))
	runtime = findCodexEvidence(t, manifest, codexClassRuntimeSettings)
	if runtime.Status != codexStatusUnavailable {
		t.Fatalf("runtime source = %#v", runtime)
	}
}

func newCodexBundleTestState(t *testing.T) (config.AppConfig, *state.StateStore, string) {
	cfg, st := newBundleTestState(t)
	codexHome := filepath.Join(t.TempDir(), ".codex")
	cfg.CodexConfigDir = codexHome
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, st, codexHome
}

func writeCodexRollout(t *testing.T, home, rel string, metaTimestamp time.Time, id, parentThreadID, threadSource string, guardianSource bool, lineTimestamps []time.Time) {
	t.Helper()
	payload := map[string]any{
		"session_id":    id,
		"id":            id,
		"timestamp":     metaTimestamp.UTC().Format(time.RFC3339Nano),
		"cwd":           "/repo",
		"thread_source": threadSource,
		"source":        "vscode",
	}
	if parentThreadID != "" {
		payload["parent_thread_id"] = parentThreadID
	}
	if guardianSource {
		payload["source"] = map[string]any{"subagent": map[string]any{"other": "guardian"}}
	}
	var buffer bytes.Buffer
	encodeCodexLine(t, &buffer, map[string]any{
		"timestamp": metaTimestamp.UTC().Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload":   payload,
	})
	for _, lineTimestamp := range lineTimestamps {
		encodeCodexLine(t, &buffer, map[string]any{
			"timestamp": lineTimestamp.UTC().Format(time.RFC3339Nano),
			"type":      "event_msg",
		})
	}
	writeBundleFile(t, filepath.Join(home, filepath.FromSlash(rel)), buffer.String())
}

func encodeCodexLine(t *testing.T, buffer *bytes.Buffer, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	buffer.Write(encoded)
	buffer.WriteByte('\n')
}

func writeCodexConfigToml(t *testing.T, home string, withAllowlistScalar bool) {
	t.Helper()
	var builder strings.Builder
	if withAllowlistScalar {
		builder.WriteString("background_terminal_max_timeout = 21600000\n")
	}
	builder.WriteString("model = \"gpt\"\n\n[projects.example]\nbackground_terminal_max_timeout = 1\n")
	writeBundleFile(t, filepath.Join(home, "config.toml"), builder.String())
}

func writeCodexChatProcesses(t *testing.T, home, conversationID, otherConversationID string) {
	t.Helper()
	rows := []map[string]any{
		{"conversationId": conversationID, "turnId": "turn-1", "itemId": "item-1", "cwd": "/repo", "command": "glm-parent-action start", "osPid": nil, "startedAtMs": 1788066448000},
		{"conversationId": otherConversationID, "turnId": "turn-2", "itemId": "item-2", "cwd": "/other", "command": "unrelated", "osPid": nil, "startedAtMs": 1788066449000},
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	writeBundleFile(t, filepath.Join(home, "process_manager", "chat_processes.json"), string(encoded)+"\n")
}

func writeCodexLogDatabase(t *testing.T, home string, start time.Time) {
	t.Helper()
	statement := "CREATE TABLE logs (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, ts_nanos INTEGER NOT NULL, level TEXT NOT NULL, target TEXT NOT NULL, feedback_log_body TEXT, thread_id TEXT, process_uuid TEXT, estimated_bytes INTEGER NOT NULL DEFAULT 0);" +
		codexLogInsert(codexLogRow{TS: start.Add(-time.Hour).Unix(), TSNanos: 900, Level: "INFO", Target: "codex_core::parent", ThreadID: codexTestParentThreadID, EstimatedBytes: 99}) +
		codexLogInsert(codexLogRow{TS: start.Unix(), TSNanos: 100, Level: "INFO", Target: "codex_core::parent", ThreadID: codexTestParentThreadID, EstimatedBytes: 42}) +
		codexLogInsert(codexLogRow{TS: start.Unix(), TSNanos: 150, Level: "INFO", Target: "codex_core::other", ThreadID: codexTestOtherThreadID, EstimatedBytes: 50}) +
		codexLogInsert(codexLogRow{TS: start.Unix(), TSNanos: 200, Level: "DEBUG", Target: "codex_core::guardian", ThreadID: codexTestGuardianThreadID, EstimatedBytes: 7})
	var stderr bytes.Buffer
	cmd := exec.Command("sqlite3", filepath.Join(home, "logs_2.sqlite"), statement)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("fixture db creation failed: %v: %s", err, stderr.String())
	}
}

func codexLogInsert(row codexLogRow) string {
	return fmt.Sprintf(
		"INSERT INTO logs (ts, ts_nanos, level, target, feedback_log_body, thread_id, process_uuid, estimated_bytes) VALUES (%d, %d, '%s', '%s', NULL, '%s', NULL, %d);",
		row.TS, row.TSNanos, row.Level, row.Target, row.ThreadID, row.EstimatedBytes,
	)
}

func readBundleManifest(t *testing.T, archive map[string][]byte) bundleManifest {
	t.Helper()
	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func findCodexEvidence(t *testing.T, manifest bundleManifest, class string) bundleCodexSource {
	t.Helper()
	for _, source := range manifest.CodexEvidence {
		if source.Class == class {
			return source
		}
	}
	t.Fatalf("codex evidence class %s is missing", class)
	return bundleCodexSource{}
}

func codexEvidenceStatuses(t *testing.T, manifest bundleManifest) map[string]string {
	t.Helper()
	statuses := make(map[string]string, len(manifest.CodexEvidence))
	for _, source := range manifest.CodexEvidence {
		if _, duplicated := statuses[source.Class]; duplicated {
			t.Fatalf("codex evidence class %s is duplicated", source.Class)
		}
		statuses[source.Class] = source.Status
	}
	return statuses
}

func assertCodexLogRows(t *testing.T, data []byte, want []codexLogRow) {
	t.Helper()
	var got []codexLogRow
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var row codexLogRow
		if err := json.Unmarshal(line, &row); err != nil {
			t.Fatalf("log row decode failed: %v: %s", err, line)
		}
		got = append(got, row)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("log rows = %#v, want %#v", got, want)
	}
}
