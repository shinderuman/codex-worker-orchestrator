package observationexec

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestCopiedModuleExecutableRuns(t *testing.T) {
	t.Parallel()
	source := filepath.Join(t.TempDir(), "source.sh")
	destination := filepath.Join(t.TempDir(), "copied.sh")
	if err := execfile.Write(source, []byte("#!/bin/sh\nprintf copied\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	copier := &moduleTreeCopier{}
	if err := copier.copyFile(destination, source, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(destination).Output(); err != nil || string(output) != "copied" {
		t.Fatalf("copied executable = %q, %v", output, err)
	}
}

func TestModuleTreeCopierEnforcesByteBudgetDuringCopy(t *testing.T) {
	sourceDir := t.TempDir()
	destinationDir := t.TempDir()
	source := filepath.Join(sourceDir, "oversized.txt")
	destination := filepath.Join(destinationDir, "oversized.txt")
	if err := os.WriteFile(source, []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	copier := &moduleTreeCopier{maxBytes: 4}
	if err := copier.copyFile(destination, source, 0o600); err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("copy error = %v, want byte-budget rejection", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("over-budget destination survived failed copy: %v", err)
	}
	if copier.bytes != 0 {
		t.Fatalf("committed bytes = %d want 0", copier.bytes)
	}
}

func TestBoundedTailBufferRetainsOnlyConfiguredTail(t *testing.T) {
	buffer := &boundedTailBuffer{limit: 4}
	for _, chunk := range [][]byte{[]byte("abc"), []byte("def"), []byte("ghijk")} {
		if _, err := buffer.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	data, total, truncated := buffer.snapshot()
	if got := string(data); got != "hijk" {
		t.Fatalf("tail = %q want hijk", got)
	}
	if total != 11 || !truncated {
		t.Fatalf("total=%d truncated=%v want 11/true", total, truncated)
	}
}

func TestDurableBoundedLogMarksTruncationWithinLimit(t *testing.T) {
	data := bytes.Repeat([]byte("x"), logMaxBytes)
	bounded := durableBoundedLog(data, int64(logMaxBytes+4096), true)
	if len(bounded) > logMaxBytes {
		t.Fatalf("durable log bytes=%d exceed limit=%d", len(bounded), logMaxBytes)
	}
	if !bytes.HasPrefix(bounded, []byte("[observation output truncated:")) {
		t.Fatalf("durable log does not declare truncation: %q", bounded[:min(len(bounded), 80)])
	}
}

func TestIsolatedGoTestEnvDoesNotInheritHostHome(t *testing.T) {
	hostHome := filepath.Join(t.TempDir(), "host-home")
	t.Setenv("HOME", hostHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(hostHome, "config"))
	t.Setenv("APPDATA", filepath.Join(hostHome, "config"))
	configHome, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(hostHome, configHome)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Fatalf("telemetry fixture escapes fake HOME: %q %v", configHome, err)
	}
	mode := filepath.Join(configHome, "go", "telemetry", "mode")
	if err := os.MkdirAll(filepath.Dir(mode), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mode, []byte("off"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "env", "GOTELEMETRY")
	command.Env = append(os.Environ(), "GOENV=off")
	if data, err := command.Output(); err != nil || strings.TrimSpace(string(data)) != "off" {
		t.Fatalf("real Go telemetry mode: %q %v", data, err)
	}
	tempRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tempRoot, "home"), 0o700); err != nil {
		t.Fatal(err)
	}
	env, err := isolatedGoTestEnv(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	wantHome := "HOME=" + filepath.Join(tempRoot, "home")
	for _, entry := range env {
		if strings.HasPrefix(entry, "HOME=") && entry != wantHome {
			t.Fatalf("host HOME leaked into isolated env: %q", entry)
		}
	}
	if !containsEnvEntry(env, wantHome) {
		t.Fatalf("isolated HOME missing: %#v", env)
	}
}

func containsEnvEntry(env []string, want string) bool {
	for _, entry := range env {
		if entry == want {
			return true
		}
	}
	return false
}
