package workflow

import (
	"errors"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func parentOutcomeTotal(stats state.TaskStats) int {
	total := 0
	for _, value := range stats.ParentOutcomes {
		total += value
	}
	return total
}

func TestWorkerDecisionOpportunityCarriesProducer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: needsSolDecisionPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	stats := currentStats(t, st)
	if stats.ParentReviewOpen == nil {
		t.Fatalf("NEEDS_SOL_DECISION後にopportunityがopenではありません: %#v", stats)
	}
	open := stats.ParentReviewOpen
	if open.PacketStatus != "NEEDS_SOL_DECISION" || open.Role != "worker" || open.ModelAlias != "opus" || open.Risk != "HIGH" {
		t.Fatalf("worker decision packetのproducer対応付け = %#v", open)
	}
	if parentOutcomeTotal(stats) != 0 {
		t.Fatalf("decision前にoutcomeが計上されています: %#v", stats.ParentOutcomes)
	}
}

func TestReviewerOpportunityCarriesProducer(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: needsSolReviewPacket()},
	}}
	w := newWorkflowT(t, st, r)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	stats := currentStats(t, st)
	open := stats.ParentReviewOpen
	if open == nil || open.PacketStatus != "NEEDS_SOL_REVIEW" || open.Role != "reviewer" || open.ModelAlias != "haiku" || open.Risk != "HIGH" {
		t.Fatalf("reviewer packetのproducer対応付け = %#v", open)
	}
}

func TestAutoFixRoundsDoNotRecordParentOutcome(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fix")},
		{structured: fixRequiredPacket()},
	}}
	w := newWorkflowT(t, st, r)
	w.config.MaxAutoFixRounds = 1

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	stats := currentStats(t, st)
	if stats.AutoFixRounds != 1 || stats.NeedsSolReviewPackets != 1 {
		t.Fatalf("auto-fix収束不能の計数 = %#v", stats)
	}
	if stats.ParentReviewOpen == nil || stats.ParentReviewOpen.PacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("収束不能terminalがopportunityとしてopenではありません: %#v", stats.ParentReviewOpen)
	}
	if parentOutcomeTotal(stats) != 0 {
		t.Fatalf("reviewer auto-fixはparent outcomeを作りません: %#v", stats.ParentOutcomes)
	}
}

func TestExplicitFixRecordsOutcomeOnceDespiteReexecution(t *testing.T) {
	st := newStateStoreT(t)
	setup := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("done")},
		{structured: fixRequiredPacket()},
		{structured: implementedPacket("fix")},
		{structured: fixRequiredPacket()},
	}}
	w := newWorkflowT(t, st, setup)
	w.config.MaxAutoFixRounds = 1
	var workerErr *WorkerError
	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}

	failed := &scriptedRunner{steps: []runnerStep{{runErr: errors.New("boom")}}}
	wf := newWorkflowT(t, st, failed)
	// reviewer terminal resultの非収束指摘をそのまま差し戻すためglm-reviewer。
	err := wf.ExecuteExplicitFix("境界値を修正する", state.ParentOriginGLMReviewer)
	if err == nil || !errors.As(err, &workerErr) {
		t.Fatalf("fix実行中の失敗を伝播する必要があります: %v", err)
	}
	stats := currentStats(t, st)
	if stats.ParentOutcomes[state.ParentOutcomeFix] != 1 || stats.ParentFixOrigins[state.ParentOriginGLMReviewer] != 1 {
		t.Fatalf("fix outcome = %#v origins=%#v", stats.ParentOutcomes, stats.ParentFixOrigins)
	}
	if stats.FixCommands != 1 || stats.ParentReviewOpen != nil {
		t.Fatalf("fix後のstate: commands=%d open=%#v", stats.FixCommands, stats.ParentReviewOpen)
	}

	retry := &scriptedRunner{steps: []runnerStep{{runErr: errors.New("boom again")}}}
	wf2 := newWorkflowT(t, st, retry)
	err = wf2.ExecuteExplicitFix("境界値を修正する", state.ParentOriginGLMReviewer)
	if err == nil || !strings.Contains(err.Error(), "--fix is only available after NEEDS_SOL_REVIEW") {
		t.Fatalf("status activeでの同一fix再実行はgateで拒否: %v", err)
	}
	stats = currentStats(t, st)
	if stats.ParentOutcomes[state.ParentOutcomeFix] != 1 || stats.FixCommands != 1 {
		t.Fatalf("再実行で二重計上されています: outcomes=%#v commands=%d", stats.ParentOutcomes, stats.FixCommands)
	}
}

func TestRateLimitStopAndResumeDoNotRecordParentOutcome(t *testing.T) {
	st := newStateStoreT(t)
	stopped := &scriptedRunner{steps: []runnerStep{{
		output: zaiFiveHourLog,
		runErr: errors.New("exit status 1"),
	}}}
	w := newWorkflowT(t, st, stopped)
	w.config.RepoRoot = "/repo"
	w.config.RepoShort = "testrepo1234"
	var limitErr runner.ZaiRateLimitError
	if err := w.ExecuteNewTask("request"); err == nil || !errors.As(err, &limitErr) {
		t.Fatalf("rate limit errorを期待: %v", err)
	}
	stats := currentStats(t, st)
	if parentOutcomeTotal(stats) != 0 || stats.ParentReviewOpen != nil {
		t.Fatalf("rate limit停止はparent outcomeを作りません: outcomes=%#v open=%#v", stats.ParentOutcomes, stats.ParentReviewOpen)
	}

	resumed := &scriptedRunner{steps: []runnerStep{
		{structured: implementedPacket("resumed")},
		{structured: passPacket()},
	}}
	wf := newWorkflowT(t, st, resumed)
	wf.config.RepoRoot = "/repo"
	if err := wf.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	stats = currentStats(t, st)
	if stats.ResumeCommands != 1 {
		t.Fatalf("resume計数 = %#v", stats)
	}
	if parentOutcomeTotal(stats) != 0 {
		t.Fatalf("resume自体はoutcomeを確定しません: %#v", stats.ParentOutcomes)
	}
	if stats.ParentReviewOpen == nil || stats.ParentReviewOpen.PacketStatus != "PASS" {
		t.Fatalf("resume後のterminal packetだけがopportunityを開きます: %#v", stats.ParentReviewOpen)
	}
	if stats.PassPackets != 1 || outcomePlusOpen(stats) != 1 {
		t.Fatalf("resume再開での二重計上: packets=%d outcomes+open=%d", stats.PassPackets, outcomePlusOpen(stats))
	}
}

func outcomePlusOpen(stats state.TaskStats) int {
	total := parentOutcomeTotal(stats)
	if stats.ParentReviewOpen != nil {
		total++
	}
	return total
}
