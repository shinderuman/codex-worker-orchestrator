package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeArtifactContextCarriesOnlyDynamicProjection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		actual     string
		instruction string
		selector   string
	}{
		{
			name: "worker", actual: withArtifactContext("implementation instruction", "/tmp/artifacts"),
			instruction: "implementation instruction", selector: "REPORT_ARTIFACT_DIR",
		},
		{
			name: "reviewer", actual: withReviewerArtifactContext("review instruction", "/tmp/artifacts"),
			instruction: "review instruction", selector: "CURRENT_TASK_ARTIFACT_DIR",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !strings.Contains(tc.actual, tc.instruction) {
				t.Fatalf("original instruction was lost: %q", tc.actual)
			}
			if strings.Count(tc.actual, tc.selector+": /tmp/artifacts") != 1 {
				t.Fatalf("artifact directory is not projected exactly once: %q", tc.actual)
			}
			if strings.Count(tc.actual, priorArtifactReferenceMarker) != 1 {
				t.Fatalf("prior artifact reference policy is not projected exactly once: %q", tc.actual)
			}
			if strings.Contains(tc.actual, "--packet-check") {
				t.Fatalf("stable packet procedure leaked into dynamic runtime projection: %q", tc.actual)
			}
		})
	}
}

func TestProductionPromptsOwnStableArtifactProcedure(t *testing.T) {
	t.Parallel()
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
