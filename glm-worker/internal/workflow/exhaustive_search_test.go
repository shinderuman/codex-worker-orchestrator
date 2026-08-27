package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestExhaustiveRequirementUsesPrimaryTaskSectionsOnly(t *testing.T) {
	content := "# task\n\n## Contract\n\n- exhaustive needle inspection\n\n## Must not\n\n- unrelated prose\n"
	if !hasExhaustiveRequirement(taskExhaustiveRequirementText(content)) {
		t.Fatal("Contractのexhaustive要求を検出できません")
	}
	negative := "# task\n\n## Contract\n\n- normal inspection\n\n## Must not\n\n- do not claim exhaustive completion\n"
	if hasExhaustiveRequirement(taskExhaustiveRequirementText(negative)) {
		t.Fatal("Must notだけのexhaustive語を要求authorityにしてはいけません")
	}
}

func TestExecuteNewTaskInjectsWorkerAndIndependentReviewerExhaustiveProof(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	taskContent := activeTaskGuardSeed + "\n- exhaustive needle inspection\n"
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(taskContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "needle-target.txt"), []byte("needle implementation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, r, _, _ := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("exhaustive needle inspection"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("worker/reviewer calls=%d want=2", len(r.prompts))
	}
	for i, prompt := range r.prompts {
		for _, want := range []string{
			"EXHAUSTIVE_SEARCH_PROOF:",
			"MODE: full-corpus-deterministic",
			"PREDICATE: any-normalized-query-token-in-path-or-text",
			"MATCH: needle-target.txt",
			"BM25_TOP_N_AUTHORITY: none",
		} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("prompt %d missing %q:\n%s", i, want, prompt)
			}
		}
	}
	if !strings.Contains(r.prompts[1], "ROLE: reviewer") || !strings.Contains(r.prompts[1], "WORKER_EXHAUSTIVE_PROOF_AUTHORITY: none") {
		t.Fatalf("reviewer独立proof markerがありません:\n%s", r.prompts[1])
	}
}

func TestExecuteNewTaskDoesNotAddExhaustiveProofForNormalTask(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, _, _ := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("normal targeted change"); err != nil {
		t.Fatal(err)
	}
	for i, prompt := range r.prompts {
		if strings.Contains(prompt, "EXHAUSTIVE_SEARCH_PROOF") {
			t.Fatalf("normal prompt %dへexhaustive proofを追加しています:\n%s", i, prompt)
		}
	}
}

func TestExhaustiveSearchFailureStopsBeforeModelUse(t *testing.T) {
	root := t.TempDir()
	w := &Workflow{config: configForExhaustiveFailure(root), state: newStateStoreT(t), now: testNow}
	_, err := w.exhaustiveSearchContext("exhaustive needle", "", state.WorkerRole, 1)
	if err == nil || !strings.Contains(err.Error(), "exhaustive search proof failed before worker dispatch") {
		t.Fatalf("err=%v", err)
	}
}

func configForExhaustiveFailure(root string) config.AppConfig {
	return config.AppConfig{RepoRoot: root, RepoHash: "exhaustive-failure", StateBase: root}
}

func testNow() time.Time {
	return testFixedTime
}
