package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

const observationExecutionDeclaration = "# observation\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n"

func newObservationExecutionStore(t *testing.T, declaration string) *StateStore {
	t.Helper()
	st := newParentActionTestStore(t)
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/observation.md", []byte(declaration)); err != nil {
		t.Fatal(err)
	}
	return st
}

func admitObservationDecisionBoundary(t *testing.T, st *StateStore) {
	t.Helper()
	if err := st.WaitForDecision(); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusNeedsSolDecision, Risk: packet.RiskHigh}, ParentReviewProducer{Role: string(WorkerRole), Model: "opus"}); err != nil {
		t.Fatal(err)
	}
}

func TestObservationExecuteAdmissionRequiresPendingObservationDecision(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	if _, err := st.ObservationExecuteAdmission(); err == nil {
		t.Fatal("pending decisionがない状態でadmitされました")
	}
	admitObservationDecisionBoundary(t, st)
	admission, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatalf("pending observation decisionがadmitされません: %v", err)
	}
	if admission.TaskID == "" || admission.Round <= 0 {
		t.Fatalf("admission = %+v", admission)
	}
	epoch, err := st.ParentEvidenceLeaseEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if int64(admission.Round) != epoch {
		t.Fatalf("admission round = %d want canonical lease %d", admission.Round, epoch)
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Allows(ParentActionObservationExecute) {
		t.Fatalf("waiting-decision planにobservation-executeがありません: %#v", plan)
	}
}

func TestObservationExecuteAdmissionRejectsImplementationTask(t *testing.T) {
	st := newObservationExecutionStore(t, "# active\n\n## External feasibility\n\nstatus: implementation\nassumption: a\nevidence-source: producer\nevidence: e\ngo: g\n")
	admitObservationDecisionBoundary(t, st)
	if _, err := st.ObservationExecuteAdmission(); err == nil {
		t.Fatal("implementation taskでobservation-executeがadmitされました")
	}
	plan, err := st.ParentActionPlan()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Allows(ParentActionObservationExecute) {
		t.Fatalf("implementation decision planにobservation-executeがあります: %#v", plan)
	}
}

func TestObservationExecuteAdmissionDoesNotDependOnTaskStats(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	admitObservationDecisionBoundary(t, st)
	expected, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Remove(currentStatsFile); err != nil {
		t.Fatal(err)
	}
	missingStats, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatalf("missing TaskStats invalidated canonical observation admission: %v", err)
	}
	if missingStats != expected {
		t.Fatalf("missing TaskStats changed admission: got=%+v want=%+v", missingStats, expected)
	}
	if err := st.Write(currentStatsFile, "{not-json"); err != nil {
		t.Fatal(err)
	}
	corruptStats, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatalf("corrupt TaskStats invalidated canonical observation admission: %v", err)
	}
	if corruptStats != expected {
		t.Fatalf("corrupt TaskStats changed admission: got=%+v want=%+v", corruptStats, expected)
	}
}

func TestObservationExecuteAdmissionFailsClosedWithoutCanonicalDecisionIdentity(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	admitObservationDecisionBoundary(t, st)
	if err := st.Remove(parentEvidenceLeasePath); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ObservationExecuteAdmission(); err == nil {
		t.Fatal("missing canonical decision identity was admitted")
	}
}

func TestObservationExecutionRecordsRoundScopedAndBounded(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	rounds := make([]int, 0, 3)
	digests := make([]string, 0, 3)
	for index := 0; index < 3; index++ {
		admitObservationDecisionBoundary(t, st)
		admission, err := st.ObservationExecuteAdmission()
		if err != nil {
			t.Fatal(err)
		}
		rounds = append(rounds, admission.Round)
		record := observationExecutionRecordFixture("exec-"+string(rune('a'+index)), admission.Round)
		digests = append(digests, record.ParamsDigest)
		if err := st.AppendObservationExecution(record); err != nil {
			t.Fatal(err)
		}
		if _, err := st.BeginParentDecision(); err != nil {
			t.Fatal(err)
		}
		if err := st.CommitParentActionBegin(); err != nil {
			t.Fatal(err)
		}
	}
	if rounds[0] >= rounds[1] || rounds[1] >= rounds[2] {
		t.Fatalf("canonical decision rounds did not advance: %v", rounds)
	}
	exists, err := st.HasObservationExecution("shadow-eval", digests[0], rounds[0])
	if err != nil || !exists {
		t.Fatalf("first roundの記録が見つかりません: %v", err)
	}
	exists, err = st.HasObservationExecution("shadow-eval", digests[0], rounds[2])
	if err != nil || exists {
		t.Fatalf("later roundにfirst roundの記録が混入しています: %v", err)
	}
	records, err := st.ObservationExecutionsForRound(rounds[1])
	if err != nil || len(records) != 1 || records[0].ExecutionID != "exec-b" {
		t.Fatalf("second roundの記録抽出が不正です: %+v %v", records, err)
	}
}

func TestObservationExecutionRoundAdvancesWhenTaskStatsWriteIsLost(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	admitObservationDecisionBoundary(t, st)
	first, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatal(err)
	}
	record := observationExecutionRecordFixture("exec-first", first.Round)
	record.ParamsDigest = "shared-digest"
	if err := st.AppendObservationExecution(record); err != nil {
		t.Fatal(err)
	}

	failWritesFor(t, st, currentStatsFile)
	if _, err := st.BeginParentDecision(); err != nil {
		t.Fatalf("best-effort TaskStats write loss made canonical decision fatal: %v", err)
	}
	if err := st.CommitParentActionBegin(); err != nil {
		t.Fatal(err)
	}
	admitObservationDecisionBoundary(t, st)
	second, err := st.ObservationExecuteAdmission()
	if err != nil {
		t.Fatalf("later canonical decision was not admitted after TaskStats write loss: %v", err)
	}
	if second.Round <= first.Round {
		t.Fatalf("canonical decision round did not advance: first=%d second=%d", first.Round, second.Round)
	}
	exists, err := st.HasObservationExecution(record.Operation, record.ParamsDigest, second.Round)
	if err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("prior-round execution suppressed the same operation in a later canonical decision round")
	}
}

func TestAppendObservationExecutionBoundsRecordRetention(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	for index := 0; index < observationExecutionsRetention+4; index++ {
		record := observationExecutionRecordFixture(retentionExecutionID(index), 0)
		if err := st.AppendObservationExecution(record); err != nil {
			t.Fatal(err)
		}
	}
	records, err := st.ObservationExecutions()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != observationExecutionsRetention {
		t.Fatalf("記録数 = %d want %d", len(records), observationExecutionsRetention)
	}
	if records[0].ExecutionID != retentionExecutionID(4) {
		t.Fatalf("最古の記録が残っています: %s", records[0].ExecutionID)
	}
}

func TestAppendObservationExecutionRejectsArtifactsOutsideTaskArtifactDir(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	outside := filepath.Join(t.TempDir(), "outside.log")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	record := observationExecutionRecordFixture("exec-outside", 0)
	record.Artifacts = []string{outside}
	if err := st.AppendObservationExecution(record); err == nil {
		t.Fatal("task artifact dirの外のartifactが受理されました")
	}
	records, err := st.ObservationExecutions()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatal("拒否時に記録が保存されています")
	}

	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	boundedDir := filepath.Join(st.ArtifactDir(taskID), "observation-exec", "exec-inside")
	if err := os.MkdirAll(boundedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	bounded := filepath.Join(boundedDir, "go-test.log")
	if err := os.WriteFile(bounded, []byte("log"), 0o600); err != nil {
		t.Fatal(err)
	}
	record.Artifacts = []string{bounded}
	if err := st.AppendObservationExecution(record); err != nil {
		t.Fatalf("境界内artifactが拒否されました: %v", err)
	}
}

func TestAppendObservationExecutionRejectsInvalidRecords(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	invalid := observationExecutionRecordFixture("exec-invalid", 0)
	invalid.Status = "success"
	if err := st.AppendObservationExecution(invalid); err == nil {
		t.Fatal("pass/fail以外のstatusが受理されました")
	}
	missing := observationExecutionRecordFixture("exec-invalid", 0)
	missing.ParamsDigest = ""
	if err := st.AppendObservationExecution(missing); err == nil {
		t.Fatal("必須field欠損の記録が受理されました")
	}
}

func TestObservationExecutionsFailClosedOnCorruptState(t *testing.T) {
	st := newObservationExecutionStore(t, observationExecutionDeclaration)
	if err := st.Write(observationExecutionsStateFile, "{not-json"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ObservationExecutions(); err == nil {
		t.Fatal("破損stateが黙って空扱いされました")
	}
	record := observationExecutionRecordFixture("exec-corrupt", 0)
	if err := st.AppendObservationExecution(record); err == nil {
		t.Fatal("破損stateへの追記が受理されました")
	}
}

func observationExecutionRecordFixture(executionID string, round int) ObservationExecutionRecord {
	return ObservationExecutionRecord{
		ExecutionID:        executionID,
		Operation:          "shadow-eval",
		ParamsDigest:       "digest-" + executionID,
		Status:             ObservationExecutionStatusPass,
		DecisionRound:      round,
		CompletedAtRFC3339: time.Now().UTC().Format(time.RFC3339),
	}
}

func retentionExecutionID(index int) string {
	return "exec-retention-" + string(rune('0'+index%10)) + "-" + string(rune('a'+index/10))
}
