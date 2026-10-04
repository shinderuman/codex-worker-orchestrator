package observationexec

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	t.Setenv("HOME", filepath.Join(t.TempDir(), "host-home"))
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
