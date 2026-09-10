package parentactioncmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const codexIdentityTestThreadID = "01a0463c-d477-7410-9efd-cb34ff2e0b0e"

func TestDirectWorkerArgsStartUsesCurrentActiveTask(t *testing.T) {
	args := directWorkerArgs("start")
	if len(args) != 1 || args[0] != activeTaskRequest {
		t.Fatalf("start args = %#v", args)
	}
}

func TestRunRejectsUnknownActionNonZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"made-up-action"}, &stdout, &stderr); code == 0 {
		t.Fatal("未知actionがexit 0で受理されました")
	}
	if stdout.Len() != 0 {
		t.Fatalf("失敗時にstdoutへ出力がありました: %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("拒否理由がstderrへ出力されていません")
	}
}

func TestPayloadWorkerArgsDecisionOwnsFraming(t *testing.T) {
	payload := []byte("判断\n`$'\"")
	args := payloadWorkerArgs("decision", payload, nil)
	if len(args) != 4 || args[0] != "--decision-stdin" || args[1] != strconv.Itoa(len(payload)) || args[2] != "--sha256" || len(args[3]) != 64 {
		t.Fatalf("decision args = %#v", args)
	}
}

func TestPayloadWorkerArgsMilestoneModes(t *testing.T) {
	payload := []byte(`{"milestones":[]}`)
	for action, wantMode := range map[string]string{
		actionStartMilestones:  "--execution-milestones-stdin",
		actionReviseMilestones: "--execution-milestones-revise-stdin",
	} {
		args := payloadWorkerArgs(action, payload, nil)
		if len(args) != 4 || args[0] != wantMode || args[1] != strconv.Itoa(len(payload)) || args[2] != "--sha256" || len(args[3]) != 64 {
			t.Fatalf("%s args = %#v", action, args)
		}
	}
}

func TestValidateFixOptionsMatchesProductionDomain(t *testing.T) {
	for _, valid := range [][]string{
		{"--origin", "glm-reviewer", "--accepted-scope", "current-diff"},
		{"--accepted-scope", "current-diff"},
	} {
		if err := validateFixOptions(valid); err != nil {
			t.Fatalf("valid options rejected: %v: %v", valid, err)
		}
	}
	for _, options := range [][]string{
		{"--origin", "invented"},
		{"--accepted-scope", "anything"},
		{"--origin", "codex-review", "--origin", "glm-reviewer"},
		{"--other", "value"},
		{"--origin"},
		{"--approval-only"},
		{"--accepted-scope", "current-diff", "--approval-only"},
	} {
		if err := validateFixOptions(options); err == nil {
			t.Fatalf("invalid options accepted: %s", strings.Join(options, " "))
		}
	}
}

func TestDirectWorkerArgsApproveSurfaceUsesDedicatedMode(t *testing.T) {
	args := directWorkerArgs(actionApprove)
	if len(args) != 2 || args[0] != "--approve-surface" || args[1] != "current-diff" {
		t.Fatalf("approve-surface args = %#v", args)
	}
}

func TestApproveSurfaceUsageRequiresExactScope(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"approve-surface"}, &stdout, &stderr); code == 0 {
		t.Fatal("scope省略がexit 0で受理されました")
	}
	if !strings.Contains(stderr.String(), "usage: glm-parent-action approve-surface --accepted-scope current-diff") {
		t.Fatalf("usage出力がありません: %q", stderr.String())
	}
	if code := Run([]string{"approve-surface", "--accepted-scope", "other"}, &stdout, &stderr); code == 0 {
		t.Fatal("scope otherがexit 0で受理されました")
	}
}

func TestApproveSurfaceValidInvocationPassesUsageValidation(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Run([]string{"approve-surface", "--accepted-scope", "current-diff"}, &stdout, &stderr)
	if code == 0 {
		t.Fatal("glm-worker不在環境でexit 0にはならない")
	}
	if strings.Contains(stderr.String(), "usage:") {
		t.Fatalf("正規のapprove-surface引数がusage error扱いされました: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "glm-worker executable not found") {
		t.Fatalf("usage検証通過後のworker解決まで到達していません: %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("失敗時にstdoutへ出力がありました: %q", stdout.String())
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

func TestExecuteStartMilestonesPropagatesIdentityEnvAndMode(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	payload := []byte(`{"request":"現在のACTIVE taskを実行してください。","milestones":[{"id":"a","scope":"a","acceptance":"a"},{"id":"b","scope":"b","acceptance":"b"}]}`)
	token := writePreparedPayload(t, cfg.RepoRoot, actionStartMilestones, payload)
	marker := writeParentActionWorkerStubWithCheck(t, cfg, `test "$1" = "--execution-milestones-stdin"`)
	if err := execute(cfg, []string{actionStartMilestones, token}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked")
	}
}

func TestExecuteStartMilestonesRejectsPayloadHashMismatch(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	prepared, err := parentaction.Prepare(cfg.RepoRoot, actionStartMilestones)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline < 0 {
		t.Fatal("prepared payload header missing")
	}
	content := append(append([]byte(nil), raw[:newline+1]...), []byte("tampered")...)
	if err := os.WriteFile(prepared.Path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := execute(cfg, []string{actionStartMilestones, prepared.Token}, &stdout, &stderr); err == nil {
		t.Fatal("tampered payload was accepted")
	}
}

func TestRotationMilestoneStartArgsPropagatesClaim(t *testing.T) {
	claimID := "74c3926e-759d-43ab-b7fa-2aa63fa3b4aa"
	args, env, err := rotationMilestoneStartArgs([]string{actionStartMilestones, "token", "--rotation-claim", claimID}, []string{"A=B"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 2 || args[0] != actionStartMilestones || args[1] != "token" {
		t.Fatalf("args = %#v", args)
	}
	if len(env) != 2 || env[0] != "A=B" || env[1] != state.SessionRotationClaimIDEnv+"="+claimID {
		t.Fatalf("env = %#v", env)
	}
}

func TestRotationMilestoneStartArgsRejectsInvalidClaim(t *testing.T) {
	if _, _, err := rotationMilestoneStartArgs([]string{actionStartMilestones, "token", "--rotation-claim", "bad"}, nil); err == nil {
		t.Fatal("invalid rotation claim was accepted")
	}
}

func TestExecuteRotationActionRequiresCurrentThreadIdentity(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	var stdout bytes.Buffer
	if err := execute(cfg, []string{"rotation-claim", "74c3926e-759d-43ab-b7fa-2aa63fa3b4aa"}, &stdout, io.Discard); err == nil {
		t.Fatal("rotation action without current thread identity was accepted")
	}
}

func TestExecuteRotationClaimRejectsForeignThread(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SetParentCodexIdentity(codexIdentityTestThreadID, codexIdentityTestThreadID, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", "01a0463c-d477-7410-9efd-cb34ff2e0b0f")
	t.Setenv("CODEX_SESSION_ID", "01a0463c-d477-7410-9efd-cb34ff2e0b0f")
	var stdout bytes.Buffer
	if err := execute(cfg, []string{"rotation-claim", "74c3926e-759d-43ab-b7fa-2aa63fa3b4aa"}, &stdout, io.Discard); err == nil {
		t.Fatal("foreign thread rotation claim was accepted")
	}
}

func TestExecuteRotationClaimRoundTrip(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	if err := st.SetParentCodexIdentity(codexIdentityTestThreadID, codexIdentityTestThreadID, nil); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	directive, err := st.SaveSessionRotationDirective(codexIdentityTestThreadID, state.SessionRotationTriggerRotate, 0.88, state.SessionRotationThreshold, 3, 9)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := execute(cfg, []string{"rotation-claim", directive.DirectiveID}, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var claim state.SessionRotationClaim
	if err := json.Unmarshal(stdout.Bytes(), &struct {
		Status string `json:"status"`
		*state.SessionRotationClaim
	}{SessionRotationClaim: &claim}); err != nil {
		t.Fatal(err)
	}
	if claim.ClaimID == "" || claim.DirectiveID != directive.DirectiveID {
		t.Fatalf("claim = %#v", claim)
	}
}

func writePreparedPayload(t *testing.T, repoRoot, action string, payload []byte) string {
	t.Helper()
	prepared, err := parentaction.Prepare(repoRoot, action)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	newline := bytes.IndexByte(raw, '\n')
	if newline < 0 {
		t.Fatal("prepared payload header missing")
	}
	content := append(append([]byte(nil), raw[:newline+1]...), payload...)
	if err := os.WriteFile(prepared.Path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return prepared.Token
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
		StateBase: filepath.Join(t.TempDir(), "sessions"),
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

func TestEvidenceActionPassesAbsoluteManifestPathToWorker(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	workDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(workDir, "evidence-manifest.json"), []byte(`{"version":1,"reason":"absolute path","status":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})
	expectedManifestPath, err := filepath.Abs("evidence-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	marker := writeParentActionWorkerStubWithCheck(t, cfg,
		`test "$1" = "--evidence" && test "$2" = "`+expectedManifestPath+`"`)

	if err := execute(cfg, []string{"evidence", "evidence-manifest.json"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("stub worker was not invoked with the absolute manifest path")
	}
}
