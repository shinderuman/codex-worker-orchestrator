package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResolvedActiveTaskPromptsOmitFallbackRequestAuthority(t *testing.T) {
	request := "fixed-parent-request-sentinel"
	activeTaskPath := "IMPLEMENTATION_TASKS/task.md"

	prompts := map[string]string{
		"new task":    newTaskPrompt(request, activeTaskPath),
		"decision":    decisionPrompt(request, "decision-delta", activeTaskPath),
		"explicit fix": explicitFixPrompt(request, "decision-delta", "previous-review", "review-feedback", activeTaskPath),
		"reviewer": reviewerPrompt(
			request,
			"decision-delta",
			`{"status":"IMPLEMENTED","risk":"LOW","summary":"done"}`,
			1,
			"baseline",
			"navigation",
			activeTaskPath,
		),
		"automatic fix": automaticFixPrompt(request, "decision-delta", "review-report", activeTaskPath),
		"report only":   reportOnlyFixPrompt(request, "decision-delta", "review-report", activeTaskPath),
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(prompt, request) {
				t.Fatalf("resolved ACTIVE task prompt still projects fallback request:\n%s", prompt)
			}
			if !strings.Contains(prompt, "ACTIVE_TASK_FILE: "+activeTaskPath) {
				t.Fatalf("resolved ACTIVE task prompt lost canonical task authority:\n%s", prompt)
			}
		})
	}
}

func TestUnboundPromptsPreserveRequestAuthority(t *testing.T) {
	request := "semantic-unbound-request"
	prompts := map[string]string{
		"new task":      newTaskPrompt(request, ""),
		"decision":      decisionPrompt(request, "decision-delta", ""),
		"explicit fix":  explicitFixPrompt(request, "decision-delta", "previous-review", "review-feedback", ""),
		"reviewer":      reviewerPrompt(request, "decision-delta", `{"status":"IMPLEMENTED","risk":"LOW","summary":"done"}`, 1, "baseline", "navigation", ""),
		"automatic fix": automaticFixPrompt(request, "decision-delta", "review-report", ""),
		"report only":   reportOnlyFixPrompt(request, "decision-delta", "review-report", ""),
	}

	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			if !strings.Contains(prompt, request) {
				t.Fatalf("unbound prompt lost USER_REQUEST fallback authority:\n%s", prompt)
			}
			if strings.Contains(prompt, "ACTIVE_TASK_FILE:") {
				t.Fatalf("unbound prompt must not invent ACTIVE task authority:\n%s", prompt)
			}
		})
	}
}

func TestManagedResumeKeepsActiveTaskAuthorityWithoutFallbackRequest(t *testing.T) {
	request := "fixed-parent-request-sentinel"
	activeTaskPath := "IMPLEMENTATION_TASKS/task.md"
	original := decisionPrompt(request, "decision-delta", activeTaskPath)
	got := resumePrompt(state.ResumeCheckpoint{OriginalPrompt: original})

	if strings.Contains(got, request) {
		t.Fatalf("resume prompt restored fallback request that managed prompt omitted:\n%s", got)
	}
	if !strings.Contains(got, "ACTIVE_TASK_FILE: "+activeTaskPath) {
		t.Fatalf("resume prompt lost canonical ACTIVE task authority:\n%s", got)
	}
	if !strings.Contains(got, "SOL_DECISION:\ndecision-delta") {
		t.Fatalf("resume prompt lost action-specific semantic delta:\n%s", got)
	}
}
