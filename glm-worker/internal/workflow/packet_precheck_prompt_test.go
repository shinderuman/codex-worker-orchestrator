package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeArtifactContextCarriesOnlyDynamicProjection(t *testing.T) {
	worker := withArtifactContext("implementation instruction", "/tmp/artifacts")
	wantWorker := "implementation instruction\n\nREPORT_ARTIFACT_DIR: /tmp/artifacts\n" + priorArtifactReferenceMarker + "\n"
	if worker != wantWorker {
		t.Fatalf("worker artifact projection = %q want %q", worker, wantWorker)
	}
	if strings.Contains(worker, "--packet-check") {
		t.Fatalf("stable packet procedure leaked into runtime artifact projection: %s", worker)
	}

	reviewer := withReviewerArtifactContext("review instruction", "/tmp/artifacts")
	wantReviewer := "review instruction\n\nCURRENT_TASK_ARTIFACT_DIR: /tmp/artifacts\n" + priorArtifactReferenceMarker + "\n"
	if reviewer != wantReviewer {
		t.Fatalf("reviewer artifact projection = %q want %q", reviewer, wantReviewer)
	}
}

func TestProductionPromptsOwnStableArtifactProcedure(t *testing.T) {
	root := scenarioRepoRoot(t)
	worker, err := os.ReadFile(filepath.Join(root, "codex", "glm-worker", "prompts", "WORKER.md"))
	if err != nil {
		t.Fatal(err)
	}
	workerPrompt := string(worker)
	for _, want := range []string{"glm-worker --packet-check", "REPORT_ARTIFACT_DIR", "PRIOR_ARTIFACT_PATHS: reference-only"} {
		if !strings.Contains(workerPrompt, want) {
			t.Fatalf("production WORKER.mdにartifact contract marker %qがありません", want)
		}
	}

	reviewer, err := os.ReadFile(filepath.Join(root, "codex", "glm-worker", "prompts", "REVIEWER.md"))
	if err != nil {
		t.Fatal(err)
	}
	reviewerPrompt := string(reviewer)
	for _, want := range []string{"CURRENT_TASK_ARTIFACT_DIR", "PRIOR_ARTIFACT_PATHS: reference-only"} {
		if !strings.Contains(reviewerPrompt, want) {
			t.Fatalf("production REVIEWER.mdにartifact contract marker %qがありません", want)
		}
	}
	if strings.Contains(reviewerPrompt, "--packet-check") {
		t.Fatal("reviewer sessionはBashを持たないため、REVIEWER.mdへpre-check指示を配線してはいけません")
	}
}
