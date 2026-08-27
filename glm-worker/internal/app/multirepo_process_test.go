//go:build unix

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type multiRepoEnv struct {
	binary    string
	home      string
	promptDir string
	claudeCfg string
	override  string
	repoA     string
	repoB     string
	stubA     string
	stubB     string
}

type multiRepoResult struct {
	code   int
	stdout string
	stderr string
}

type multiRepoHolder struct {
	stdout *strings.Builder
	stderr *strings.Builder
	done   chan error
}

const (
	multiRepoPollInterval = 50 * time.Millisecond
	multiRepoRunTimeout   = 3 * time.Minute
	multiRepoWaitTimeout  = 30 * time.Second
)

const multiRepoStubClaude = `#!/bin/sh
dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
mode=$(cat "$dir/mode" 2>/dev/null || echo none)
case "$mode" in
hold)
	waits=0
	while [ ! -f "$dir/release" ] && [ "$waits" -lt 1200 ]; do
		sleep 0.1
		waits=$((waits + 1))
	done
	echo "stub claude hold released" >&2
	exit 1
	;;
ratelimit)
	echo "API Error: Request rejected (429) [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-08-23 12:00:00]" >&2
	exit 1
	;;
hold-with-tool)
	(
		trap '' TERM
		while :; do sleep 0.2; done
	) &
	echo $! > "$dir/tool.pid"
	waits=0
	while [ ! -f "$dir/release" ] && [ "$waits" -lt 3000 ]; do
		sleep 0.1
		waits=$((waits + 1))
	done
	exit 1
	;;
dirty-hold)
	# 停止保持基準の観測対象として未commit作業を残してからholdする。
	printf 'stub uncommitted work\n' > uncommitted.txt
	waits=0
	while [ ! -f "$dir/release" ] && [ "$waits" -lt 3000 ]; do
		sleep 0.1
		waits=$((waits + 1))
	done
	exit 1
	;;
reviewer-hold)
	role=worker
	for arg in "$@"; do
		if [ "$arg" = "--disallowedTools" ]; then role=reviewer; fi
	done
	if [ "$role" = reviewer ]; then
		waits=0
		while [ ! -f "$dir/release" ] && [ "$waits" -lt 3000 ]; do
			sleep 0.1
			waits=$((waits + 1))
		done
		exit 1
	fi
	printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"worker implemented","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"stub implementation summary","requirement_coverage":"stub coverage","tests":"stub tests","unverified":"none"},"usage":{"input_tokens":5,"output_tokens":5},"duration_ms":5}'
	exit 0
	;;
success)
	role=worker
	for arg in "$@"; do
		if [ "$arg" = "--disallowedTools" ]; then role=reviewer; fi
	done
	if [ "$role" = reviewer ]; then
		printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"reviewer pass","structured_output":{"status":"PASS","risk":"LOW","summary":"stub review summary","requirement_coverage":"stub coverage","invariants":"stub invariants","test_evidence":"stub evidence","issues":"none","residual_risk":"none","targets":["none"]},"usage":{"input_tokens":3,"output_tokens":3},"duration_ms":3}'
	else
		printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"worker implemented","structured_output":{"status":"IMPLEMENTED","risk":"LOW","summary":"stub implementation summary","requirement_coverage":"stub coverage","tests":"stub tests","unverified":"none"},"usage":{"input_tokens":5,"output_tokens":5},"duration_ms":5}'
	fi
	exit 0
	;;
esac
echo "stub claude unknown mode: $mode" >&2
exit 1
`

func TestMultiRepositoryProcessIsolation(t *testing.T) {
	env := newMultiRepoEnv(t)
	env.setStubMode(t, env.stubA, "hold")
	env.setStubMode(t, env.stubB, "success")

	holdCtx, cancelHold := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	defer cancelHold()
	holder := env.start(t, holdCtx, env.repoA, "repo A first task marker MRISOA1")

	stateA := env.waitStateDir(t, env.repoA, holder)
	env.waitHeldWithWorkerSession(t, stateA)
	taskB := env.runTaskToPass(t, env.repoB, "repo B parallel task marker MRISOB")
	stateB := env.waitStateDir(t, env.repoB, nil)

	taskA1 := readStateFile(t, stateA, "task.id")
	assertRepoLockSemantics(t, env, stateA, stateB, taskA1, taskB)
	assertSameRepoSecondProcessDenied(t, env, env.repoA)
	assertNoCrossContamination(t, stateA, stateB,
		[]string{taskA1, readStateFile(t, stateA, "worker.id"), "MRISOA1"},
		[]string{taskB, readStateFile(t, stateB, "worker.id"), readStateFile(t, stateB, "reviewer.id"), "MRISOB"},
	)
	snapshotB := snapshotStateDir(t, stateB)

	env.releaseHold(t)
	holder.waitFailure(t)
	if probe := ProbeRepoLock(filepath.Join(stateA, "lock")); probe.State != LockFree {
		t.Fatalf("repo A process終了後にlockが解放されていません: %s", probe.State)
	}

	env.setStubMode(t, env.stubA, "ratelimit")
	rateLimited := env.run(t, env.repoA, "repo A second task after recovery marker MRISOA2")
	if rateLimited.code != 1 || !strings.Contains(rateLimited.stderr, `"kind":"rate_limited"`) {
		t.Fatalf("rate-limit停止になりません: code=%d stderr=%s", rateLimited.code, rateLimited.stderr)
	}
	statusA := env.status(t, env.repoA)
	rateLimitedStatus, ok := statusJSONField(t, statusA, "rate_limited").(map[string]any)
	if !ok || rateLimitedStatus["limited"] != true {
		t.Fatalf("repo Aがrate-limited stateになっていません: %s", statusA)
	}
	checkpointA := parseStateJSON(t, stateA, "resume-state.json")
	if checkpointA["rate_limited"] != true || !strings.Contains(fmt.Sprint(checkpointA["request"]), "MRISOA2") {
		t.Fatalf("repo Aのrate-limit checkpointが当該taskの停止状態を保持していません: %v", checkpointA["request"])
	}
	if _, err := os.Stat(filepath.Join(stateB, "resume-state.json")); !os.IsNotExist(err) {
		t.Fatalf("完結済みrepo Bへcheckpointが残っています: %v", err)
	}
	assertStateDirUnchanged(t, stateB, snapshotB)

	env.setStubMode(t, env.stubA, "success")
	resumed := env.run(t, env.repoA, "--resume")
	if resumed.code != 0 || !strings.Contains(resumed.stdout, `"status":"PASS"`) {
		t.Fatalf("rate-limit resumeが完結しません: code=%d stdout=%s stderr=%s", resumed.code, resumed.stdout, resumed.stderr)
	}
	taskA2 := readStateFile(t, stateA, "task.id")
	assertStateDirUnchanged(t, stateB, snapshotB)
	assertRepoLocalObservability(t, stateA, taskA2, "MRISOA2")
	assertRepoLocalObservability(t, stateB, taskB, "MRISOB")

	reset := env.run(t, env.repoA, "--reset")
	if reset.code != 0 || !strings.Contains(reset.stdout, `"status":"reset"`) {
		t.Fatalf("repo Aのresetが失敗しました: code=%d stdout=%s stderr=%s", reset.code, reset.stdout, reset.stderr)
	}
	assertStateDirUnchanged(t, stateB, snapshotB)
	if _, err := os.Stat(filepath.Join(stateA, "task.id")); !os.IsNotExist(err) {
		t.Fatalf("repo A reset後もtask.idが残っています: %v", err)
	}
}

func assertRepoLockSemantics(t *testing.T, env *multiRepoEnv, stateA string, stateB string, taskA1 string, taskB string) {
	t.Helper()
	if stateA == stateB || filepath.Join(stateA, "lock") == filepath.Join(stateB, "lock") {
		t.Fatalf("state dir・lock pathが分離されていません: %s vs %s", stateA, stateB)
	}
	if probe := ProbeRepoLock(filepath.Join(stateA, "lock")); probe.State != LockHeld {
		t.Fatalf("repo Aのlockが保持されていません: %s pid=%s", probe.State, probe.PID)
	}
	if probe := ProbeRepoLock(filepath.Join(stateB, "lock")); probe.State != LockFree {
		t.Fatalf("repo B完了後のlockが解放されていません: %s pid=%s", probe.State, probe.PID)
	}

	statusA := env.status(t, env.repoA)
	statusB := env.status(t, env.repoB)
	for name, want := range map[string]struct {
		status string
		repo   string
		lock   string
		task   string
	}{
		"A": {statusA, env.repoA, "held", taskA1},
		"B": {statusB, env.repoB, "free", taskB},
	} {
		if got := statusJSONField(t, want.status, "repo_root"); got != want.repo {
			t.Fatalf("repo %sのstatusが別repoを指しています: want %s got %v", name, want.repo, got)
		}
		if got := statusJSONField(t, want.status, "repository_lock"); got != want.lock {
			t.Fatalf("repo %sのlock状態が期待と違います: want %s got %v", name, want.lock, got)
		}
		if got := statusJSONField(t, want.status, "task_id"); got != want.task {
			t.Fatalf("repo %sのtask IDが期待と違います: want %s got %v", name, want.task, got)
		}
	}
	if statusJSONField(t, statusA, "worker_session") == statusJSONField(t, statusB, "worker_session") {
		t.Fatalf("worker sessionが両repoで同一です: %v", statusJSONField(t, statusA, "worker_session"))
	}
	if statusJSONField(t, statusB, "reviewer_session") == nil {
		t.Fatalf("repo B完了後にreviewer sessionが記録されていません: %s", statusB)
	}
}

func assertSameRepoSecondProcessDenied(t *testing.T, env *multiRepoEnv, repo string) {
	t.Helper()
	denied := env.run(t, repo, "--reset")
	if denied.code == 0 || !strings.Contains(denied.stderr, "another glm-worker is already running for this repository") {
		t.Fatalf("同一repo 2本目のlock拒否が成立していません: code=%d stderr=%s", denied.code, denied.stderr)
	}
}

func assertNoCrossContamination(t *testing.T, stateA string, stateB string, secretsA []string, secretsB []string) {
	t.Helper()
	for _, secret := range secretsB {
		assertStateDirExcludes(t, stateA, secret, "repo B")
	}
	for _, secret := range secretsA {
		assertStateDirExcludes(t, stateB, secret, "repo A")
	}
}

func assertStateDirExcludes(t *testing.T, stateDir string, secret string, other string) {
	t.Helper()
	for path, content := range snapshotStateDir(t, stateDir) {
		if strings.Contains(content, secret) {
			t.Fatalf("%sのstate file %sへ他repo(%s)の識別子 %q が混入しています", stateDir, path, other, secret)
		}
	}
}

func assertRepoLocalObservability(t *testing.T, stateDir string, taskID string, marker string) {
	t.Helper()
	telemetry := readStateFile(t, stateDir, filepath.Join("telemetry", taskID+".jsonl"))
	if !strings.Contains(telemetry, taskID) || !strings.Contains(telemetry, marker) {
		t.Fatalf("telemetryが当該taskの記録を含みません: task=%s marker=%s", taskID, marker)
	}
	events := readStateFile(t, stateDir, filepath.Join("events", taskID+".jsonl"))
	if !strings.Contains(events, taskID) {
		t.Fatalf("event logが当該taskの記録を含みません: task=%s", taskID)
	}
	for _, line := range append(
		strings.Split(telemetry, "\n"),
		strings.Split(events, "\n")...,
	) {
		if strings.Contains(line, "\"task_id\"") && !strings.Contains(line, "\"task_id\":\""+taskID+"\"") {
			t.Fatalf("state dir %sに他task IDのrecordがあります: %s", stateDir, line)
		}
	}
}

func newMultiRepoEnv(t *testing.T) *multiRepoEnv {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないため実binary testをskipします: %v", err)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git commandがないため実binary testをskipします: %v", err)
	}
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	env := &multiRepoEnv{
		binary:    binary,
		home:      filepath.Join(root, "glm-home"),
		promptDir: filepath.Join(root, "prompts"),
		claudeCfg: filepath.Join(root, "claude-config"),
		override:  filepath.Join(root, "absent-claude-override.json"),
	}
	for _, dir := range []string{env.home, env.promptDir, env.claudeCfg} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, prompt := range []struct{ name, body string }{
		{"WORKER.md", "stub worker system prompt\n"},
		{"REVIEWER.md", "stub reviewer system prompt\n"},
	} {
		if err := os.WriteFile(filepath.Join(env.promptDir, prompt.name), []byte(prompt.body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	env.repoA = canonicalRepoPath(t, newMultiRepoGitRepo(t, filepath.Join(root, "repo-a"), "mrsearchalpha"))
	env.repoB = canonicalRepoPath(t, newMultiRepoGitRepo(t, filepath.Join(root, "repo-b"), "mrsearchbeta"))
	env.stubA = writeMultiRepoStubClaude(t, filepath.Join(root, "stub-a"))
	env.stubB = writeMultiRepoStubClaude(t, filepath.Join(root, "stub-b"))
	return env
}

func canonicalRepoPath(t *testing.T, repo string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func buildMultiRepoWorkerBinary(t *testing.T) (string, error) {
	t.Helper()
	moduleRoot, err := filepath.Abs("../..")
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(moduleRoot, "go.mod")); err != nil {
		return "", fmt.Errorf("module root解決失敗: %w", err)
	}
	binary := filepath.Join(t.TempDir(), "glm-worker")
	build := exec.Command("go", "build", "-o", binary, "./cmd/glm-worker")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build失敗: %w: %s", err, output)
	}
	return binary, nil
}

func newMultiRepoGitRepo(t *testing.T, dir string, marker string) string {
	t.Helper()
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v失敗: %v: %s", args, err, output)
		}
	}
	run("init", "-q")
	run("config", "user.email", "multirepo@example.invalid")
	run("config", "user.name", "multirepo test")
	document := fmt.Sprintf("%s unique corpus\n", marker)
	if err := os.WriteFile(filepath.Join(dir, "corpus.md"), []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "corpus.md")
	run("commit", "-q", "-m", "initial")
	return dir
}

func writeMultiRepoStubClaude(t *testing.T, dir string) string {
	t.Helper()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stub := filepath.Join(dir, "claude")
	if err := os.WriteFile(stub, []byte(multiRepoStubClaude), 0o755); err != nil {
		t.Fatal(err)
	}
	return stub
}

func (*multiRepoEnv) setStubMode(t *testing.T, stub string, mode string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(stub), "mode"), []byte(mode), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *multiRepoEnv) releaseHold(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(filepath.Dir(e.stubA), "release"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (e *multiRepoEnv) childEnv(repo string) []string {
	stub := e.stubA
	if repo == e.repoB {
		stub = e.stubB
	}
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + e.home,
		"TMPDIR=" + e.home,
		"GLM_WORKER_HOME=" + e.home,
		"GLM_WORKER_PROMPT_DIR=" + e.promptDir,
		"GLM_WORKER_CLAUDE_BIN=" + stub,
		"CLAUDE_CONFIG_DIR=" + e.claudeCfg,
		"CODEX_CONFIG_DIR=" + filepath.Join(e.home, "codex"),
		"CODEX_CONFIG_CLAUDE_SETTINGS_OVERRIDE=" + e.override,
	}
}

func (e *multiRepoEnv) start(t *testing.T, ctx context.Context, repo string, args ...string) *multiRepoHolder {
	t.Helper()
	command := exec.CommandContext(ctx, e.binary, args...)
	command.Dir = repo
	command.Env = e.childEnv(repo)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	return &multiRepoHolder{stdout: &stdout, stderr: &stderr, done: done}
}

func (h *multiRepoHolder) waitFailure(t *testing.T) {
	t.Helper()
	select {
	case err := <-h.done:
		if err == nil {
			t.Fatalf("期待に反してglm-workerが成功終了しました: stdout=%s stderr=%s", h.stdout.String(), h.stderr.String())
		}
	case <-time.After(multiRepoWaitTimeout):
		t.Fatal("glm-worker holder processが終了しません")
	}
}

func (e *multiRepoEnv) run(t *testing.T, repo string, args ...string) multiRepoResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), multiRepoRunTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, e.binary, args...)
	command.Dir = repo
	command.Env = e.childEnv(repo)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	code := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("glm-worker実行失敗(%v): %s %s", err, stdout.String(), stderr.String())
	}
	return multiRepoResult{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func (e *multiRepoEnv) runTaskToPass(t *testing.T, repo string, instruction string) string {
	t.Helper()
	result := e.run(t, repo, instruction)
	if result.code != 0 || !strings.Contains(result.stdout, `"status":"PASS"`) {
		t.Fatalf("repo %sのtaskがPASS完結しません: code=%d stdout=%s stderr=%s", repo, result.code, result.stdout, result.stderr)
	}
	return readStateFile(t, e.waitStateDir(t, repo, nil), "task.id")
}

func (e *multiRepoEnv) status(t *testing.T, repo string) string {
	t.Helper()
	result := e.run(t, repo, "--status")
	if result.code != 0 {
		t.Fatalf("--statusが失敗しました: %s %s", result.stdout, result.stderr)
	}
	return result.stdout
}

func (e *multiRepoEnv) waitStateDir(t *testing.T, repo string, holder *multiRepoHolder) string {
	t.Helper()
	canonical, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	sessions := filepath.Join(e.home, "sessions")
	deadline := time.Now().Add(multiRepoWaitTimeout)
	for time.Now().Before(deadline) {
		if stateDir := findStateDirForRepo(sessions, canonical); stateDir != "" {
			return stateDir
		}
		if holder != nil {
			select {
			case waitErr := <-holder.done:
				t.Fatalf("repo %sのstate dir出現前にprocessが終了しました: %v stdout=%s stderr=%s",
					repo, waitErr, holder.stdout.String(), holder.stderr.String())
			default:
			}
		}
		time.Sleep(multiRepoPollInterval)
	}
	t.Fatalf("repo %sのstate dirが現れません: %s", repo, sessions)
	return ""
}

func findStateDirForRepo(sessions string, canonicalRepo string) string {
	entries, err := os.ReadDir(sessions)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		stateDir := filepath.Join(sessions, entry.Name())
		content, err := os.ReadFile(filepath.Join(stateDir, "repo-root"))
		if err == nil && strings.TrimSpace(string(content)) == canonicalRepo {
			return stateDir
		}
	}
	return ""
}

func (*multiRepoEnv) waitHeldWithWorkerSession(t *testing.T, stateDir string) {
	t.Helper()
	deadline := time.Now().Add(multiRepoWaitTimeout)
	for time.Now().Before(deadline) {
		if ProbeRepoLock(filepath.Join(stateDir, "lock")).State == LockHeld &&
			fileExists(filepath.Join(stateDir, "task.id")) &&
			fileExists(filepath.Join(stateDir, "worker.id")) {
			return
		}
		time.Sleep(multiRepoPollInterval)
	}
	t.Fatalf("holderがlock・task・worker sessionを確定しません: %s", stateDir)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readStateFile(t *testing.T, stateDir string, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stateDir, name))
	if err != nil {
		t.Fatalf("state file %s/%sを読めません: %v", stateDir, name, err)
	}
	return strings.TrimSpace(string(data))
}

func parseStateJSON(t *testing.T, stateDir string, name string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(readStateFile(t, stateDir, name)), &value); err != nil {
		t.Fatalf("state file %s/%sをJSONとして読めません: %v", stateDir, name, err)
	}
	return value
}

func snapshotStateDir(t *testing.T, stateDir string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(stateDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(stateDir, path)
		if err != nil {
			return err
		}
		snapshot[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("state dir %sを読めません: %v", stateDir, err)
	}
	return snapshot
}

func assertStateDirUnchanged(t *testing.T, stateDir string, snapshot map[string]string) {
	t.Helper()
	current := snapshotStateDir(t, stateDir)
	delete(current, "lock")
	expected := make(map[string]string, len(snapshot))
	for path, content := range snapshot {
		if path != "lock" {
			expected[path] = content
		}
	}
	if len(current) != len(expected) {
		t.Fatalf("state dir %sのfile構成が変化しました: want %d files got %d files", stateDir, len(expected), len(current))
	}
	for path, content := range expected {
		if current[path] != content {
			t.Fatalf("state dir %sの %s が変化しました", stateDir, path)
		}
	}
}

func statusJSONField(t *testing.T, output string, key string) any {
	t.Helper()
	var status map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &status); err != nil {
		t.Fatalf("--status出力がmachine JSONではありません: %v: %q", err, output)
	}
	value, ok := status[key]
	if !ok {
		t.Fatalf("status出力に%qがありません: %q", key, output)
	}
	return value
}
