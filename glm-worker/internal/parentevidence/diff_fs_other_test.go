//go:build !linux && !darwin && !windows

package parentevidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiffWorktreeUnsupportedPlatformFailsClosed(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoRoot, "review.go"), []byte("present\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"review.go", "missing.go"} {
		t.Run(path, func(t *testing.T) {
			sha, mode, content, present, err := diffWorktreeObject(repoRoot, path)
			if err == nil || !strings.Contains(err.Error(), "unsupported on this platform") || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected unsupported-platform error for %q, got %v", path, err)
			}
			if sha != "" || mode != "" || content != nil || present {
				t.Fatalf("unsupported platform returned capture evidence: sha=%q mode=%q content=%v present=%v", sha, mode, content, present)
			}
		})
	}
}
