package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestResumePromptNormalStopsShareGenericContinuation(t *testing.T) {
	const original = "ORIGINAL TASK AUTHORITY"
	var baseline string
	for _, kind := range []state.ResumeStopKind{
		state.ResumeStopRateLimited,
		state.ResumeStopProviderUnavailable,
		state.ResumeStopInterrupted,
	} {
		t.Run(string(kind), func(t *testing.T) {
			prompt := resumePrompt(state.ResumeCheckpoint{StopKind: kind, OriginalPrompt: original})
			if !strings.Contains(prompt, "MODE: RESUME_TASK") || !strings.Contains(prompt, original) {
				t.Fatalf("generic continuation contract missing: %q", prompt)
			}
			for _, forbidden := range []string{"5時間", "plan-limit", "provider-unavailable", "一時的なprovider障害", "RESUME_REASON:"} {
				if strings.Contains(prompt, forbidden) {
					t.Fatalf("transport stop reason leaked into prompt for %s: %q", kind, prompt)
				}
			}
			if baseline == "" {
				baseline = prompt
			} else if prompt != baseline {
				t.Fatalf("normal stop kinds must use identical continuation semantics: kind=%s\nwant=%q\ngot=%q", kind, baseline, prompt)
			}
		})
	}
}

func TestResumePromptPreservesOriginalPromptAuthority(t *testing.T) {
	checkpoint := state.ResumeCheckpoint{
		StopKind:       state.ResumeStopInterrupted,
		Prompt:         "already wrapped resume prompt",
		OriginalPrompt: "ORIGINAL TASK AUTHORITY",
	}
	prompt := resumePrompt(checkpoint)
	if !strings.Contains(prompt, checkpoint.OriginalPrompt) {
		t.Fatalf("original prompt authority missing: %q", prompt)
	}
	if strings.Contains(prompt, checkpoint.Prompt) {
		t.Fatalf("resume prompt recursively reused wrapped prompt: %q", prompt)
	}
}

func TestResumePromptKeepsSpecializedGuardRecovery(t *testing.T) {
	prompt := resumePrompt(state.ResumeCheckpoint{
		StopKind:       state.ResumeStopGuardRecoverable,
		OriginalPrompt: "ORIGINAL TASK",
	})
	if !strings.Contains(prompt, "RESUME_REASON: guard-recovery") || !strings.Contains(prompt, "新しいsession") {
		t.Fatalf("guard recovery semantics lost: %q", prompt)
	}
}

func TestResumePromptQualityGatePreservesOriginalWithoutWrapper(t *testing.T) {
	const original = "ORIGINAL QUALITY-GATE TASK"
	prompt := resumePrompt(state.ResumeCheckpoint{
		StopKind:       state.ResumeStopQualityGate,
		OriginalPrompt: original,
	})
	if prompt != original {
		t.Fatalf("quality-gate result reuse must not synthesize a model-visible resume wrapper: %q", prompt)
	}
}

func TestPrepareResumeContinuationRejectsUnsupportedStopKind(t *testing.T) {
	w := newWorkflowT(t, newStateStoreT(t), &scriptedRunner{})
	for _, kind := range []state.ResumeStopKind{state.ResumeStopNone, state.ResumeStopKind("unknown-stop")} {
		t.Run(string(kind), func(t *testing.T) {
			_, err := w.prepareResumeContinuation(
				state.ResumeCheckpoint{StopKind: kind, Phase: "worker-new", OriginalPrompt: "ORIGINAL TASK"},
				externalFeasibility{},
				qualitySurfaceApprovalResumeNone,
			)
			if err == nil || !strings.Contains(err.Error(), "unsupported resume stop kind") {
				t.Fatalf("unsupported stop kind must fail closed: kind=%q err=%v", kind, err)
			}
		})
	}
}
