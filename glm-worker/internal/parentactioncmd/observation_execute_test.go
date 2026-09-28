package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/observationexec"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func newObservationExecuteTestState(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg, st := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	initObservationGitRepo(t, cfg.RepoRoot)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, state.ParentReviewProducer{Role: string(state.WorkerRole), Model: "opus"}); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func initObservationGitRepo(t *testing.T, repoRoot string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "observation@example.invalid"},
		{"config", "user.name", "observation test"},
	} {
		command := exec.Command("git", append([]string{"-C", repoRoot}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", repoRoot, "add", "tracked.txt")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	command = exec.Command("git", "-C", repoRoot, "commit", "-q", "-m", "initial")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}
}

func writeObservationGoModule(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, "go.mod"), []byte("module observation.test/repo\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "repo.go"), []byte("package repo\n\nfunc Value() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, "repo_test.go"), []byte("package repo\n\nimport \"testing\"\n\nfunc TestValue(t *testing.T) {\n\tif Value() != 2 {\n\t\tt.Fatal(\"unexpected\")\n\t}\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func stageObservationPayload(t *testing.T, cfg config.AppConfig, payload string) string {
	t.Helper()
	var stdout bytes.Buffer
	if err := prepare(cfg, []string{"prepare", actionObservationExecute}, &stdout); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Action string         `json:"action"`
		Token  string         `json:"token"`
		Path   string         `json:"path"`
		Slots  map[string]any `json:"slots"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	if prepared.Action != actionObservationExecute || prepared.Token == "" || prepared.Path == "" {
		t.Fatalf("prepared projection = %s", stdout.String())
	}
	if prepared.Slots == nil {
		t.Fatalf("prepare projectionにslotsがありません: %s", stdout.String())
	}
	if err := os.WriteFile(prepared.Path, []byte(tokenPayload(prepared.Token, payload)), 0o600); err != nil {
		t.Fatal(err)
	}
	return prepared.Token
}

func skipObservationExecuteWhenConfinementUnavailable(t *testing.T) {
	t.Helper()
	if err := observationexec.ConfinementPreflight(t.TempDir()); err != nil {
		t.Skipf("この実行環境ではconfinement初期化が拒否されるため隔離go test実行は親validation環境で実行します: %v", err)
	}
}

func tokenPayload(token string, payload string) string {
	return "GLM_PARENT_ACTION_TOKEN:" + token + "\n" + payload
}

func TestObservationExecuteRunsIsolatedGoTestAndRecordsTypedResult(t *testing.T) {
	skipObservationExecuteWhenConfinementUnavailable(t)
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	cfg, st := newObservationExecuteTestState(t)
	writeObservationGoModule(t, cfg.RepoRoot)
	token := stageObservationPayload(t, cfg, "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")

	var stdout bytes.Buffer
	if err := execute(cfg, []string{actionObservationExecute, token}, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output observationExecuteOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != "executed" || output.Result != "pass" || output.Operation != "go-test" || output.DecisionRound != 0 {
		t.Fatalf("output = %#v", output)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v err=%v", records, err)
	}
	record := records[0]
	if record.Status != state.ObservationExecutionStatusPass || record.Head == "" || record.IndexDigest == "" || record.WorktreeDigest == "" {
		t.Fatalf("record = %+v", record)
	}
	if len(record.Artifacts) != 1 || !strings.HasSuffix(record.Artifacts[0], "go-test.log") {
		t.Fatalf("artifacts = %+v", record.Artifacts)
	}
	if _, err := os.Stat(record.Artifacts[0]); err != nil {
		t.Fatalf("gate log artifactがありません: %v", err)
	}
}

func TestObservationExecuteRejectsDuplicateSameRoundRequest(t *testing.T) {
	skipObservationExecuteWhenConfinementUnavailable(t)
	if testing.Short() {
		t.Skip("隔離go testの実行をskipします")
	}
	cfg, st := newObservationExecuteTestState(t)
	writeObservationGoModule(t, cfg.RepoRoot)
	payload := "OPERATION: go-test\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n"
	first := stageObservationPayload(t, cfg, payload)
	if err := execute(cfg, []string{actionObservationExecute, first}, &bytes.Buffer{}, nil); err != nil {
		t.Fatal(err)
	}
	second := stageObservationPayload(t, cfg, payload)
	err := execute(cfg, []string{actionObservationExecute, second}, &bytes.Buffer{}, nil)
	if err == nil || !strings.Contains(err.Error(), "再実行") {
		t.Fatalf("同一round再実行が拒否されていません: %v", err)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("拒否時に記録が増えています: %+v err=%v", records, err)
	}
}

func TestObservationExecuteRejectsAdmissionOutsidePendingObservationDecision(t *testing.T) {
	cfg, st := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte("# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}

	var prepareStdout bytes.Buffer
	if err := prepare(cfg, []string{"prepare", actionObservationExecute}, &prepareStdout); err == nil {
		t.Fatal("pending decisionのない状態でprepareが受理されました")
	}
	if err := execute(cfg, []string{actionObservationExecute, "0123456789abcdef0123456789abcdef"}, &bytes.Buffer{}, nil); err == nil {
		t.Fatal("pending decisionのない状態でexecuteが受理されました")
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 0 {
		t.Fatalf("非admission境界で記録が残っています: %+v err=%v", records, err)
	}
}

func TestObservationExecuteRejectsUnknownOperationWithoutRecord(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	token := stageObservationPayload(t, cfg, "OPERATION: arbitrary-shell\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")
	err := execute(cfg, []string{actionObservationExecute, token}, &bytes.Buffer{}, nil)
	if err == nil {
		t.Fatal("閉集合外operationが受理されました")
	}
	records, errR := st.ObservationExecutions()
	if errR != nil || len(records) != 0 {
		t.Fatalf("拒否時に記録が残っています: %+v err=%v", records, errR)
	}
}

func TestObservationExecuteShadowEvalUsesMachineRouteWithoutSuccessPromotion(t *testing.T) {
	cfg, st := newObservationExecuteTestState(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	artifactsDir := st.ArtifactDir(taskID)
	if err := os.MkdirAll(filepath.Join(artifactsDir, "shadow-eval", "run1"), 0o700); err != nil {
		t.Fatal(err)
	}
	inputArtifact := filepath.Join(artifactsDir, "shadow-eval", "run1", "shadow-input.json")
	comparisonArtifact := filepath.Join(artifactsDir, "shadow-eval", "run1", "shadow-comparison.json")
	for _, artifact := range []string{inputArtifact, comparisonArtifact} {
		if err := os.WriteFile(artifact, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	shadowSuccess := fmt.Sprintf(`{"task_id":%q,"typed_schema_valid":true,"artifacts":{"input":%q,"comparison":%q}}`, taskID, inputArtifact, comparisonArtifact)
	shadowFailure := `{"task_id":"x","typed_schema_valid":false,"shadow_failure":{"kind":"provider","detail":"provider unavailable"}}`
	stub := writeObservationShadowEvalStub(t, shadowSuccess+"\n", "")
	failureStub := writeObservationShadowEvalStub(t, shadowFailure+"\n", "")

	original := resolveObservationShadowEvalWorker
	defer func() { resolveObservationShadowEvalWorker = original }()

	resolveObservationShadowEvalWorker = func() (string, error) { return stub, nil }
	token := stageObservationPayload(t, cfg, "OPERATION: shadow-eval\nREFERENCE: -\nWORKING_DIR: -\nDEADLINE_MS: default\n")
	var stdout bytes.Buffer
	if err := execute(cfg, []string{actionObservationExecute, token}, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output observationExecuteOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Operation != "shadow-eval" || output.Result != "pass" {
		t.Fatalf("output = %#v", output)
	}
	records, err := st.ObservationExecutions()
	if err != nil || len(records) != 1 {
		t.Fatalf("records = %+v err=%v", records, err)
	}
	if records[0].Status != state.ObservationExecutionStatusPass || len(records[0].Artifacts) != 2 {
		t.Fatalf("成功記録 = %+v", records[0])
	}

	resolveObservationShadowEvalWorker = func() (string, error) { return failureStub, nil }
	labelsDir := filepath.Join(artifactsDir, "labels")
	if err := os.MkdirAll(labelsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(labelsDir, "reference.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	failureToken := stageObservationPayload(t, cfg, "OPERATION: shadow-eval\nREFERENCE: labels/reference.json\nWORKING_DIR: -\nDEADLINE_MS: default\n")
	var failureStdout bytes.Buffer
	if err := execute(cfg, []string{actionObservationExecute, failureToken}, &failureStdout, nil); err != nil {
		t.Fatal(err)
	}
	records, err = st.ObservationExecutions()
	if err != nil || len(records) != 2 {
		t.Fatalf("failure records = %+v err=%v", records, err)
	}
	if records[1].Status != state.ObservationExecutionStatusFail || !strings.Contains(records[1].Detail, "provider unavailable") {
		t.Fatalf("失敗が成功へ昇格または詳細欠損: %+v", records[1])
	}
}

func TestObservationExecutePrepareRejectsNonObservationBoundary(t *testing.T) {
	cfg, _ := newParentActionIdentityTestState(t)
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)
	var stdout bytes.Buffer
	err := prepare(cfg, []string{"prepare", actionObservationExecute}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "pending Sol decision") {
		t.Fatalf("非observation境界でprepareが拒否されていません: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("拒否時にprojectionが出力されています: %s", stdout.String())
	}
}

func writeObservationShadowEvalStub(t *testing.T, stdoutPayload string, stderrPayload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "glm-worker")
	script := "#!/bin/sh\nprintf '%s' " + shellQuote(stdoutPayload)
	if stderrPayload != "" {
		script += "\nprintf '%s' " + shellQuote(stderrPayload) + " >&2"
	}
	script += "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
