package harnesslint

import (
	"strings"
	"testing"
)

func TestWebGPTMailboxWorkflowPinsDedicatedIssueAndActorBoundary(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-mailbox.yml")
	requireWebGPTAutofixTokens(t, text, []string{
		"on:\n  issues:\n    types:\n      - edited",
		"if: ${{ github.event.issue.number == 1236 }}",
		"CONTROL_REF: ${{ github.ref }}",
		"test \"$CONTROL_REF\" = refs/heads/main",
		"ref: ${{ github.sha }}\n          path: control\n          persist-credentials: false",
		"jq -r '.issue.body // \"\"' \"$GITHUB_EVENT_PATH\"",
		"MAILBOX_ISSUE: ${{ github.event.issue.number }}",
		"MAILBOX_ACTOR: ${{ github.actor }}",
		"go -C control/glm-worker run ./cmd/web-gpt-dispatch-request mailbox",
		"web-gpt-mailbox-admission-${{ github.run_id }}",
	})
	for _, forbidden := range []string{
		"\n  push:",
		"\n  pull_request:",
		"issue_comment:",
		"github.event.issue.title",
		"secrets.",
		"persist-credentials: true",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("mailbox workflow contains forbidden transport surface: %q", forbidden)
		}
	}
}

func TestWebGPTMailboxWorkflowReusesCanonicalOperationWorkflows(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-mailbox.yml")
	requireWebGPTAutofixTokens(t, text, []string{
		"needs.parse.outputs.operation == 'autofix'",
		"uses: ./.github/workflows/web-gpt-autofix.yml",
		"actions: read\n      contents: write",
		"needs.parse.outputs.operation == 'validate'",
		"uses: ./.github/workflows/web-gpt-validate.yml",
		"permissions:\n      contents: read",
		"target_branch: ${{ needs.parse.outputs.target_branch }}",
		"expected_head_sha: ${{ needs.parse.outputs.expected_head_sha }}",
		"validation_mode: ${{ needs.parse.outputs.validation_mode }}",
		"package_scope: ${{ needs.parse.outputs.package_scope }}",
	})
	for _, forbidden := range []string{
		"target/web-gpt-",
		"target/.github/workflows",
		"sh ../target/",
		"uses: ./target/",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("mailbox dispatcher must not execute target-owned control: %q", forbidden)
		}
	}
}

func TestWebGPTMailboxWorkflowEmitsTransportRelationship(t *testing.T) {
	text := readWebGPTAutofixContractFile(t, "../../../.github/workflows/web-gpt-mailbox.yml")
	for _, key := range []string{
		"mailbox_issue",
		"request_id",
		"operation",
		"control_sha",
		"target_branch",
		"target_sha",
		"run_id",
		"operation_result",
		"operation_result_artifact",
	} {
		if !strings.Contains(text, key) {
			t.Fatalf("mailbox transport evidence key missing: %s", key)
		}
	}
	requireWebGPTAutofixTokens(t, text, []string{
		"web-gpt-autofix-result-$RUN_ID",
		"web-gpt-validation-result-$RUN_ID",
		"web-gpt-mailbox-result-${{ github.run_id }}",
	})
}
