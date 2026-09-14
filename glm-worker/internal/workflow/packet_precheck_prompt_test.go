package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerDispatchRoutesPacketPrecheck(t *testing.T) {
	prompt := withArtifactContext("implementation instruction", "/tmp/artifacts")
	for _, want := range []string{"glm-worker --packet-check", "--artifact-root", "同じcall内で修正", "Bashを利用できない"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("worker dispatchのartifact contextに%qがありません: %s", want, prompt)
		}
	}
	if reviewer := withReviewerArtifactContext("review instruction", "/tmp/artifacts"); strings.Contains(reviewer, "--packet-check") {
		t.Fatal("reviewerはread-onlyでBashを持たないため、pre-check指示を配線してはいけません")
	}
}

func TestResultCorrectionPromptDelegatesPacketGrammar(t *testing.T) {
	correction := resultCorrectionPrompt("validator rejected packet")
	for _, want := range []string{"structured schemaとvalidatorを正として", "validator rejected packet", rejectedArtifactMarker} {
		if !strings.Contains(correction, want) {
			t.Fatalf("結果修正promptにruntime context %qがありません: %s", want, correction)
		}
	}
	for _, forbidden := range []string{"glm-worker --packet-check", "6 KiB", "1536 bytes", "STATUSに応じた必須field"} {
		if strings.Contains(correction, forbidden) {
			t.Fatalf("結果修正promptがpacket grammar %qを再定義しています: %s", forbidden, correction)
		}
	}
}

func TestProductionWorkerPromptRoutesPacketPrecheck(t *testing.T) {
	root := scenarioRepoRoot(t)
	worker, err := os.ReadFile(filepath.Join(root, "codex", "glm-worker", "prompts", "WORKER.md"))
	if err != nil {
		t.Fatal(err)
	}
	workerPrompt := string(worker)
	for _, want := range []string{"glm-worker --packet-check", "提出前検証"} {
		if !strings.Contains(workerPrompt, want) {
			t.Fatalf("production WORKER.mdに%qがありません", want)
		}
	}
	reviewer, err := os.ReadFile(filepath.Join(root, "codex", "glm-worker", "prompts", "REVIEWER.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(reviewer), "--packet-check") {
		t.Fatal("reviewer sessionはBashを持たないため、REVIEWER.mdへpre-check指示を配線してはいけません")
	}
}
