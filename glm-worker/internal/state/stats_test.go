package state

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStatsWarnings(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	previous := statsWarnOut
	statsWarnOut = &buf
	return &buf, func() { statsWarnOut = previous }
}

func capturedWarnings(t *testing.T, buf *bytes.Buffer) []statsWarningEvent {
	t.Helper()
	var events []statsWarningEvent
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var event statsWarningEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("warning行がJSON eventとして読めません: %q: %v", line, err)
		}
		if event.Type != "warning" {
			t.Fatalf("warning行のtypeが違います: %q", line)
		}
		if event.Message == "" {
			t.Fatalf("warning行にmessageがありません: %q", line)
		}
		events = append(events, event)
	}
	return events
}

func requireStatsWarning(t *testing.T, buf *bytes.Buffer, scope string) {
	t.Helper()
	for _, event := range capturedWarnings(t, buf) {
		if event.Scope == scope {
			return
		}
	}
	t.Fatalf("scope %qのwarningが出ませんでした: %q", scope, buf.String())
}

func writeCorruptedTaskStats(t *testing.T, st *StateStore) {
	t.Helper()
	if err := os.WriteFile(st.Path(currentStatsFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestStartNewTaskContinuesWithCorruptedStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	first, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	writeCorruptedTaskStats(t, st)

	warn, restore := captureStatsWarnings(t)
	defer restore()

	second, err := st.StartNewTask()
	if err != nil {
		t.Fatalf("StartNewTaskが破損mirrorで停止しました: %v", err)
	}
	if first == second {
		t.Fatal("taskがrotateしませんでした")
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("task.status = %q", st.TaskStatus())
	}
	requireStatsWarning(t, warn, "task_stats")
}

func TestSetTaskStatusContinuesWithCorruptedStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeCorruptedTaskStats(t, st)

	warn, restore := captureStatsWarnings(t)
	defer restore()

	if err := st.SetTaskStatus(TaskStatusComplete); err != nil {
		t.Fatalf("SetTaskStatusが破損mirrorで停止しました: %v", err)
	}
	if st.TaskStatus() != TaskStatusComplete {
		t.Fatalf("正規状態 task.status = %q", st.TaskStatus())
	}
	requireStatsWarning(t, warn, "task_stats")
}

func TestResetContinuesWithCorruptedStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeCorruptedTaskStats(t, st)

	warn, restore := captureStatsWarnings(t)
	defer restore()

	if err := st.Reset(); err != nil {
		t.Fatalf("Resetが破損mirrorで停止しました: %v", err)
	}
	requireStatsWarning(t, warn, "task_stats")
	if st.TaskStatus() != TaskStatus("none") {
		t.Fatalf("reset後の task.status = %q", st.TaskStatus())
	}
}

func TestRecordModelCallContinuesWithCorruptedStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeCorruptedTaskStats(t, st)

	warn, restore := captureStatsWarnings(t)
	defer restore()

	st.RecordModelCall(WorkerRole, "opus")

	requireStatsWarning(t, warn, "task_stats")
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("正規状態 task.status = %q", st.TaskStatus())
	}
}

func TestStartNewTaskContinuesWhenArchiveWriteFails(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	first, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	statsDir := filepath.Join(st.dir, "stats")
	if err := os.MkdirAll(statsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(statsDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(statsDir, 0o700) }()

	warn, restore := captureStatsWarnings(t)
	defer restore()

	second, err := st.StartNewTask()
	if err != nil {
		t.Fatalf("StartNewTaskがarchive書き込み失敗で停止しました: %v", err)
	}
	if first == second {
		t.Fatal("taskがrotateしませんでした")
	}
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("task.status = %q", st.TaskStatus())
	}
	requireStatsWarning(t, warn, "task_stats")
}

func TestUpdateTaskStatsToleratesWriteFailure(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(st.dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(st.dir, 0o700) }()

	warn, restore := captureStatsWarnings(t)
	defer restore()

	st.RecordModelCall(WorkerRole, "opus")

	requireStatsWarning(t, warn, "task_stats")
	if st.TaskStatus() != TaskStatusActive {
		t.Fatalf("正規状態 task.status = %q", st.TaskStatus())
	}
}

func TestAllTaskStatsSurfacesCorruptedMirror(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeCorruptedTaskStats(t, st)

	if _, err := st.AllTaskStats(); err == nil {
		t.Fatal("明示 --stats は破損mirrorをエラーとして返す必要があります")
	}
}

func TestAllTaskStatsSkipsVersion1(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	currentTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(st.dir, "stats", "legacy.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"task_id":"legacy","model_calls":99,"input_tokens_by_alias":{"opus":999}}`
	if err := os.WriteFile(legacyPath, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].TaskID != currentTask || all[0].Version != taskStatsVersion {
		t.Fatalf("version 1を除外したstats = %#v", all)
	}
}

func TestUpdateTaskStatsRebuildsVersion1Mirror(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	legacy := `{"version":1,"task_id":"` + taskID + `","model_calls":99,"input_tokens_by_alias":{"opus":999}}`
	if err := os.WriteFile(st.Path(currentStatsFile), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	warnings, restore := captureStatsWarnings(t)
	defer restore()

	st.RecordModelCall(WorkerRole, "opus")

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Version != taskStatsVersion || stats.ModelCalls != 1 || stats.InputTokensByAlias["opus"] != 0 {
		t.Fatalf("version 1から再構築したstats = %#v", stats)
	}
	if warnings.Len() == 0 {
		t.Fatal("version 1を破棄したwarningがありません")
	}
}

func TestAllTaskStatsSkipsVersion2Archive(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	historyDir := filepath.Join(st.dir, "stats")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyArchive := `{"version":2,"task_id":"legacy","model_calls":5,"input_tokens_by_alias":{"opus":999},"output_tokens_by_alias":{"opus":111}}`
	if err := os.WriteFile(filepath.Join(historyDir, "legacy.json"), []byte(legacyArchive), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordModelCall(WorkerRole, "opus")
	st.RecordTransientRetry()
	st.RecordProbeOutcome("probe_failure")
	st.RecordRiskFloor("worker-declared")

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	var (
		modelCalls       int
		input, output    map[string]int64
		riskFloor, probe map[string]int
		transientRetries int
	)
	for _, s := range all {
		modelCalls += s.ModelCalls
		input = mergeInt64Test(input, s.InputTokensByAlias)
		output = mergeInt64Test(output, s.OutputTokensByAlias)
		riskFloor = mergeIntTest(riskFloor, s.RiskFloorByCategory)
		probe = mergeIntTest(probe, s.ProbeOutcome)
		transientRetries += s.TransientRetries
	}
	if len(all) != 1 {
		t.Fatalf("v2 archiveを除外したarchive数 = %d", len(all))
	}
	if modelCalls != 1 || input["opus"] != 0 || output["opus"] != 0 {
		t.Fatalf("v2 archiveの集計が混入している: modelCalls=%d input=%+v output=%+v", modelCalls, input, output)
	}
	if transientRetries != 1 || riskFloor["worker-declared"] != 1 || probe["probe_failure"] != 1 {
		t.Fatalf("現在mirrorの集計が正しくない: retries=%d floor=%+v probe=%+v", transientRetries, riskFloor, probe)
	}
}

func mergeIntTest(dst, src map[string]int) map[string]int {
	if dst == nil {
		dst = make(map[string]int)
	}
	for k, v := range src {
		dst[k] += v
	}
	return dst
}

func mergeInt64Test(dst, src map[string]int64) map[string]int64 {
	if dst == nil {
		dst = make(map[string]int64)
	}
	for k, v := range src {
		dst[k] += v
	}
	return dst
}

func TestSetParentCodexIdentityPersistsValidatesAndFailsClosed(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.SetParentCodexIdentity(threadID, sessionID); err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != threadID || stats.ParentCodexSessionID != sessionID || stats.TaskID != taskID {
		t.Fatalf("identity = %#v", stats)
	}

	if err := st.SetParentCodexIdentity(threadID, sessionID); err != nil {
		t.Fatalf("同一identityの再保存が失敗しました: %v", err)
	}

	if err := st.SetParentCodexIdentity("01a0244a-4ee4-7e71-b2e1-dec3bdda2120", sessionID); err == nil {
		t.Fatal("矛盾するidentityを上書きしました")
	}
	after, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.ParentCodexThreadID != threadID {
		t.Fatalf("fail closed後のidentity = %s", after.ParentCodexThreadID)
	}
}

func TestValidUUIDFormatAcceptsCodexThreadIDs(t *testing.T) {
	for _, id := range []string{
		"01a0463c-d477-7410-9efd-cb34ff2e0b0e",
		"019fbc5d-8f7d-7ca2-8be6-19d85487311b",
	} {
		if !ValidUUIDFormat(id) {
			t.Fatalf("Codex thread IDが拒否されました: %s", id)
		}
		if ValidGeneratedUUID(id) {
			t.Fatalf("UUIDv7が生成UUID検証を通りました: %s", id)
		}
	}
	for _, id := range []string{
		"",
		"01A0463C-D477-7410-9EFD-CB34FF2E0B0E",
		"01a0463cd47774109efdcb34ff2e0b0e",
		"01a0463c-d477-7410-9efd-cb34ff2e0b0g",
		"01a0463c-d477-7410-9efd-cb34ff2e0b0e-extra",
	} {
		if ValidUUIDFormat(id) {
			t.Fatalf("不正なUUIDが受理されました: %q", id)
		}
	}
}

func TestSetParentCodexIdentityToleratesStatsMirrorFailures(t *testing.T) {
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"

	t.Run("corrupted stats warn and continue", func(t *testing.T) {
		st := &StateStore{dir: t.TempDir()}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		writeCorruptedTaskStats(t, st)
		warnings, restore := captureStatsWarnings(t)
		defer restore()
		if err := st.SetParentCodexIdentity(threadID, sessionID); err != nil {
			t.Fatalf("破損statsで操作がblockされました: %v", err)
		}
		requireStatsWarning(t, warnings, "task_stats")
	})

	t.Run("missing stats skip without blocking", func(t *testing.T) {
		st := &StateStore{dir: t.TempDir()}
		if err := st.SetParentCodexIdentity(threadID, sessionID); err != nil {
			t.Fatalf("欠損statsで操作がblockされました: %v", err)
		}
	})

	t.Run("write failure warns and continues", func(t *testing.T) {
		root := t.TempDir()
		st := &StateStore{dir: root}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o700) })
		warnings, restore := captureStatsWarnings(t)
		defer restore()
		if err := st.SetParentCodexIdentity(threadID, sessionID); err != nil {
			t.Fatalf("書込失敗で操作がblockされました: %v", err)
		}
		requireStatsWarning(t, warnings, "task_stats")
	})
}
