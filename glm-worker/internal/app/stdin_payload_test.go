package app

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func needsSolReviewPacketApp() string {
	return appPacketBody(packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             "review",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "i",
		ResidualRisk:        "r",
		Targets:             []string{"glm-worker/internal/app/stdin_payload_test.go:needsSolReviewPacketApp"},
		SolQuestion:         "q",
	})
}

func stdinTestPayload() string {
	return "1. 責務/API\n" +
		"- one-shotの`Search(context.Context, repoRoot, query, Options) (Report, error)`を主APIとする。\n" +
		"- `git ls-files -s -z` + `git ls-files --others --exclude-standard -z`でworktree現物を列挙する。\n" +
		"- 展開されてはいけない文字列: $HOME $(id -u) `pwd` \"double\" 'single'\n" +
		"- 日本語・タブ\t・記号<>|&;をそのまま保持する。"
}

func stdinPayloadSHA(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func snapshotStateFiles(t *testing.T, cfg config.AppConfig) map[string]string {
	t.Helper()
	root := filepath.Join(cfg.StateBase, cfg.RepoHash)
	snapshot := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		sum := sha256.Sum256(data)
		snapshot[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func promptSection(t *testing.T, prompt string, start string, end string) string {
	t.Helper()
	i := strings.Index(prompt, start)
	if i < 0 {
		t.Fatalf("promptに%qがありません: %q", start, prompt)
	}
	section := prompt[i+len(start):]
	j := strings.Index(section, end)
	if j < 0 {
		t.Fatalf("promptに%qがありません: %q", end, prompt)
	}
	return section[:j]
}

func prepareWaitingDecisionState(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	bindCanonicalAppTask(t, cfg, st)
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationInactiveValue); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	return st
}

func prepareWaitingSolReviewState(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	bindCanonicalAppTask(t, cfg, st)
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationInactiveValue); err != nil {
		t.Fatal(err)
	}
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-review", "review"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	return st
}

func stagedSingleDecision(value string) string {
	return "EXECUTION_UNIT: single\nMILESTONES_JSON: {\"milestones\":[]}\nDECISION:\n" + value
}

func writeStagedInput(t *testing.T, cfg config.AppConfig, action, payload string) parentaction.Prepared {
	t.Helper()
	stage, err := parentaction.Prepare(cfg.RepoRoot, action)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(stage.Path)
	if err != nil {
		t.Fatal(err)
	}
	header, _, ok := strings.Cut(string(raw), "\n")
	if !ok {
		t.Fatal("missing staged token header")
	}
	if err := os.WriteFile(stage.Path, []byte(header+"\n"+payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return stage
}

func runStagedInput(t *testing.T, cfg config.AppConfig, r *fakeRunner, action, token, digest string) error {
	t.Helper()
	return runEntry(
		[]string{parentaction.StagedWorkerInput, action, token, "--sha256", digest},
		func() (config.AppConfig, error) { return cfg, nil },
		r.factory(),
		strings.NewReader(""),
		io.Discard,
		io.Discard,
	)
}

func assertFailClosedStateUnchanged(t *testing.T, cfg config.AppConfig, before map[string]string, r *fakeRunner, wantStatus state.TaskStatus) {
	t.Helper()
	if len(r.prompts) != 0 {
		t.Fatal("fail closed時にrunnerを起動しています")
	}
	after := snapshotStateFiles(t, cfg)
	if len(after) != len(before) {
		t.Fatalf("fail closed後にstate file構成が変化しました: before=%v after=%v", before, after)
	}
	for name, digest := range before {
		if after[name] != digest {
			t.Fatalf("fail closed後にstate file %s が変化しました", name)
		}
	}
	st := state.AttachStateStore(cfg)
	if st.TaskStatus() != wantStatus {
		t.Fatalf("status = %q, want %q", st.TaskStatus(), wantStatus)
	}
}

func TestRunStagedDecisionPreservesPayloadBytes(t *testing.T) {
	cfg := newHarnessAppConfig(t)
	startStdinTaskAtDecision(t, cfg)
	payload := stdinTestPayload()
	r := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("decision applied")},
		{structured: needsSolReviewPacketApp()},
	}}

	framed := stagedSingleDecision(payload)
	stage := writeStagedInput(t, cfg, "decision", framed)
	if err := runStagedInput(t, cfg, r, "decision", stage.Token, stdinPayloadSHA(framed)); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(stage.Path); !os.IsNotExist(err) {
		t.Fatalf("valid staged decision was not consumed exactly once: %v", err)
	}
	st := state.AttachStateStore(cfg)
	persisted, err := os.ReadFile(st.Path("last-decision"))
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) != payload+"\n" {
		t.Fatalf("last-decisionが全byte保存されていません: %q", persisted)
	}
	decision := promptSection(t, r.prompts[0], "\nSOL_DECISION:\n", "\n\nACTIVE_TASK_FILE:")
	if decision != payload {
		t.Fatalf("worker promptのdecision部分が全byte保存されていません: %q", decision)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRunStagedDecisionFailsClosedWithoutTokenPayload(t *testing.T) {
	t.Parallel()
	cfg := newCanonicalAppConfig(t)
	prepareWaitingDecisionState(t, cfg)
	payload := stdinTestPayload()
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	before := snapshotStateFiles(t, cfg)

	framed := stagedSingleDecision(payload)
	stage := writeStagedInput(t, cfg, "decision", framed)
	if err := os.Remove(stage.Path); err != nil {
		t.Fatal(err)
	}
	err := runStagedInput(t, cfg, r, "decision", stage.Token, stdinPayloadSHA(framed))
	if !os.IsNotExist(err) && (err == nil || !strings.Contains(err.Error(), "parent action staging file unavailable")) {
		t.Fatalf("missing staged payload must fail closed: %v", err)
	}
	assertFailClosedStateUnchanged(t, cfg, before, r, state.TaskStatusWaitingDecision)
	st := state.AttachStateStore(cfg)
	if !st.Exists("pending-decision") || st.Exists("last-decision") {
		t.Fatalf("waiting decision状態が変わっています: pending=%t last-decision=%t", st.Exists("pending-decision"), st.Exists("last-decision"))
	}
}

func TestRunStagedDecisionFailsClosedOnSHAMismatch(t *testing.T) {
	t.Parallel()
	cfg := newCanonicalAppConfig(t)
	prepareWaitingDecisionState(t, cfg)
	payload := stdinTestPayload()
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	before := snapshotStateFiles(t, cfg)

	framed := stagedSingleDecision(payload)
	stage := writeStagedInput(t, cfg, "decision", framed)
	err := runStagedInput(t, cfg, r, "decision", stage.Token, stdinPayloadSHA(framed+"corrupted"))
	if err == nil || !strings.Contains(err.Error(), "payload changed after validation") {
		t.Fatalf("sha256不一致をfail closedする必要があります: %v", err)
	}
	if _, err := os.Stat(stage.Path); err != nil {
		t.Fatalf("failed digest check consumed authorization: %v", err)
	}
	assertFailClosedStateUnchanged(t, cfg, before, r, state.TaskStatusWaitingDecision)
}

func TestRunStagedFixPreservesPayloadBytes(t *testing.T) {
	cfg := newHarnessAppConfig(t)
	startStdinTaskAtReview(t, cfg)
	payload := stdinTestPayload()
	r := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("fix applied")},
		{structured: needsSolReviewPacketApp()},
	}}

	stage := writeStagedInput(t, cfg, "fix", payload)
	if err := runStagedInput(t, cfg, r, "fix", stage.Token, stdinPayloadSHA(payload)); err != nil {
		t.Fatal(err)
	}

	feedback := promptSection(t, r.prompts[0], "\nREVIEW_FEEDBACK:\n", "\n\nACTIVE_TASK_FILE:")
	if feedback != payload {
		t.Fatalf("worker promptのfix指示部分が全byte保存されていません: %q", feedback)
	}
	st := state.AttachStateStore(cfg)
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q", st.TaskStatus())
	}
}

func TestRunStagedFixFailsClosedOnWrongAction(t *testing.T) {
	t.Parallel()
	cfg := newCanonicalAppConfig(t)
	prepareWaitingSolReviewState(t, cfg)
	payload := stdinTestPayload()
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	before := snapshotStateFiles(t, cfg)

	stage := writeStagedInput(t, cfg, "decision", stagedSingleDecision(payload))
	err := runStagedInput(t, cfg, r, "fix", stage.Token, stdinPayloadSHA(payload))
	if err == nil || !strings.Contains(err.Error(), "parent action staging file unavailable") {
		t.Fatalf("a token for a different action must fail closed: %v", err)
	}
	assertFailClosedStateUnchanged(t, cfg, before, r, state.TaskStatusWaitingSolReview)
	if st := state.AttachStateStore(cfg); !st.Exists("last-review") {
		t.Fatal("last-reviewが失われました")
	}
}

func TestRunStagedDecisionPreservesNULBytes(t *testing.T) {
	cfg := newHarnessAppConfig(t)
	startStdinTaskAtDecision(t, cfg)
	payload := "NUL先頭\x00中間\x00末尾\n2行目 `backtick` $HOME \"double\" 'single' 日本語\x00"
	r := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("decision applied")},
		{structured: needsSolReviewPacketApp()},
	}}

	framed := stagedSingleDecision(payload)
	stage := writeStagedInput(t, cfg, "decision", framed)
	if err := runStagedInput(t, cfg, r, "decision", stage.Token, stdinPayloadSHA(framed)); err != nil {
		t.Fatal(err)
	}

	decision := promptSection(t, r.prompts[0], "\nSOL_DECISION:\n", "\n\nACTIVE_TASK_FILE:")
	if decision != payload {
		t.Fatalf("NUL混在payloadが全byte保存されていません: %q", decision)
	}
}

func TestReadStdinPayloadReturnsWithoutEOF(t *testing.T) {
	t.Parallel()
	payload := stdinTestPayload()
	reader, writer := io.Pipe()
	go func() {
		_, _ = writer.Write([]byte(payload))
	}()

	got, err := readStdinPayload(reader, int64(len(payload)), stdinPayloadSHA(payload))
	if err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("payload = %q", got)
	}
}

func TestReadStdinPayloadAcceptsUppercaseSHA(t *testing.T) {
	t.Parallel()
	payload := stdinTestPayload()
	upper := strings.ToUpper(stdinPayloadSHA(payload))

	got, err := readStdinPayload(strings.NewReader(payload), int64(len(payload)), upper)
	if err != nil {
		t.Fatal(err)
	}
	if got != payload {
		t.Fatalf("payload = %q", got)
	}
}

func TestReadStdinPayloadRejectsInsufficientBytes(t *testing.T) {
	t.Parallel()
	payload := stdinTestPayload()

	_, err := readStdinPayload(strings.NewReader(payload[:10]), int64(len(payload)), "")
	if err == nil || !strings.Contains(err.Error(), "stdin payload read failed after 10 of") {
		t.Fatalf("不足byteをerrorにする必要があります: %v", err)
	}
}

func startStdinTaskAtDecision(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	scripted := &fakeRunner{steps: []fakeStep{{structured: needsSolDecisionPacketApp()}}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, scripted.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func startStdinTaskAtReview(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	scripted := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}, {structured: needsSolReviewPacketApp()}}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, scripted.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}
