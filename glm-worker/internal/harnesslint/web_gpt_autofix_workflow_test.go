package harnesslint

import (
	"os"
	"strings"
	"testing"
)

func TestWebGPTAutofixWorkflowSeparatesReadAndWriteCapabilities(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-autofix.yml")
	requireWebGPTAutofixTokens(t, text, []string{
		"on:\n  workflow_dispatch:",
		"workflow_call:",
		"target_branch:",
		"expected_head_sha:",
		"prepare:\n    permissions:\n      contents: read",
		"go -C control/glm-worker run ./cmd/web-gpt-dispatch-request fields autofix",
		"Checkout target at expected head without credentials",
		"ref: ${{ inputs.expected_head_sha }}\n          path: target\n          persist-credentials: false",
		"sh ../control/web-gpt-autofix.sh prepare",
		"publish:\n    needs: prepare",
		"needs.prepare.outputs.changed == 'true'",
		"actions: read\n      contents: write",
		"Checkout target at expected head for publication",
		"sh ../control/web-gpt-autofix.sh publish",
		"uses: actions/upload-artifact@v4",
		"uses: actions/download-artifact@v4",
	})
	for _, forbidden := range []string{"\n  push:", "\n  pull_request:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("autofix workflow must not gain automatic repository triggers; found %q", forbidden)
		}
	}
	publish := strings.SplitN(text, "\n  publish:\n", 2)
	if len(publish) != 2 {
		t.Fatal("publish job missing")
	}
	for _, forbidden := range []string{"harnesslint", "install-quality-tools.sh", "setup-go"} {
		if strings.Contains(publish[1], forbidden) {
			t.Fatalf("write-capable publish job must not execute quality tooling: %q", forbidden)
		}
	}
}

func TestWebGPTAutofixHelperFailsClosedAroundPreparedPublication(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../web-gpt-autofix.sh")
	requireWebGPTAutofixTokens(t, text, []string{
		"usage: web-gpt-autofix.sh <prepare|publish>",
		"case \"$target_branch\" in\n\tweb-gpt/*)",
		"git check-ref-format \"refs/heads/$target_branch\"",
		"run_controlled_harnesslint --deterministic-fix",
		"run_controlled_harnesslint --controlled-check",
		"cmp -s \"$patch_path\" \"$validation_patch\"",
		"validation_mutated_target",
		"git apply --check \"$patch_path\"",
		"git apply --index \"$patch_path\"",
		"git commit --no-verify -m 'Apply deterministic repository autofixes'",
		"git push origin \"HEAD:refs/heads/$target_branch\"",
		"publication=pushed",
		"unexpected_failure",
	})
	if strings.Contains(text, "--force") {
		t.Fatal("autofix helper must never force-push")
	}
	if strings.Contains(text, "run_controlled_harnesslint --fix") {
		t.Fatal("autofix helper must not call broad harnesslint --fix")
	}
}

func TestControlledAutofixUsesControlPolicyOnly(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "controlled_autofix.go")
	requireWebGPTAutofixTokens(t, text, []string{
		"newRealCommandRunner(controlRoot)",
		"commentlint.Run(runner.targetRoot, fix)",
		"controlled[index+1] = filepath.Join(controlRoot, \".golangci.yml\")",
		"fixGoFormatting(root, paths)",
		"runDeterministicShellFixes(root, paths, runner)",
	})
	for _, forbidden := range []string{"runExternalFixers(", "\"run\", \"--fix\", \"--config\""} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("controlled autofix must not use broad external fixer: %q", forbidden)
		}
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
