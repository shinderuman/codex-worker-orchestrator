package workflow

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestReviewerGetsCurrentArtifactContext(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: passPacket()}}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageReview,
		Phase:  "reviewer-1",
		Role:   state.ReviewerRole,
		Model:  "haiku",
		Effort: "high",
		Prompt: "review WORKER_REPORT artifact /old/task/report.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	artifactDir := st.ArtifactDir(taskID)
	prompt := r.prompts[0]
	if !strings.Contains(prompt, reviewerArtifactPromptMarker+" "+artifactDir) {
		t.Fatalf("reviewer prompt has no current artifact root: %q", prompt)
	}
	if strings.Contains(prompt, artifactPromptMarker) {
		t.Fatalf("reviewer prompt contains worker artifact write context: %q", prompt)
	}
	if !strings.Contains(prompt, priorArtifactReferenceMarker) {
		t.Fatalf("reviewer prompt has no prior-artifact marker: %q", prompt)
	}
}

func TestReviewerArtifactViolationCorrectionGetsCurrentRoot(t *testing.T) {
	st := newStateStoreT(t)
	if _, err := st.PrepareArtifactDir(); err != nil {
		t.Fatal(err)
	}
	oldArtifact := filepath.Join(t.TempDir(), "old-task-artifact.md")
	r := &scriptedRunner{steps: []runnerStep{
		{structured: reviewerPacketWithArtifacts(oldArtifact)},
		{structured: passPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	result, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageReview,
		Phase:  "reviewer-1",
		Role:   state.ReviewerRole,
		Model:  "haiku",
		Effort: "high",
		Prompt: "review prior task evidence",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != packet.StatusPass {
		t.Fatalf("result status = %s", result.Status)
	}
	if len(r.prompts) != 2 || len(r.phases) != 2 || r.phases[1] != "reviewer-1-result-correct" {
		t.Fatalf("correction calls = prompts:%d phases:%v", len(r.prompts), r.phases)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	artifactDir := st.ArtifactDir(taskID)
	correction := r.prompts[1]
	if !strings.Contains(correction, reviewerArtifactPromptMarker+" "+artifactDir) {
		t.Fatalf("correction prompt has no current artifact root: %q", correction)
	}
	if strings.Contains(correction, artifactPromptMarker) {
		t.Fatalf("correction prompt contains reviewer write context: %q", correction)
	}
	if !strings.Contains(correction, oldArtifact) {
		t.Fatalf("correction prompt lost rejected artifact evidence: %q", correction)
	}
	if !strings.Contains(correction, rejectedArtifactMarker) {
		t.Fatalf("correction prompt has no rejected-artifact marker: %q", correction)
	}
}

func reviewerPacketWithArtifacts(artifact string) string {
	return packetBody(packet.Result{
		Status:              packet.StatusPass,
		Risk:                packet.RiskLow,
		Summary:             "pass",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "none",
		ResidualRisk:        "none",
		Targets:             []string{"final diff"},
		Artifacts:           []string{artifact},
	})
}
