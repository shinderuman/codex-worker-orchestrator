package harnesslint

import (
	"os"
	"strings"
	"testing"
)

func TestCIWorkflowSkipsWebGPTBranchCreation(t *testing.T) {
	data, err := os.ReadFile("../../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	required := []string{
		"basic:\n    if: ${{ (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"lint:\n    if: ${{ always() && (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"install-smoke:\n    if: ${{ (github.event_name == 'push' && github.event.created == false && startsWith(github.ref, 'refs/heads/web-gpt/')) || github.event_name == 'workflow_dispatch' }}",
	}
	for _, token := range required {
		if !strings.Contains(text, token) {
			t.Fatalf("ci workflow branch-creation guard missing: %q", token)
		}
	}
}
