package harnesslint

import (
	"strings"
	"testing"
)

func TestQualityWiringPackageRequiresBoundSelectors(t *testing.T) {
	const canonicalImport = "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	for _, tc := range []struct {
		name, source, token string
		missing             bool
	}{
		{
			name: "canonical package alias",
			source: "package workflow\nimport gate \"" + canonicalImport + "\"\n" +
				"func (w *Workflow) review() { gate.IsViolation(qualityReport) }\n",
			token: "harnesslint.IsViolation(qualityReport)",
		},
		{
			name: "canonical package name",
			source: "package workflow\nimport \"" + canonicalImport + "\"\n" +
				"func (w *Workflow) review() { harnesslint.IsViolation(qualityReport) }\n",
			token: "harnesslint.IsViolation(qualityReport)",
		},
		{
			name: "renamed workflow receiver",
			source: "package workflow\n" +
				"func (handler *Workflow) review() { handler.qualityGate(handler.config.RepoRoot) }\n",
			token: "w.qualityGate(w.config.RepoRoot)",
		},
		{
			name: "shadowed package alias",
			source: "package workflow\nimport gate \"" + canonicalImport + "\"\n" +
				"type fakeGate struct{}\nfunc (fakeGate) IsViolation(any) bool { return true }\n" +
				"func (w *Workflow) review() { gate := fakeGate{}; gate.IsViolation(qualityReport) }\n",
			token:   "harnesslint.IsViolation(qualityReport)",
			missing: true,
		},
		{
			name: "shadowed canonical import name",
			source: "package workflow\nimport \"" + canonicalImport + "\"\n" +
				"type fakeGate struct{}\nfunc (fakeGate) IsViolation(any) bool { return true }\n" +
				"func (w *Workflow) review() { harnesslint := fakeGate{}; harnesslint.IsViolation(qualityReport) }\n",
			token:   "harnesslint.IsViolation(qualityReport)",
			missing: true,
		},
		{
			name: "shadowed workflow receiver",
			source: "package workflow\n" +
				"type fakeWorkflow struct { config struct { RepoRoot string } }\n" +
				"func (fakeWorkflow) qualityGate(string) {}\n" +
				"func (w *Workflow) review() { { w := fakeWorkflow{}; w.qualityGate(w.config.RepoRoot) } }\n",
			token:   "w.qualityGate(w.config.RepoRoot)",
			missing: true,
		},
		{
			name: "fake selector without canonical import",
			source: "package workflow\n" +
				"type fakeGate struct{}\nfunc (fakeGate) IsViolation(any) bool { return true }\n" +
				"func (w *Workflow) review() { fake := fakeGate{}; fake.IsViolation(qualityReport) }\n",
			token:   "harnesslint.IsViolation(qualityReport)",
			missing: true,
		},
		{
			name: "comment decoy",
			source: "package workflow\nimport gate \"" + canonicalImport + "\"\n" +
				"func (w *Workflow) review() { /* gate.IsViolation(qualityReport) */ }\n",
			token:   "harnesslint.IsViolation(qualityReport)",
			missing: true,
		},
		{
			name: "real receiver after unrelated nested shadow",
			source: "package workflow\n" +
				"type fakeWorkflow struct { config struct { RepoRoot string } }\n" +
				"func (fakeWorkflow) qualityGate(string) {}\n" +
				"func (w *Workflow) review() { { w := fakeWorkflow{}; w.qualityGate(w.config.RepoRoot) }; w.qualityGate(w.config.RepoRoot) }\n",
			token: "w.qualityGate(w.config.RepoRoot)",
		},
		{
			name: "real import after unrelated nested shadow",
			source: "package workflow\nimport gate \"" + canonicalImport + "\"\n" +
				"type fakeGate struct{}\nfunc (fakeGate) IsViolation(any) bool { return true }\n" +
				"func (w *Workflow) review() { { gate := fakeGate{}; gate.IsViolation(qualityReport) }; gate.IsViolation(qualityReport) }\n",
			token: "harnesslint.IsViolation(qualityReport)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := "glm-worker/internal/workflow/review.go"
			writeQualityFile(t, root, path, tc.source)
			violations, err := qualityWiringPackageViolations(root, map[string]bool{path: true},
				"glm-worker/internal/workflow/", []string{tc.token})
			if err != nil {
				t.Fatal(err)
			}
			if tc.missing {
				if len(violations) != 1 || !strings.Contains(violations[0].Message, "required quality-gate wiring is missing: "+tc.token) {
					t.Fatalf("unbound or decoy selector satisfied quality wiring: %+v", violations)
				}
			} else if len(violations) != 0 {
				t.Fatalf("bound selector was rejected: %+v", violations)
			}
		})
	}
}
