//go:build !linux && !darwin && !windows

package parentevidence

import (
	"strings"
	"testing"
)

func TestDiffWorktreeUnsupportedPlatformFailsClosed(t *testing.T) {
	for _, path := range []string{"review.go", "missing.go"} {
		t.Run(path, func(t *testing.T) {
			sha, mode, content, present, err := diffWorktreeObject(t.TempDir(), path)
			if err == nil || !strings.Contains(err.Error(), "unsupported on this platform") || !strings.Contains(err.Error(), path) {
				t.Fatalf("expected unsupported-platform error for %q, got %v", path, err)
			}
			if sha != "" || mode != "" || content != nil || present {
				t.Fatalf("unsupported platform returned capture evidence: sha=%q mode=%q content=%v present=%v", sha, mode, content, present)
			}
		})
	}
}
