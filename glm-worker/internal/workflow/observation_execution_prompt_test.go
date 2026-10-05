package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestObservationWorkerPromptIncludesMachineExecutionRoute(t *testing.T) {
	for name, decl := range map[string]string{"poc": feasibilityPoCDecl, "observation": feasibilityObservationDecl} {
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writeFeasibilityActiveTask(t, repoRoot, decl)
			w, r, _, _ := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("observed")},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("PoC taskはworker 1呼出: %d", len(r.prompts))
			}
			prompt := r.prompts[0]
			if !strings.Contains(prompt, observationRouteMarker) {
				t.Fatalf("promptに機械実行routeの記述がありません:\n%s", prompt)
			}
			if !strings.Contains(prompt, "production implementation") || !strings.Contains(prompt, "read-only") {
				t.Fatalf("promptにread-only production実装不可の境界明記がありません:\n%s", prompt)
			}
			if strings.Contains(prompt, observationResultsBlockHeader()) {
				t.Fatalf("実行記録がない段階で結果blockが注入されています:\n%s", prompt)
			}
		})
	}
}

func TestObservationDecisionInjectsMachineExecutionResults(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityObservationDecl)
	w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("observed needs measurement")},
		{structured: implementedPacket("analyzed with machine results")},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision || !st.Exists("pending-decision") {
		t.Fatalf("waiting decision state = %s pending=%v", st.TaskStatus(), st.Exists("pending-decision"))
	}
	admission, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	comparisonArtifact := filepath.Join(st.ArtifactDir(taskID), "shadow-eval", "run1", "shadow-comparison.json")
	record := state.ObservationExecutionRecord{
		ExecutionID:        "exec-inject-0001",
		Operation:          "shadow-eval",
		ParamsDigest:       "digest-inject",
		Status:             state.ObservationExecutionStatusPass,
		Artifacts:          []string{comparisonArtifact},
		DecisionRound:      admission.Round,
		CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
	if err := seedObservationExecutionArtifact(t, record.Artifacts[0]); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendObservationExecution(record); err != nil {
		t.Fatal(err)
	}
	failedRecord := record
	failedRecord.ExecutionID = "exec-inject-0002"
	failedRecord.ParamsDigest = "digest-inject-fail"
	failedRecord.Status = state.ObservationExecutionStatusFail
	failedRecord.Detail = "deadline exceeded"
	failedRecord.Artifacts = nil
	if err := st.AppendObservationExecution(failedRecord); err != nil {
		t.Fatal(err)
	}

	if err := w.ExecuteDecision("機械実行結果を解析してGo/No-Go材料へ反映"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 2 {
		t.Fatalf("decision worker呼出 = %d want 2", len(r.prompts))
	}
	decisionPrompt := r.prompts[1]
	if !strings.Contains(decisionPrompt, observationResultsBlockHeader()) {
		t.Fatalf("decision promptに機械実行結果が注入されていません:\n%s", decisionPrompt)
	}
	if !strings.Contains(decisionPrompt, "exec-inject-0001") || !strings.Contains(decisionPrompt, "shadow-eval") {
		t.Fatalf("decision promptにtyped実行記録がありません:\n%s", decisionPrompt)
	}
	if !strings.Contains(decisionPrompt, "exec-inject-0002") || !strings.Contains(decisionPrompt, "status=fail detail=deadline exceeded") {
		t.Fatalf("decision promptの失敗記録が成功評価へ変換されています:\n%s", decisionPrompt)
	}
	if !strings.Contains(decisionPrompt, "machineが拒否") {
		t.Fatalf("decision promptに再実行loop抑止の記述がありません:\n%s", decisionPrompt)
	}
	if !strings.Contains(decisionPrompt, observationRouteMarker) {
		t.Fatalf("decision promptにroute blockがありません:\n%s", decisionPrompt)
	}
}

func seedObservationExecutionArtifact(t *testing.T, artifactPath string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(artifactPath, []byte("{}"), 0o600)
}

func observationResultsBlockHeader() string {
	return "\n" + observationResultsMarker + "\n"
}
