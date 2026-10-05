package harnesslint

import "testing"

func TestQualityWiringForbiddenCheckUsesSyntax(t *testing.T) {
	check := qualityWiringCheck{
		path:            "glm-worker/internal/workflow/quality_gate.go",
		forbiddenTokens: []string{"harnesslint.Check(root)"},
	}
	forbidden, err := qualityWiringForbiddenCalls(check, []byte(`package workflow
func run(repoRoot string) { harnesslint.Check(repoRoot) }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(forbidden) != 1 || forbidden[0] != "harnesslint.Check" {
		t.Fatalf("forbidden = %#v", forbidden)
	}
}
