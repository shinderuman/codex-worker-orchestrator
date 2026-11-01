package harnesslint

import (
	"strings"
	"testing"
)

func TestQualityWiringGoChecksIgnoreCommentsAndAcceptWhitespace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		source  string
		missing string
	}{
		{
			name:   "real calls with reformatted arguments",
			source: "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(\n root,\n true,\n) }\n",
		},
		{
			name:    "comment and string cannot impersonate live quality gate",
			source:  "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\n// harnesslint.Run(root, true)\nconst claim = \"captureQualitySurfaceDigest\"\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "decoy helper cannot impersonate canonical quality gate",
			source:  "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc decoy() { harnesslint.Run(root, true) }\nfunc runRepositoryQualityGate(root string) {}\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "wrong run mode is not accepted",
			source:  "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(root, false) }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "function name in comment is not implementation",
			source:  "package workflow\nimport \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\n// captureQualitySurfaceDigest\nfunc runRepositoryQualityGate(root string) { harnesslint.Run(root, true) }\n",
			missing: "captureQualitySurfaceDigest",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			check := qualityWiringChecks()[0]
			writeQualityFile(t, root, check.path, tc.source)
			violations, err := qualityWiringCheckViolations(root, map[string]bool{check.path: true}, check)
			if err != nil {
				t.Fatal(err)
			}
			if tc.missing == "" && len(violations) != 0 {
				t.Fatalf("equivalent quality wiring was rejected: %#v", violations)
			}
			if tc.missing != "" {
				found := false
				for _, violation := range violations {
					found = found || strings.Contains(violation.Message, "required quality-gate wiring is missing: "+tc.missing)
				}
				if !found {
					t.Fatalf("missing live quality wiring was accepted: %#v", violations)
				}
			}
		})
	}
}
