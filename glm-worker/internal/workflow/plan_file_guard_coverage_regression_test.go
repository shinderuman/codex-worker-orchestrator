package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestPlanGuardWorkerMutationKeepsExactTelemetryEvidence(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "worker-new", 0, mutatePlanFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	content, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile))
	if err != nil || string(content) != "glm edited plan\n" {
		t.Fatalf("GLMのplan変更がbaselineへ自動復元されています: %q %v", content, err)
	}

	taskViolations, mismatchEvents := 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == "parent_metadata_violation" {
			taskViolations++
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_mismatch" {
			mismatchEvents++
		}
	}
	if taskViolations != 1 || mismatchEvents != 1 {
		t.Fatalf("telemetry = task violation %d / mismatch event %d want 1/1", taskViolations, mismatchEvents)
	}
}

func TestTrackedPlanMissingKeepsStrongFailClosedPostconditions(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	gitIn(t, repoRoot, "add", implementationPlanFile)
	if err := os.Remove(filepath.Join(repoRoot, implementationPlanFile)); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("never")}}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("tracked plan欠損をmodel呼出前に停止できていません: calls=%d status=%q", len(r.prompts), st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("tracked plan欠損のfail closed後にresume checkpointが残っています")
	}
	if !strings.Contains(out.String(), "working treeへ存在しません") {
		t.Fatalf("tracked plan欠損理由がありません: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(repoRoot, implementationPlanFile)); !os.IsNotExist(err) {
		t.Fatalf("欠損planが復元・生成されています: %v", err)
	}
	assertMetadataMissingEventWithoutTaskCall(t, st)
}

func TestPlanTrackingIndeterminateKeepsStrongFailClosedPostconditions(t *testing.T) {
	repoRoot := initMutationRepo(t)
	if err := os.Remove(filepath.Join(repoRoot, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("never")}}, "", 0, nil)
	w.captureSnapshot = func(string) (state.GitSnapshot, error) { return fixedSnapshot, nil }

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("plan追跡判定不能をmodel呼出前に停止できていません: calls=%d status=%q", len(r.prompts), st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("plan追跡判定不能のfail closed後にresume checkpointが残っています")
	}
	if !strings.Contains(out.String(), "Git追跡判定に失敗") {
		t.Fatalf("plan追跡判定不能理由がありません: %s", out.String())
	}
	events := 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			t.Fatalf("model呼出前停止なのにtask記録があります: %+v", l)
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_unavailable" {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("parent_metadata_unavailable event = %d want 1", events)
	}
}

func TestHistoryGuardWorkerMutationKeepsPlanAndTelemetryEvidence(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "worker-new", 0, mutateHistoryFile)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requirePlanFileFailClosed(t, st, r, out, "内容が変化", 1)
	history, err := os.ReadFile(filepath.Join(repoRoot, implementationHistoryFile))
	if err != nil || string(history) != "glm edited history\n" {
		t.Fatalf("GLMのhistory変更がbaselineへ自動復元されています: %q %v", history, err)
	}
	plan, err := os.ReadFile(filepath.Join(repoRoot, implementationPlanFile))
	if err != nil || string(plan) != planGuardSeed {
		t.Fatalf("planまで変更されています: %q %v", plan, err)
	}

	taskViolations, mismatchEvents := 0, 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask && l.Outcome == "parent_metadata_violation" {
			taskViolations++
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_mismatch" {
			mismatchEvents++
		}
	}
	if taskViolations != 1 || mismatchEvents != 1 {
		t.Fatalf("history telemetry = task violation %d / mismatch event %d want 1/1", taskViolations, mismatchEvents)
	}
}

func TestTrackedHistoryMissingKeepsStrongFailClosedPostconditions(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writePlanFileContent(t, repoRoot, planGuardSeed)
	writeHistoryFileContent(t, repoRoot, historyGuardSeed)
	gitIn(t, repoRoot, "add", implementationHistoryFile)
	if err := os.Remove(filepath.Join(repoRoot, implementationHistoryFile)); err != nil {
		t.Fatal(err)
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("never")}}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 || st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("tracked history欠損をmodel呼出前に停止できていません: calls=%d status=%q", len(r.prompts), st.TaskStatus())
	}
	if _, err := st.LoadResumeCheckpoint(); err == nil {
		t.Fatal("tracked history欠損のfail closed後にresume checkpointが残っています")
	}
	if !strings.Contains(out.String(), implementationHistoryFile+"がworking treeへ存在しません") {
		t.Fatalf("tracked history欠損理由がありません: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(repoRoot, implementationHistoryFile)); !os.IsNotExist(err) {
		t.Fatalf("欠損historyが復元・生成されています: %v", err)
	}
	assertMetadataMissingEventWithoutTaskCall(t, st)
}

func TestReviewerMetadataMutationStillUsesReviewSnapshotInvariant(t *testing.T) {
	cases := []struct {
		name string
		seed func(*testing.T, string)
		mutate func(string) error
	}{
		{"plan", func(t *testing.T, root string) { writePlanFileContent(t, root, planGuardSeed) }, mutatePlanFile},
		{"history", func(t *testing.T, root string) {
			writePlanFileContent(t, root, planGuardSeed)
			writeHistoryFileContent(t, root, historyGuardSeed)
		}, mutateHistoryFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			tc.seed(t, repoRoot)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{{structured: implementedPacket("done")}, {structured: passPacket()}}, "", 0, tc.mutate)
			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 2 || st.TaskStatus() != state.TaskStatusWaitingSolReview || !strings.Contains(out.String(), "reviewer実行中にrepository状態が変化") {
				t.Fatalf("review snapshot invariantで停止できていません: calls=%d status=%q out=%s", len(r.prompts), st.TaskStatus(), out.String())
			}
			for _, l := range taskLogs(t, st) {
				if l.Outcome == "parent_metadata_mismatch" || l.Outcome == "parent_metadata_violation" {
					t.Fatalf("reviewer経路へparent metadata guardを誤適用しています: %+v", l)
				}
			}
		})
	}
}

func TestPlanGuardManifestKeepsTrackedMissingScenario(t *testing.T) {
	sc, mf := loadCorpus(t)
	trackedAbsent := ""
	for _, s := range sc.Scenarios {
		if s.PlanFileTrackedAbsent {
			trackedAbsent = s.ID
			break
		}
	}
	if trackedAbsent == "" {
		t.Fatal("escaped corpusに追跡中plan欠損scenarioがありません")
	}
	for _, e := range mf.InstructionFiles {
		if e.Path != "AGENTS.md" {
			continue
		}
		for _, sid := range e.Scenarios {
			if sid == trackedAbsent {
				return
			}
		}
	}
	t.Fatalf("manifestのAGENTS.mdが%sをpinしていません", trackedAbsent)
}

func assertMetadataMissingEventWithoutTaskCall(t *testing.T, st *state.StateStore) {
	t.Helper()
	events := 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			t.Fatalf("model呼出前停止なのにtask記録があります: %+v", l)
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == "parent_metadata_missing" {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("parent_metadata_missing event = %d want 1", events)
	}
}
