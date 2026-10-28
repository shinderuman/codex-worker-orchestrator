package parentactioncmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newAttemptTimingConfig(t *testing.T) config.AppConfig {
	t.Helper()
	home := t.TempDir()
	return config.AppConfig{
		StateBase: filepath.Join(home, "sessions"),
		RepoHash:  "attempttiminghash",
		RepoRoot:  home,
	}
}

func TestBeginParentActionAttemptExportsWorkerBindingEnv(t *testing.T) {
	t.Setenv(state.ParentActionAttemptIDEnv, "")
	t.Setenv(state.ParentActionAttemptEnterEnv, "")
	attempt := beginParentActionAttempt()
	attempt.exportEnv()

	if !state.ValidGeneratedUUID(attempt.attemptID) {
		t.Fatalf("attempt idが生成UUIDではありません: %q", attempt.attemptID)
	}
	enteredEnv := os.Getenv(state.ParentActionAttemptIDEnv)
	if enteredEnv != attempt.attemptID {
		t.Fatalf("worker binding env = %q want %q", enteredEnv, attempt.attemptID)
	}
	enteredAt, err := time.Parse(time.RFC3339Nano, os.Getenv(state.ParentActionAttemptEnterEnv))
	if err != nil || enteredAt.IsZero() {
		t.Fatalf("worker binding entry時刻が読めません: %q %v", os.Getenv(state.ParentActionAttemptEnterEnv), err)
	}
}

func TestRecordParentActionAttemptExitWritesLedger(t *testing.T) {
	cfg := newAttemptTimingConfig(t)
	attempt := parentActionAttemptScope{attemptID: "01a0463c-d477-7410-9efd-cb34ff2e0b0e", enteredAt: time.Now().UTC().Add(-2 * time.Second)}

	attempt.recordExit(cfg, "start", nil)
	attempt.recordExit(cfg, "decision", errors.New("decision packetが無効です"))

	records, err := state.AttachStateStore(cfg).ReadParentActionAttempts()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("ledger件数 = %d", len(records))
	}
	if records[0].Action != "start" || records[0].Outcome != state.ParentActionAttemptOutcomeSuccess || records[0].DurationMS <= 0 {
		t.Fatalf("成功記録 = %#v", records[0])
	}
	if records[1].Outcome != state.ParentActionAttemptOutcomeFailed || records[1].Error == "" {
		t.Fatalf("失敗記録 = %#v", records[1])
	}
	for _, record := range records {
		if record.TaskID != "" {
			t.Fatalf("task未生成時点の記録へtask idが付いています: %q", record.TaskID)
		}
	}
}

func TestRunRecordsParentActionAttemptLedgerOnFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GLM_WORKER_HOME", home)
	binDir := t.TempDir()
	fakeGit := "#!/bin/sh\nexit 97\n"
	if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(fakeGit), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	var stdout, stderr bytes.Buffer
	code := Run([]string{"resume"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("基盤git失敗がexit 0になりました")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	records, ledgerErr := state.AttachStateStore(cfg).ReadParentActionAttempts()
	if ledgerErr != nil {
		t.Fatal(ledgerErr)
	}
	if len(records) != 1 {
		t.Fatalf("失敗actionのledger記録 = %d want 1", len(records))
	}
	if records[0].Action != "resume" || records[0].Outcome != state.ParentActionAttemptOutcomeFailed {
		t.Fatalf("失敗ledger記録 = %#v", records[0])
	}
	if records[0].DurationMS < 0 {
		t.Fatalf("ledger duration = %d", records[0].DurationMS)
	}
}

func TestRejectedStartAttemptDoesNotReuseResidualTaskIdentity(t *testing.T) {
	cfg := newAttemptTimingConfig(t)
	st := state.AttachStateStore(cfg)
	if err := st.Write("task.id", "01a0463c-d477-4410-9efd-cb34ff2e0b0e"); err != nil {
		t.Fatal(err)
	}
	attempt := beginParentActionAttempt()
	attempt.recordExit(cfg, actionStart, errors.New("pre-model拒否"))
	records, err := st.ReadParentActionAttempts()
	if err != nil || len(records) != 1 || records[0].TaskID != "" {
		t.Fatalf("拒否されたstartが残留Taskへ帰属しました: %+v %v", records, err)
	}
}
