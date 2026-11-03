package harnesslint

import "testing"

func TestQualityWiringForbiddenCheckUsesSyntax(t *testing.T) {
	check := qualityWiringCheck{
		path:            "glm-worker/internal/workflow/quality_gate.go",
		forbiddenTokens: []string{"harnesslint.Check(root)"},
	}
	source := []byte(`package workflow
import "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
func run(repoRoot string) { harnesslint.Check(repoRoot) }
`)
	evidence, err := qualityWiringGoTokensForPath(check.path, source)
	if err != nil {
		t.Fatal(err)
	}
	forbidden, err := qualityWiringForbiddenCalls(check, source, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(forbidden) != 1 || forbidden[0] != "harnesslint.Check" {
		t.Fatalf("forbidden = %#v", forbidden)
	}
}
