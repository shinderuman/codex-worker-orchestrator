//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestStatusRawJSONLockProbeUnknownIsNull(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lock", st.LockPath()); err != nil {
		t.Fatal(err)
	}
	if probe := ProbeRepoLock(st.LockPath()); probe.State != LockUnknown {
		t.Skipf("この環境ではprobe不能状態を作れません: %s", probe.State)
	}

	decoded := statusRawJSON(t, cfg)
	assertNullJSONValue(t, "repository_lock", requireJSONKey(t, decoded, "repository_lock"))
	assertNullJSONValue(t, "lock_pid", requireJSONKey(t, decoded, "lock_pid"))
	assertNullJSONValue(t, "task_status", requireJSONKey(t, decoded, "task_status"))
	assertNullJSONValue(t, "task_liveness", requireJSONKey(t, decoded, "task_liveness"))
	assertNoPresentationSentinel(t, decoded, "repository_lock", "lock_pid", "task_status", "task_liveness")
}

func TestWatchProcessNonENOENTOutputsStructuredErrorAndNonZero(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go commandがないため実binary testをskipします: %v", err)
	}
	binary, err := buildMultiRepoWorkerBinary(t)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	home := filepath.Join(root, "glm-home")
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(canonicalRepoPath(t, repo)))
	stateDir := filepath.Join(home, "sessions", hex.EncodeToString(digest[:]))
	if err := os.Mkdir(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	if err := os.WriteFile(filepath.Join(stateDir, "task.id"), []byte(taskID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "events"), []byte("not a directory\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "--watch")
	command.Dir = repo
	command.Env = append(os.Environ(), "GLM_WORKER_HOME="+home)
	stdout := &strings.Builder{}
	stderr := &strings.Builder{}
	command.Stdout = stdout
	command.Stderr = stderr
	runErr := command.Run()
	if runErr == nil {
		t.Fatalf("--watchがexit 0で成功しました: stdout=%s stderr=%s", stdout, stderr)
	}
	exitErr := &exec.ExitError{}
	ok := errors.As(runErr, &exitErr)
	if !ok || exitErr.ExitCode() <= 0 {
		t.Fatalf("non-zero exitを期待しました: %v", runErr)
	}
	if stdout.String() != "" {
		t.Fatalf("失敗時のstdoutは空のまま: %q", stdout)
	}
	var envelope decodeProcessError
	if err := json.Unmarshal([]byte(strings.TrimSpace(stderr.String())), &envelope); err != nil {
		t.Fatalf("stderrがprocess error JSONではありません: %v: %q", err, stderr)
	}
	if envelope.Error.Kind != "internal" {
		t.Fatalf("process error kind = %q want internal: %s", envelope.Error.Kind, stderr)
	}
	if envelope.Error.Message == "" {
		t.Fatalf("process error messageが空です: %s", stderr)
	}
}
