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
	guards := ciWorkflowJobGuards(string(data))
	expected := map[string]string{
		"basic":         "${{ (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"lint":          "${{ always() && (github.event_name != 'push' || github.event.created == false) && (github.event_name != 'pull_request' || !startsWith(github.head_ref, 'web-gpt/')) }}",
		"install-smoke": "${{ (github.event_name == 'push' && github.event.created == false && startsWith(github.ref, 'refs/heads/web-gpt/')) || github.event_name == 'workflow_dispatch' }}",
	}
	for name, want := range expected {
		if strings.Join(strings.Fields(guards[name]), " ") != strings.Join(strings.Fields(want), " ") {
			t.Errorf("CI job %q guard differs: got %q, want %q", name, guards[name], want)
		}
	}
}

func ciWorkflowJobGuards(source string) map[string]string {
	result := make(map[string]string)
	inJobs := false
	jobIndent, fieldIndent := -1, -1
	currentJob := ""
	for _, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent == 0 {
			inJobs = trimmed == "jobs:"
			jobIndent, fieldIndent = -1, -1
			currentJob = ""
			continue
		}
		if !inJobs {
			continue
		}
		if jobIndent == -1 {
			jobIndent = indent
		}
		if indent == jobIndent && strings.HasSuffix(trimmed, ":") {
			currentJob = strings.TrimSuffix(trimmed, ":")
			fieldIndent = -1
			continue
		}
		if currentJob == "" || indent <= jobIndent {
			continue
		}
		if fieldIndent == -1 {
			fieldIndent = indent
		}
		if indent == fieldIndent && strings.HasPrefix(trimmed, "if:") {
			result[currentJob] = strings.TrimSpace(strings.TrimPrefix(trimmed, "if:"))
		}
	}
	return result
}

func TestCIJobGuardExtractionIgnoresNestedAndCommentedCondition(t *testing.T) {
	t.Parallel()
	source := "jobs:\n" +
		"    basic:\n" +
		"      # if: ${{ false }}\n" +
		"      name: actual\n" +
		"      if: ${{ true }}\n" +
		"      steps:\n" +
		"        - if: ${{ false }}\n" +
		"  # if: ${{ false }}\n"
	guards := ciWorkflowJobGuards(source)
	if guards["basic"] != "${{ true }}" {
		t.Fatalf("nested or comment-only guard displaced job-level if: %#v", guards)
	}
	if len(ciWorkflowJobGuards("jobs:\n  basic:\n    # if: ${{ true }}\n    steps: []\n")) != 0 {
		t.Fatal("comment-only job guard was accepted")
	}
}
