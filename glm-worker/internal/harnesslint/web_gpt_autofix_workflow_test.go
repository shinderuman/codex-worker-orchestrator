package harnesslint

import (
	"os"
	"strings"
	"testing"
)

func TestWebGPTAutofixWorkflowIsManualAndPinned(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-autofix.yml")
	requireWebGPTAutofixTokens(t, text, []string{
		"on:\n  workflow_dispatch:",
		"target_branch:",
		"expected_head_sha:",
		"permissions:\n  contents: write",
		"group: web-gpt-autofix-${{ inputs.target_branch }}",
		"ref: ${{ inputs.expected_head_sha }}",
		"QUALITY_TOOLS_BIN_DIR=\"$quality_bin\" ./install-quality-tools.sh",
		"sh ./web-gpt-autofix.sh \"$TARGET_BRANCH\" \"$EXPECTED_HEAD_SHA\" \"$RUNNER_TEMP/web-gpt-autofix-result.json\"",
		"uses: actions/upload-artifact@v4",
	})
	for _, forbidden := range []string{"\n  push:", "\n  pull_request:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("autofix workflow must remain workflow_dispatch-only; found %q", forbidden)
		}
	}
}

func TestWebGPTAutofixHelperFailsClosedAroundPublication(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../web-gpt-autofix.sh")
	requireWebGPTAutofixTokens(t, text, []string{
		"case \"$target_branch\" in\nweb-gpt/*)",
		"git check-ref-format \"refs/heads/$target_branch\"",
		"remote_head=$(git ls-remote --exit-code origin \"refs/heads/$target_branch\"",
		"if [ \"$remote_head\" != \"$expected_head_sha\" ]",
		"./harnesslint --fix",
		"./harnesslint",
		"git ls-files --others --exclude-standard",
		"git add -u",
		"git commit -m 'Apply deterministic repository autofixes'",
		"remote_before_publish=$(git ls-remote --exit-code origin \"refs/heads/$target_branch\"",
		"if [ \"$remote_before_publish\" != \"$expected_head_sha\" ]",
		"git push origin \"HEAD:refs/heads/$target_branch\"",
		"publication=pushed",
		"\"before_sha\"",
		"\"changed\"",
		"\"resulting_head\"",
		"\"validation\"",
	})
	if strings.Contains(text, "--force") {
		t.Fatal("autofix helper must never force-push")
	}
}

func readWebGPTAutofixContractFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func requireWebGPTAutofixTokens(t *testing.T, text string, tokens []string) {
	t.Helper()
	for _, token := range tokens {
		if !strings.Contains(text, token) {
			t.Fatalf("Web GPT autofix contract token missing: %q", token)
		}
	}
}
