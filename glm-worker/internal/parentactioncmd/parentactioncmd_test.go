package parentactioncmd

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const codexIdentityTestThreadID = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"

func TestDirectWorkerArgsStartUsesCurrentActiveTask(t *testing.T) {
	args := directWorkerArgs("start")
	if len(args) != 1 || args[0] != activeTaskRequest {
		t.Fatalf("start args = %#v", args)
	}
}

func TestPayloadWorkerArgsDecisionOwnsFraming(t *testing.T) {
	payload := []byte("判断\n`$'\"")
	args := payloadWorkerArgs("decision", payload, nil)
	if len(args) != 4 || args[0] != "--decision-stdin" || args[1] != strconv.Itoa(len(payload)) || args[2] != "--sha256" || len(args[3]) != 64 {
		t.Fatalf("decision args = %#v", args)
	}
}

func TestValidateFixOptionsMatchesProductionDomain(t *testing.T) {
	valid := []string{"--origin", "glm-reviewer", "--accepted-scope", "current-diff"}
	if err := validateFixOptions(valid); err != nil {
		t.Fatal(err)
	}
	for _, options := range [][]string{
		{"--origin", "invented"},
		{"--accepted-scope", "anything"},
		{"--origin", "codex-review", "--origin", "glm-reviewer"},
		{"--other", "value"},
		{"--origin"},
	} {
		if err := validateFixOptions(options); err == nil {
			t.Fatalf("invalid options accepted: %s", strings.Join(options, " "))
		}
	}
}

func TestCodexIdentityFromEnvRequiresCanonicalUUIDs(t *testing.T) {
	threadID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	sessionID := "01a0463c-d477-7410-9efd-cb34ff2e0b0e"
	t.Setenv("CODEX_THREAD_ID", threadID)
	t.Setenv("CODEX_SESSION_ID", sessionID)
	if gotThread, gotSession, ok := codexIdentityFromEnv(); !ok || gotThread != threadID || gotSession != sessionID {
		t.Fatalf("identity = %s %s %v", gotThread, gotSession, ok)
	}

	invalid := []struct{ thread, session string }{
		{"", sessionID},
		{threadID, ""},
		{"not-a-uuid", sessionID},
		{threadID, "01A0463C-D477-7410-9EFD-CB34FF2E0B0E"},
	}
	for _, env := range invalid {
		t.Setenv("CODEX_THREAD_ID", env.thread)
		t.Setenv("CODEX_SESSION_ID", env.session)
		if _, _, ok := codexIdentityFromEnv(); ok {
			t.Fatalf("不正なenv identityが受理されました: %#v", env)
		}
	}
}

func TestExecuteResumePersistsParentCodexIdentityBeforeWorkerRun(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	marker := writeParentActionWorkerStub(t, cfg, true)
	if err := execute(cfg, []string{"resume"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != codexIdentityTestThreadID {
		t.Fatalf("identity = %s", stats.ParentCodexThreadID)
	}
}

func TestExecuteStartPropagatesIdentityEnvToWorkerRun(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	marker := writeParentActionWorkerStubWithCheck(t, cfg,
		`test "$GLM_PARENT_ACTION_CODEX_THREAD_ID" = "`+codexIdentityTestThreadID+`" && test "$GLM_PARENT_ACTION_CODEX_SESSION_ID" = "`+codexIdentityTestThreadID+`"`)
	if err := execute(cfg, []string{"start"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != "" || stats.ParentCodexSessionID != "" {
		t.Fatalf("startはchild側保存へ置き換えられた前提でstatsへ直接書いています: %#v", stats)
	}
}

func TestExecuteStartWithoutIdentityEnvRunsChildWithoutPropagation(t *testing.T) {
	cfg, _ := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	marker := writeParentActionWorkerStubWithCheck(t, cfg,
		`test -z "$GLM_PARENT_ACTION_CODEX_THREAD_ID" && test -z "$GLM_PARENT_ACTION_CODEX_SESSION_ID"`)
	if err := execute(cfg, []string{"start"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
}

func TestExecuteResumeFailsClosedOnConflictingParentCodexIdentity(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SetParentCodexIdentity("01a0244a-4ee4-7e71-b2e1-dec3bdda2120", "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	marker := writeParentActionWorkerStub(t, cfg, true)
	if err := execute(cfg, []string{"resume"}, nil, nil); err == nil {
		t.Fatal("矛盾するidentityで実行が続行されました")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("fail closed後にworkerが実行されました")
	}
}

func TestExecuteWithoutCodexIdentityEnvLeavesStateUnchanged(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	marker := writeParentActionWorkerStub(t, cfg, false)
	if err := execute(cfg, []string{"resume"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentCodexThreadID != "" || stats.ParentCodexSessionID != "" {
		t.Fatalf("identity = %#v", stats)
	}
}

func TestExecuteResumeIsNotBlockedByCorruptedTaskStats(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := os.WriteFile(st.Path("task-stats.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	marker := writeParentActionWorkerStub(t, cfg, false)
	if err := execute(cfg, []string{"resume"}, nil, nil); err != nil {
		t.Fatalf("破損statsでresumeがblockされました: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
}

func newParentActionIdentityTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg, st := newParentActionTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func newParentActionTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  root,
		RepoHash:  strings.Repeat("a", 64),
		StateBase: filepath.Join(root, "sessions"),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func writeParentActionWorkerStub(t *testing.T, cfg config.AppConfig, identityRequiredAtRun bool) string {
	t.Helper()
	check := "grep -q parent_codex_thread_id \"$GLM_TEST_STATS\""
	if !identityRequiredAtRun {
		check = "! " + check
	}
	return writeParentActionWorkerStubWithCheck(t, cfg, check)
}

func writeParentActionWorkerStubWithCheck(t *testing.T, cfg config.AppConfig, check string) string {
	t.Helper()
	st := state.AttachStateStore(cfg)
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "invoked")
	script := "#!/bin/sh\n" + check + " || exit 3\ntouch \"$GLM_STUB_MARKER\"\n"
	stubPath := filepath.Join(binDir, "glm-worker")
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GLM_STUB_MARKER", marker)
	t.Setenv("GLM_TEST_STATS", st.Path("task-stats.json"))
	return marker
}
