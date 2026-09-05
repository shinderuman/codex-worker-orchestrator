package state

import "testing"

func TestFixPathCategory(t *testing.T) {
	tests := []struct {
		relPath string
		want    string
	}{
		{"AGENTS.md", FixCategoryInstruction},
		{"AGENTS.local.md", FixCategoryInstruction},
		{"CLAUDE.md", FixCategoryInstruction},
		{"codex/instructions/glm-execution.md", FixCategoryInstruction},
		{".codex/instructions/agents.md", FixCategoryInstruction},
		{"IMPLEMENTATION_PLAN.local.md", FixCategoryMetadata},
		{"IMPLEMENTATION_RULES.md", FixCategoryMetadata},
		{"IMPLEMENTATION_HISTORY.md", FixCategoryMetadata},
		{"IMPLEMENTATION_TASKS/codex-review-gap-telemetry.md", FixCategoryMetadata},
		{"internal/app/review_gap_test.go", FixCategoryTest},
		{"pkg/lib/widget.spec.ts", FixCategoryTest},
		{"pkg/lib/widget_spec.ts", FixCategoryProduction},
		{"src/util/app.test.js", FixCategoryTest},
		{"tests/helper.go", FixCategoryTest},
		{"scenarios/worker-flow.json", FixCategoryTest},
		{"testdata/input.txt", FixCategoryTest},
		{"test_frobnicate.py", FixCategoryTest},
		{".glm-worker/telemetry/task.jsonl", FixCategoryTelemetry},
		{"internal/telemetry/scan.go", FixCategoryTelemetry},
		{"README.md", FixCategoryDocumentation},
		{"docs/architecture.md", FixCategoryDocumentation},
		{"internal/app/review_gap.go", FixCategoryProduction},
		{"cmd/glm-worker/main.go", FixCategoryProduction},
		{"install.sh", FixCategoryOther},
		{"quality-tools.yml", FixCategoryOther},
		{"agents.md", FixCategoryInstruction},
		{"./codex/instructions/glm-packets.md", FixCategoryInstruction},
		{"Codex/Instructions/Other.md", FixCategoryInstruction},
		{"implementation_tasks/some-task.md", FixCategoryMetadata},
		{"Internal/App/Review_Gap_Test.go", FixCategoryTest},
	}
	for _, test := range tests {
		if got := FixPathCategory(test.relPath); got != test.want {
			t.Errorf("FixPathCategory(%q) = %q, want %q", test.relPath, got, test.want)
		}
	}
}
