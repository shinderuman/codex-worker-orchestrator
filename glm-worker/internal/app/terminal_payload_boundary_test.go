package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	terminalPayloadHelperEnv     = "GLM_WORKER_TERMINAL_PAYLOAD_HELPER"
	terminalPayloadRequestEnv    = "GLM_WORKER_TERMINAL_PAYLOAD_REQUEST"
	terminalPayloadMarker        = "GLMTERM DELAYED MARKER PAYLOAD"
	terminalPayloadPacketHead    = `"status":"NEEDS_SOL_DECISION"`
	terminalPayloadCapturePrefix = "GLM_TERMINAL_CAPTURED"
)

func TestTerminalPayloadBoundarySingleRender(t *testing.T) {
	if os.Getenv(terminalPayloadHelperEnv) == "1" {
		terminalPayloadHelperMain()
		return
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh command not found: %v", err)
	}

	terminalPayloadDelayedMarkerSingleRender(t)
	terminalPayloadLegacyDoubleRenderDetected(t)
	terminalPayloadRealWorkerTerminalResult(t)
}

type terminalPayloadOrchestration struct {
	captured  string
	liveEmit  []string
	cellValue string
	store     map[string]string
}

func newTerminalPayloadOrchestration() *terminalPayloadOrchestration {
	return &terminalPayloadOrchestration{store: make(map[string]string)}
}

var terminalPayloadDroppedEnvKeys = []string{
	"GLM_WORKER_HOME",
	"GLM_WORKER_PROMPT_DIR",
	"GLM_WORKER_CLAUDE_BIN",
	"GLM_WORKER_TELEMETRY_CONTENT",
	"CLAUDE_CONFIG_DIR",
}

func terminalPayloadCellEnv(extraEnv []string) []string {
	drop := make(map[string]bool, len(terminalPayloadDroppedEnvKeys))
	for _, key := range terminalPayloadDroppedEnvKeys {
		drop[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(extraEnv))
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if ok && drop[key] {
			continue
		}
		env = append(env, item)
	}
	return append(env, extraEnv...)
}

func (o *terminalPayloadOrchestration) runLongCell(
	t *testing.T,
	ctx context.Context,
	dir string,
	name string,
	args []string,
	extraEnv []string,
) {
	t.Helper()

	cell := exec.CommandContext(ctx, name, args...)
	cell.Dir = dir
	cell.Env = terminalPayloadCellEnv(extraEnv)
	captured := &strings.Builder{}
	cell.Stdout = captured
	cell.Stderr = captured
	if err := cell.Start(); err != nil {
		t.Fatalf("長時間cellの起動に失敗: %v", err)
	}

	if err := cell.Wait(); err != nil {
		t.Fatalf("長時間cellが非zero終了: %v captured=%q", err, captured.String())
	}
	o.captured = captured.String()
}

func (o *terminalPayloadOrchestration) emitLive(text string) {
	o.liveEmit = append(o.liveEmit, text)
}

func (o *terminalPayloadOrchestration) captureTerminal(key string) {
	o.store[key] = o.captured
	o.cellValue = terminalPayloadCapturePrefix + " " + key
}

func (o *terminalPayloadOrchestration) passThroughCellValue() {
	o.cellValue = o.captured
}

func (o *terminalPayloadOrchestration) syncLoad(key string) string {
	return o.store[key]
}

func (o *terminalPayloadOrchestration) renderedPayloads(syncText string) []string {
	rendered := append([]string(nil), o.liveEmit...)
	return append(rendered, o.cellValue, o.cellValue, syncText)
}

func (o *terminalPayloadOrchestration) renderedCount(syncText string, payload string) int {
	count := 0
	for _, rendered := range o.renderedPayloads(syncText) {
		count += strings.Count(rendered, payload)
	}
	return count
}

func terminalPayloadDelayedMarkerSingleRender(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const taskKey = "terminal-payload-eval-delayed"
	orchestration := newTerminalPayloadOrchestration()
	started := time.Now()
	orchestration.runLongCell(t, ctx, "", "sh", []string{"-c", `sleep 1; echo "` + terminalPayloadMarker + `"`}, nil)
	if elapsed := time.Since(started); elapsed < 900*time.Millisecond {
		t.Fatalf("delayed markerがcell終端より前に出力された: elapsed=%s", elapsed)
	}
	orchestration.captureTerminal(taskKey)

	if got := strings.Count(orchestration.store[taskKey], terminalPayloadMarker); got != 1 {
		t.Fatalf("内部storeへmarkerが1回蓄積されていない: got=%d store=%q", got, orchestration.store[taskKey])
	}
	if strings.Contains(orchestration.cellValue, terminalPayloadMarker) {
		t.Fatalf("raw markerがcell返り値へ流れている: %q", orchestration.cellValue)
	}
	if !strings.Contains(orchestration.cellValue, terminalPayloadCapturePrefix) {
		t.Fatalf("cell返り値がcaptured markerになっていない: %q", orchestration.cellValue)
	}
	if got := orchestration.renderedCount(orchestration.syncLoad(taskKey), terminalPayloadMarker); got != 1 {
		t.Fatalf("契約手順の親可視payload出現回数が1でない: got=%d cellValue=%q", got, orchestration.cellValue)
	}
}

func terminalPayloadLegacyDoubleRenderDetected(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	orchestration := newTerminalPayloadOrchestration()
	orchestration.runLongCell(t, ctx, "", "sh", []string{"-c", `sleep 1; echo "` + terminalPayloadMarker + `"`}, nil)
	orchestration.passThroughCellValue()

	if !strings.Contains(orchestration.cellValue, terminalPayloadMarker) {
		t.Fatalf("旧形のcell返り値にmarkerがない: %q", orchestration.cellValue)
	}
	if got := orchestration.renderedCount("", terminalPayloadMarker); got != 2 {
		t.Fatalf("旧形の二面表示が境界で検出できていない: got=%d", got)
	}

	orchestration.emitLive(orchestration.captured)
	if got := orchestration.renderedCount("", terminalPayloadMarker); got != 3 {
		t.Fatalf("旧形の即時描画流出を含むaggregateが境界で検出できていない: got=%d", got)
	}
}

func terminalPayloadRealWorkerTerminalResult(t *testing.T) {
	t.Helper()

	home := t.TempDir()
	repo := t.TempDir()
	if err := exec.Command("git", "-C", repo, "init", "-q").Run(); err == nil {
		commitEnv := append(os.Environ(),
			"GIT_AUTHOR_NAME=glm-worker-test",
			"GIT_AUTHOR_EMAIL=glm-worker-test@example.com",
			"GIT_COMMITTER_NAME=glm-worker-test",
			"GIT_COMMITTER_EMAIL=glm-worker-test@example.com",
		)
		seed := exec.Command("sh", "-c", `echo seed > tracked.txt && git add tracked.txt && git commit -qm seed`)
		seed.Dir = repo
		seed.Env = commitEnv
		_ = seed.Run()
	}

	prompts := filepath.Join(home, "prompts")
	if err := os.MkdirAll(prompts, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompts, "WORKER.md"), []byte("worker prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	claudeBin := filepath.Join(home, "fake-claude")
	if err := os.WriteFile(claudeBin, []byte(strings.Join([]string{
		"#!/bin/sh",
		"printf '%s\\n' '" + terminalPayloadFakeResultJSON() + "'",
		"",
	}, "\n")), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "claude-config"), 0o700); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	const taskKey = "terminal-payload-eval-worker"
	orchestration := newTerminalPayloadOrchestration()
	extraEnv := []string{
		terminalPayloadHelperEnv + "=1",
		terminalPayloadRequestEnv + "=terminal payload単一描画境界の検証用依頼",
		"GLM_WORKER_HOME=" + home,
		"GLM_WORKER_PROMPT_DIR=" + prompts,
		"GLM_WORKER_CLAUDE_BIN=" + claudeBin,
		"GLM_WORKER_TELEMETRY_CONTENT=false",
		"CLAUDE_CONFIG_DIR=" + filepath.Join(home, "claude-config"),
	}
	orchestration.runLongCell(t, ctx, repo, os.Args[0], []string{"-test.run=TestTerminalPayloadBoundarySingleRender"}, extraEnv)
	orchestration.captureTerminal(taskKey)

	if got := strings.Count(orchestration.store[taskKey], terminalPayloadPacketHead); got != 1 {
		t.Fatalf("実glm-worker terminal resultが内部storeへ1回出力されていない: got=%d store=%q", got, orchestration.store[taskKey])
	}
	if strings.Contains(orchestration.cellValue, terminalPayloadPacketHead) {
		t.Fatalf("実glm-worker terminal resultがcell返り値へ流れている: %q", orchestration.cellValue)
	}
	if !strings.Contains(orchestration.cellValue, terminalPayloadCapturePrefix) {
		t.Fatalf("cell返り値がcaptured markerになっていない: %q", orchestration.cellValue)
	}
	if got := orchestration.renderedCount(orchestration.syncLoad(taskKey), terminalPayloadPacketHead); got != 1 {
		t.Fatalf("実terminal resultの親可視payload出現回数が1でない: got=%d cellValue=%q", got, orchestration.cellValue)
	}
}

func terminalPayloadFakeResultJSON() string {
	return `{"type":"result","subtype":"success","is_error":false,"result":"STATUS: NEEDS_SOL_DECISION","structured_output":{"status":"NEEDS_SOL_DECISION","risk":"HIGH","decision":"terminal payload単一描画境界の検証として親が選択する判断","evidence":"fake claude binaryの固定result event","options":"契約手順で単一描画 / 旧形の二面表示","recommendation":"契約手順","test_obligations":"background exec→wait→同期取得境界の検証維持","targets":["none"],"artifacts":[]},"duration_ms":3,"duration_api_ms":3,"num_turns":1,"usage":{"input_tokens":1,"output_tokens":1}}`
}

func terminalPayloadHelperMain() {
	if err := Run([]string{os.Getenv(terminalPayloadRequestEnv)}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	os.Exit(0)
}
