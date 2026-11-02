package harnesslint

import (
	"strings"
	"testing"
)

func TestQualityWiringGoChecksIgnoreCommentsAndAcceptWhitespace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		source    string
		missing   string
		forbidden bool
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
			name:    "renamed formal parameter",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(repositoryRoot string) { gate.Run(repositoryRoot, true) }\n",
			missing: "",
		},
		{
			name:    "parenthesized equivalent call",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(repositoryRoot string) { (gate.Run)((repositoryRoot), (true)) }\n",
			missing: "",
		},
		{
			name:    "unrelated nested shadow preserves the outer formal",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { { root := \"other\"; _ = root }; gate.Run(root, true) }\n",
			missing: "",
		},
		{
			name:    "same-scope short declaration retains the formal object",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc normalize(root string) (string, error) { return root, nil }; func runRepositoryQualityGate(root string) { root, err := normalize(root); _ = err; gate.Run(root, true) }\n",
			missing: "",
		},
		{
			name:    "package boolean cannot impersonate predeclared true",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nvar true = false; func runRepositoryQualityGate(root string) { gate.Run(root, true) }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "constant true alias preserves fix mode",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { const fix = true; gate.Run(root, fix) }\n",
			missing: "",
		},
		{
			name:    "constant false alias is not fix mode",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { const fix = false; gate.Run(root, fix) }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:      "unrelated local Check is not the imported Check",
			source:    "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\ntype fakeGate struct{}; func (fakeGate) Check(string) {}; func runRepositoryQualityGate(root string) { gate.Run(root, true); { gate := fakeGate{}; gate.Check(root) } }\n",
			forbidden: false,
		},
		{
			name:      "parenthesized imported Check is forbidden",
			source:    "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { gate.Run(root, true); (gate.Check)(root) }\n",
			forbidden: true,
		},
		{
			name:    "other root cannot satisfy the formal binding",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { otherRoot := \"other\"; gate.Run(otherRoot, true) }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "shadowed root cannot satisfy the formal binding",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { { root := \"other\"; gate.Run(root, true) } }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "shadowed package cannot impersonate canonical import",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nvar _ = gate.Check; func runRepositoryQualityGate(root string) { gate := fakeGate{}; gate.Run(root, true) }\n",
			missing: "harnesslint.Run(root, true)",
		},
		{
			name:    "shadowed true is not the fix mode",
			source:  "package workflow\nimport gate \"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint\"\nfunc captureQualitySurfaceDigest(root string) {}\nfunc runRepositoryQualityGate(root string) { true := false; gate.Run(root, true) }\n",
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
			if tc.forbidden {
				for _, violation := range violations {
					if strings.Contains(violation.Message, "forbidden quality-gate wiring is present") {
						return
					}
				}
				t.Fatalf("imported Check was accepted: %#v", violations)
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
