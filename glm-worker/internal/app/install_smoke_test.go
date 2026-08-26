package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writeSmokeScript(t *testing.T, repoRoot string, failFlagPath string, countPath string) {
	t.Helper()
	dir := filepath.Join(repoRoot, "tests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "install_smoke.sh")
	body := "#!/bin/sh\n" +
		"count_file='" + countPath + "'\n" +
		"count=$(cat \"$count_file\" 2>/dev/null || printf '0')\n" +
		"count=$((count + 1))\n" +
		"printf '%s\\n' \"$count\" >\"$count_file\"\n" +
		"if [ -f '" + failFlagPath + "' ]; then\n" +
		"  printf '%s\\n' 'install smoke: FAIL'\n" +
		"  exit 1\n" +
		"fi\n" +
		"printf '%s\\n' 'install smoke: PASS'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeClaudeProbeShim(t *testing.T, dir string, name string, jsonSchema bool) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(dir, name)
	help := "usage: claude"
	if jsonSchema {
		help = "usage: claude [--json-schema]"
	}
	body := "#!/bin/sh\nprintf '%s\\n' '" + help + "'\n"
	if err := os.WriteFile(shim, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return shim
}

func newInstallSmokeEnv(t *testing.T) (config.AppConfig, *state.StateStore, string, string) {
	t.Helper()
	repoRoot := t.TempDir()
	if out, err := exec.Command("git", "init", "--quiet", repoRoot).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	failFlagPath := filepath.Join(t.TempDir(), "fail-flag")
	countPath := filepath.Join(t.TempDir(), "invocation-count")
	writeSmokeScript(t, repoRoot, failFlagPath, countPath)
	if err := os.WriteFile(filepath.Join(repoRoot, "install.sh"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, "codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "codex", "AGENTS.md"), []byte("# agents\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shimDir := filepath.Join(t.TempDir(), "bin")
	claudeBin := writeClaudeProbeShim(t, shimDir, "claude", true)
	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  config.RepoHashFor(repoRoot),
		RepoShort: config.RepoHashFor(repoRoot)[:12],
		StateBase: filepath.Join(t.TempDir(), "state"),
		ClaudeBin: claudeBin,
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st, failFlagPath, countPath
}

func smokeInvocationCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func runInstallSmokeForTest(t *testing.T, role string, cfg config.AppConfig, st *state.StateStore) (string, installSmokeOutput, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := runInstallSmoke(role, cfg, st, &stdout)
	raw := stdout.String()
	if strings.Contains(raw, "install smoke:") {
		t.Fatalf("install smoke stdout must not contain shell text: %q", raw)
	}
	var output installSmokeOutput
	if trimmed := strings.TrimSpace(raw); trimmed != "" {
		if jsonErr := json.Unmarshal([]byte(trimmed), &output); jsonErr != nil {
			t.Fatalf("install smoke stdout must be a single JSON object: %v: %q", jsonErr, raw)
		}
	}
	return raw, output, err
}

func TestInstallSmokeRepresentativeLoopSharesPassEvidence(t *testing.T) {
	cfg, st, failFlagPath, countPath := newInstallSmokeEnv(t)

	_, output, err := runInstallSmokeForTest(t, "worker", cfg, st)
	if err != nil || output.Status != "executed" || output.Result != "pass" {
		t.Fatalf("worker acquisition: output=%+v err=%v", output, err)
	}
	if output.Log == nil {
		t.Fatalf("executed pass must reference the captured smoke log")
	}
	workerLog := *output.Log
	assertSmokeLogFile(t, workerLog, "install smoke: PASS")
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("worker invocation count=%d want 1", count)
	}

	_, reviewerOutput, err := runInstallSmokeForTest(t, "reviewer", cfg, st)
	if err != nil || reviewerOutput.Status != "reused" {
		t.Fatalf("reviewer reuse: output=%+v err=%v", reviewerOutput, err)
	}
	if reviewerOutput.Evidence == nil || reviewerOutput.Evidence.Role != "worker" {
		t.Errorf("reviewer reuse must cite worker evidence: %+v", reviewerOutput.Evidence)
	}
	if reviewerOutput.Log != nil {
		t.Errorf("reused output must not reference a smoke log: %q", *reviewerOutput.Log)
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("reviewer must not re-run smoke: count=%d want 1", count)
	}

	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "install.sh"), []byte("#!/bin/sh\n# fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, output, err = runInstallSmokeForTest(t, "fix", cfg, st)
	if err != nil || output.Status != "executed" {
		t.Fatalf("fix reacquisition: output=%+v err=%v", output, err)
	}
	if output.Log == nil || *output.Log == workerLog {
		t.Fatalf("fix reacquisition must reference its own smoke log: %+v", output.Log)
	}
	if count := smokeInvocationCount(t, countPath); count != 2 {
		t.Fatalf("fix invocation count=%d want 2", count)
	}

	if _, output, err = runInstallSmokeForTest(t, "reviewer", cfg, st); err != nil || output.Status != "reused" {
		t.Fatalf("post-fix reviewer reuse: output=%+v err=%v", output, err)
	}

	if err := os.WriteFile(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md"), []byte("# parent metadata only\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSmokeGit(t, cfg.RepoRoot, "add", "-A")
	runSmokeGit(t, cfg.RepoRoot, "-c", "user.name=tester", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "candidate")
	if _, output, err = runInstallSmokeForTest(t, "parent", cfg, st); err != nil || output.Status != "reused" {
		t.Fatalf("parent gate must share pass across commit and parent metadata: output=%+v err=%v", output, err)
	}
	if count := smokeInvocationCount(t, countPath); count != 2 {
		t.Fatalf("representative loop executed %d real runs, want 2", count)
	}
	if err := os.Remove(failFlagPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func runSmokeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func assertSmokeLogFile(t *testing.T, path string, marker string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("smoke log %s must exist: %v", path, err)
	}
	if !strings.Contains(string(data), marker) {
		t.Fatalf("smoke log %s must contain %q: %q", path, marker, string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("smoke log permission=%v want 0600", info.Mode().Perm())
	}
}

func TestInstallSmokeFailureRecordIsNotReused(t *testing.T) {
	cfg, st, failFlagPath, countPath := newInstallSmokeEnv(t)

	if err := os.WriteFile(failFlagPath, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, output, err := runInstallSmokeForTest(t, "worker", cfg, st)
	if err == nil {
		t.Fatalf("failing acquisition must return an error")
	}
	if raw != "" {
		t.Fatalf("failing acquisition must not write machine stdout: %q", raw)
	}
	var smokeFail *InstallSmokeError
	if !errors.As(err, &smokeFail) {
		t.Fatalf("failing acquisition error=%T want *InstallSmokeError", err)
	}
	envelope, envelopeRaw := writeProcessErrorJSON(t, err)
	if envelope.Error.Kind != "install_smoke_failed" {
		t.Fatalf("process error kind=%q want install_smoke_failed: %s", envelope.Error.Kind, envelopeRaw)
	}
	logPath, _ := envelope.Error.Detail["log"].(string)
	if logPath == "" {
		t.Fatalf("process error detail must reference the smoke log: %s", envelopeRaw)
	}
	assertSmokeLogFile(t, logPath, "install smoke: FAIL")
	if exitCode, _ := envelope.Error.Detail["exit_code"].(float64); exitCode != 1 {
		t.Fatalf("process error exit_code=%v want 1", envelope.Error.Detail["exit_code"])
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("failure invocation count=%d want 1", count)
	}

	if err := os.Remove(failFlagPath); err != nil {
		t.Fatal(err)
	}
	_, output, err = runInstallSmokeForTest(t, "reviewer", cfg, st)
	if err != nil || output.Status != "executed" || output.Result != "pass" {
		t.Fatalf("failure evidence must not be promoted to reuse: output=%+v err=%v", output, err)
	}
	if count := smokeInvocationCount(t, countPath); count != 2 {
		t.Fatalf("post-failure invocation count=%d want 2", count)
	}

	_, output, err = runInstallSmokeForTest(t, "parent", cfg, st)
	if err != nil || output.Status != "reused" {
		t.Fatalf("pass after failure must be reusable: output=%+v err=%v", output, err)
	}
	if count := smokeInvocationCount(t, countPath); count != 2 {
		t.Fatalf("final invocation count=%d want 2", count)
	}
}

func TestInstallSmokeEnvironmentChangeInvalidatesEvidence(t *testing.T) {
	cfg, st, _, countPath := newInstallSmokeEnv(t)

	if _, _, err := runInstallSmokeForTest(t, "worker", cfg, st); err != nil {
		t.Fatal(err)
	}
	if count := smokeInvocationCount(t, countPath); count != 1 {
		t.Fatalf("worker invocation count=%d want 1", count)
	}

	cfg.ClaudeBin = writeClaudeProbeShim(t, filepath.Join(t.TempDir(), "bin2"), "claude", false)
	_, output, err := runInstallSmokeForTest(t, "parent", cfg, st)
	if err != nil || output.Status != "executed" {
		t.Fatalf("changed toolchain environment must re-run: output=%+v err=%v", output, err)
	}
	if want := "stale:environment.claude_cli"; output.ReuseReason != want {
		t.Errorf("reuse_reason=%q want %q", output.ReuseReason, want)
	}
	if count := smokeInvocationCount(t, countPath); count != 2 {
		t.Fatalf("environment change invocation count=%d want 2", count)
	}
}

func TestParseCommandInstallSmokeRoles(t *testing.T) {
	tests := []struct {
		args      []string
		wantRole  string
		wantUsage bool
	}{
		{args: []string{"--install-smoke"}},
		{args: []string{"--install-smoke", "--role", "worker"}, wantRole: "worker"},
		{args: []string{"--install-smoke", "--role", "reviewer"}, wantRole: "reviewer"},
		{args: []string{"--install-smoke", "--role", "fix"}, wantRole: "fix"},
		{args: []string{"--install-smoke", "--role", "parent"}, wantRole: "parent"},
		{args: []string{"--install-smoke", "--role", "unknown"}, wantUsage: true},
		{args: []string{"--install-smoke", "--role"}, wantUsage: true},
		{args: []string{"--install-smoke", "extra"}, wantUsage: true},
	}
	for _, tt := range tests {
		cmd, err := ParseCommand(tt.args)
		if tt.wantUsage {
			if err == nil {
				t.Errorf("ParseCommand(%v) must fail", tt.args)
			} else if _, ok := err.(*UsageError); !ok {
				t.Errorf("ParseCommand(%v) error=%T want UsageError", tt.args, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseCommand(%v): %v", tt.args, err)
			continue
		}
		if cmd.Mode != ModeInstallSmoke || cmd.Role != tt.wantRole {
			t.Errorf("ParseCommand(%v) = %+v want role %q", tt.args, cmd, tt.wantRole)
		}
	}
}
