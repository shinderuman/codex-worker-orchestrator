package state

import (
	"os"
	"testing"
)

func TestSetParentCodexIdentityPersistsValidatesAndFailsClosed(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	if err := st.SetParentCodexIdentity(threadID, sessionID, nil); err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != threadID || stats.ParentCodexSessionID != sessionID || stats.TaskID != taskID {
		t.Fatalf("identity = %#v", stats)
	}

	if err := st.SetParentCodexIdentity(threadID, sessionID, nil); err != nil {
		t.Fatalf("同一identityの再保存が失敗しました: %v", err)
	}

	if err := st.SetParentCodexIdentity("01a0244a-4ee4-7e71-b2e1-dec3bdda2120", sessionID, nil); err == nil {
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

func TestSetParentCodexIdentityDoesNotDependOnTaskStatsMirror(t *testing.T) {
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"

	for _, tc := range []struct {
		name    string
		prepare func(*StateStore) error
	}{
		{name: "corrupted stats", prepare: func(st *StateStore) error {
			return os.WriteFile(st.Path(currentStatsFile), []byte("{not json"), 0o600)
		}},
		{name: "missing stats", prepare: func(st *StateStore) error {
			return st.Remove(currentStatsFile)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &StateStore{dir: t.TempDir()}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := tc.prepare(st); err != nil {
				t.Fatal(err)
			}
			if err := st.SetParentCodexIdentity(threadID, sessionID, nil); err != nil {
				t.Fatalf("TaskStats mirror状態でcanonical bindがblockされました: %v", err)
			}
			identity, err := st.CurrentParentCodexIdentity()
			if err != nil || identity.ThreadID != threadID || identity.SessionID != sessionID {
				t.Fatalf("canonical identity = %#v err=%v", identity, err)
			}
		})
	}
}
