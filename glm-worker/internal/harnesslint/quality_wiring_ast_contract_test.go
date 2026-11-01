package harnesslint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQualityWiringPackageRejectsCommentAndStringPins(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "comment is not invocation",
			content: "package workflow\n// w.qualityGate(w.config.RepoRoot)\nfunc example() {}\n",
			want:    true,
		},
		{
			name:    "string is not invocation",
			content: "package workflow\nconst claim = \"w.qualityGate(w.config.RepoRoot)\"\n",
			want:    true,
		},
		{
			name:    "whitespace and same-call argument do not affect semantic call",
			content: "package workflow\nfunc example() { w.qualityGate(\n   root,\n ) }\n",
			want:    false,
		},
		{
			name:    "different method cannot satisfy wiring",
			content: "package workflow\nfunc example() { w.qualityGateOther(root) }\n",
			want:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := "glm-worker/internal/workflow/probe.go"
			abs := filepath.Join(root, filepath.FromSlash(path))
			if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(abs, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := qualityWiringPackageViolations(root, map[string]bool{path: true},
				"glm-worker/internal/workflow/", []string{"w.qualityGate(w.config.RepoRoot)"})
			if err != nil {
				t.Fatal(err)
			}
			if (len(result) != 0) != tc.want {
				t.Fatalf("missing call violation=%v want=%v: %#v", len(result) != 0, tc.want, result)
			}
			if len(result) != 0 && !strings.Contains(result[0].Message, "w.qualityGate") {
				t.Fatalf("unexpected missing wiring: %#v", result)
			}
		})
	}
}
