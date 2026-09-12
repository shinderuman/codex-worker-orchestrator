package state

import (
	"errors"
	"os"
	"testing"
)

const (
	canonicalParentThreadID  = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	canonicalParentSessionID = "019fbc5d-8f7d-7ca2-8be6-19d85487311b"
	legacyParentThreadID     = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	legacyParentSessionID    = "01a04f5d-bbf3-7773-9792-61d5aa28e2f9"
)

func TestSetParentCodexIdentityDoesNotPersistIntoCurrentTaskStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(canonicalParentThreadID, canonicalParentSessionID, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if raw.ParentCodexThreadID != "" || raw.ParentCodexSessionID != "" {
		t.Fatalf("live TaskStats persisted parent identity: %#v", raw)
	}

	projected, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if projected.TaskID != taskID || projected.ParentCodexThreadID != canonicalParentThreadID || projected.ParentCodexSessionID != canonicalParentSessionID {
		t.Fatalf("canonical identity projection = %#v", projected)
	}
}

func TestParentCodexIdentityDoesNotFallbackToLegacyTaskStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ParentCodexThreadID = legacyParentThreadID
	stats.ParentCodexSessionID = legacyParentSessionID
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CurrentParentCodexIdentity(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy TaskStats became current identity: %v", err)
	}
	projected, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if projected.ParentCodexThreadID != "" || projected.ParentCodexSessionID != "" {
		t.Fatalf("legacy TaskStats leaked into current projection: %#v", projected)
	}

	if err := st.SetParentCodexIdentity(canonicalParentThreadID, canonicalParentSessionID, nil); err != nil {
		t.Fatalf("legacy TaskStats blocked canonical bind: %v", err)
	}
	identity, err := st.CurrentParentCodexIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if identity.ThreadID != canonicalParentThreadID || identity.SessionID != canonicalParentSessionID {
		t.Fatalf("canonical identity = %#v", identity)
	}
}

func TestCorruptParentCodexIdentityDoesNotFallbackToLegacyTaskStats(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ParentCodexThreadID = legacyParentThreadID
	stats.ParentCodexSessionID = legacyParentSessionID
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(parentCodexIdentityFile), []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := st.CurrentParentCodexIdentity(); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt canonical identity was recovered from TaskStats: %v", err)
	}
	projected, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if projected.ParentCodexThreadID != "" || projected.ParentCodexSessionID != "" {
		t.Fatalf("corrupt canonical identity fell back to TaskStats: %#v", projected)
	}
}

func TestStartNewTaskArchivesCanonicalParentCodexIdentity(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(canonicalParentThreadID, canonicalParentSessionID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(st.TaskStatsArchivePath(oldTaskID))
	if err != nil {
		t.Fatal(err)
	}
	archived, err := decodeTaskStats(data)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ParentCodexThreadID != canonicalParentThreadID || archived.ParentCodexSessionID != canonicalParentSessionID {
		t.Fatalf("archived canonical identity = %#v", archived)
	}
	if _, err := st.CurrentParentCodexIdentity(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new task inherited prior parent identity: %v", err)
	}
}

func TestStartNewTaskDoesNotArchiveLegacyTaskStatsIdentity(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	oldTaskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ParentCodexThreadID = legacyParentThreadID
	stats.ParentCodexSessionID = legacyParentSessionID
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(st.TaskStatsArchivePath(oldTaskID))
	if err != nil {
		t.Fatal(err)
	}
	archived, err := decodeTaskStats(data)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ParentCodexThreadID != "" || archived.ParentCodexSessionID != "" {
		t.Fatalf("legacy TaskStats identity was promoted into archive: %#v", archived)
	}
}

func TestArchiveCurrentStatsDoesNotPromoteLegacyIdentityWhenCanonicalIdentityIsCorrupt(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ParentCodexThreadID = legacyParentThreadID
	stats.ParentCodexSessionID = legacyParentSessionID
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path(parentCodexIdentityFile), []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	warnings, restore := captureStatsWarnings(t)
	defer restore()
	st.ArchiveCurrentStats()
	requireStatsWarningOperation(t, warnings, "archive投影")

	data, err := os.ReadFile(st.TaskStatsArchivePath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	archived, err := decodeTaskStats(data)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ParentCodexThreadID != "" || archived.ParentCodexSessionID != "" {
		t.Fatalf("corrupt canonical identity promoted legacy TaskStats identity into archive: %#v", archived)
	}
}
