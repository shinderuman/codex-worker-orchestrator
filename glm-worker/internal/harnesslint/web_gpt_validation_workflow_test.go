package harnesslint

import (
	"strings"
	"testing"
)

func TestWebGPTValidationWorkflowPinsReadOnlyCanonicalControl(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-validate.yml")
	requireWebGPTAutofixTokens(t, text, []string{
		"on:\n  workflow_dispatch:",
		"workflow_call:",
		"validation_mode:\n        description: Repository-owned validation mode\n        required: true\n        type: choice",
		"- repository-lint",
		"- go-package-test",
		"- full-go-test",
		"- build-vet",
		"permissions:\n  contents: read",
		"go -C control/glm-worker run ./cmd/web-gpt-dispatch-request fields validate",
		"ref: ${{ github.sha }}\n          path: control\n          persist-credentials: false",
		"ref: ${{ inputs.expected_head_sha }}\n          path: target\n          persist-credentials: false",
		"WEB_GPT_VALIDATION_CONTROL_ROOT: ${{ github.workspace }}/control",
		"QUALITY_TOOLS_BIN_DIR: ${{ runner.temp }}/codex-worker-orchestrator-quality-tools",
		"sh ../control/web-gpt-validate.sh",
		"uses: actions/upload-artifact@v4",
	})
	for _, forbidden := range []string{"contents: write", "persist-credentials: true", "secrets.", "uses: actions/cache", "$GITHUB_ENV", "\n  push:", "\n  pull_request:"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("focused validation workflow contains forbidden capability: %q", forbidden)
		}
	}
}

func TestWebGPTValidationControllerUsesBoundedCommands(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../web-gpt-validate.sh")
	requireWebGPTAutofixTokens(t, text, []string{
		"case \"$validation_mode\" in",
		"repository-lint)\n\trun_repository_lint",
		"go-package-test)\n\trun_go_package_test",
		"full-go-test)\n\trun_full_go_test",
		"build-vet)\n\trun_build_vet",
		"HARNESSLINT_REPO_ROOT=\"$target_root\" HARNESSLINT_CONTROL_ROOT=\"$control_root\" ./harnesslint --controlled-check",
		"go -C \"$target_root/glm-worker\" test \"$validation_scope\"",
		"go -C \"$target_root/glm-worker\" test ./...",
		"go -C \"$target_root/glm-worker\" vet ./...",
		"go -C \"$target_root/glm-worker\" build ./...",
		"$control_root/quality-tools.yml",
		"persisted_git_credentials",
		"stale_expected_head",
	})
	for _, forbidden := range []string{"eval ", "sh -c", "$target_root/quality-tools.yml", "$target_root/goquality", "$target_root/install.sh", "$target_root/tests/"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("focused validation controller contains forbidden target-controlled execution: %q", forbidden)
		}
	}
}
