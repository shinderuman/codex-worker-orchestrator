package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskLiveStatusRoundtripAndPermissions(t *testing.T) {
	st := newEventTestStore(t)
	updatedAt := time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC)
	status := TaskLiveStatus{
		UpdatedAt:           updatedAt,
		LastEventAt:         updatedAt.Add(-time.Minute),
		LastModelActivityAt: updatedAt.Add(-2 * time.Minute),
		Tools: []TaskLiveTool{{
			ToolID:     "toolu_1",
			Command:    "sleep 295",
			Purpose:    "Check fourth run at gate section",
			Background: false,
		}},
	}
	if err := st.WriteTaskLiveStatus("task-1", status); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(st.TaskLiveStatusPath("task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("live status権限 = %v", info.Mode().Perm())
	}
	encoded, err := os.ReadFile(st.TaskLiveStatusPath("task-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"version":1`) {
		t.Fatalf("current live status versionがありません: %s", encoded)
	}

	read, err := st.ReadTaskLiveStatus("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if !read.UpdatedAt.Equal(updatedAt) || !read.LastEventAt.Equal(updatedAt.Add(-time.Minute)) || !read.LastModelActivityAt.Equal(updatedAt.Add(-2*time.Minute)) {
		t.Fatalf("roundtrip時刻 = %#v", read)
	}
	if len(read.Tools) != 1 || read.Tools[0].ToolID != "toolu_1" || read.Tools[0].Command != "sleep 295" || read.Tools[0].Purpose != "Check fourth run at gate section" {
		t.Fatalf("roundtrip tools = %#v", read.Tools)
	}

	empty := TaskLiveStatus{LastEventAt: updatedAt}
	if err := st.WriteTaskLiveStatus("task-1", empty); err != nil {
		t.Fatal(err)
	}
	overwritten, err := st.ReadTaskLiveStatus("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(overwritten.Tools) != 0 || overwritten.UpdatedAt.IsZero() {
		t.Fatalf("上書き後snapshot = %#v", overwritten)
	}
}

func TestTaskLiveStatusPathStaysInManagedEventsDir(t *testing.T) {
	st := newEventTestStore(t)
	path := st.TaskLiveStatusPath("12345678-aaaa-bbbb-cccc-dddddddddddd")
	if filepath.Base(filepath.Dir(path)) != "events" || filepath.Base(path) != "12345678-aaaa-bbbb-cccc-dddddddddddd.live.json" {
		t.Fatalf("live snapshot path = %s", path)
	}

	if !strings.HasPrefix(path, st.Path("events")+string(os.PathSeparator)) {
		t.Fatalf("live snapshotがstate dir外を指しています: %s", path)
	}
	if strings.Contains(path, ".claude") {
		t.Fatalf("live snapshotがClaude Code内部pathへ依存しています: %s", path)
	}
}

func TestTaskLiveStatusMissingAndCorruptReturnError(t *testing.T) {
	st := newEventTestStore(t)
	if _, err := st.ReadTaskLiveStatus("task-1"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("不在snapshot読み = %v", err)
	}
	if err := os.MkdirAll(st.Path("events"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskLiveStatusPath("task-1"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReadTaskLiveStatus("task-1"); err == nil || !strings.Contains(err.Error(), "task live statusを読めません") {
		t.Fatalf("破損snapshot読み = %v", err)
	}
}

func TestIsModelActivityEventAcceptanceSet(t *testing.T) {
	modelActivity := []TaskEventRecord{
		{Kind: "assistant", Blocks: []TaskBlockSummary{{Type: "thinking", Bytes: 10}}},
		{Kind: "assistant", Blocks: []TaskBlockSummary{{Type: "text", Bytes: 10}}},
		{Kind: "assistant", Blocks: []TaskBlockSummary{{Type: "tool_use", Name: "Bash", ToolID: "toolu_1"}}},
		{Kind: "assistant", Blocks: []TaskBlockSummary{{Type: "text", Bytes: 5}, {Type: "tool_result", ToolID: "toolu_1"}}},
		{Kind: "system", Subtype: "thinking_tokens"},
	}
	for _, record := range modelActivity {
		if !IsModelActivityEvent(record) {
			t.Fatalf("model activityとして扱われるべきrecordです: %#v", record)
		}
	}

	nonModelActivity := []TaskEventRecord{
		{Kind: "system", Subtype: "init"},
		{Kind: "system", Subtype: "tool_progress"},
		{Kind: "system", Subtype: "task_notification"},
		{Kind: "user", Blocks: []TaskBlockSummary{{Type: "tool_result", ToolID: "toolu_1"}}},
		{Kind: "result", Subtype: "success"},
		{Kind: "assistant"},
		{Kind: "assistant", Blocks: []TaskBlockSummary{{Type: "server_tool_use", Name: "WebSearch"}}},
	}
	for _, record := range nonModelActivity {
		if IsModelActivityEvent(record) {
			t.Fatalf("model activityとして扱うべきでないrecordです: %#v", record)
		}
	}
}

func TestTaskLiveStatusRejectsUnversionedShape(t *testing.T) {
	st := newEventTestStore(t)
	obsolete := `{"updated_at":"2026-08-23T09:10:00Z","last_event_at":"2026-08-23T09:10:00Z","tools":[{"tool_id":"toolu_1","command":"sleep 295"}]}`
	if err := os.MkdirAll(st.Path("events"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.TaskLiveStatusPath("task-1"), []byte(obsolete+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.ReadTaskLiveStatus("task-1"); err == nil || !strings.Contains(err.Error(), "unsupported task live status version") {
		t.Fatalf("unversioned live statusを拒否していません: %v", err)
	}
}

func TestPruneTaskEventLogsRemovesSiblingLiveStatus(t *testing.T) {
	st := newEventTestStore(t)
	base := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	writeEventLogWithmtime(t, st, "old-a", base)
	writeEventLogWithmtime(t, st, "old-b", base.Add(time.Hour))
	if err := st.WriteTaskLiveStatus("old-a", TaskLiveStatus{LastEventAt: base}); err != nil {
		t.Fatal(err)
	}
	if err := st.WriteTaskLiveStatus("old-b", TaskLiveStatus{LastEventAt: base}); err != nil {
		t.Fatal(err)
	}

	st.PruneTaskEventLogs(1, "current")

	if _, err := os.Stat(st.TaskLiveStatusPath("old-a")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pruned旧taskのlive statusが残っています: %v", err)
	}
	if _, err := os.Stat(st.TaskLiveStatusPath("old-b")); err != nil {
		t.Fatalf("保持taskのlive statusが削除されました: %v", err)
	}
}
