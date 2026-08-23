package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

func recordPacket(st *StateStore, status packet.Status, risk packet.Risk, producer ParentReviewProducer) {
	st.RecordSolResult(packet.Result{Status: status, Risk: risk}, producer)
}

func TestRecordSolResultOpensParentReviewOpportunity(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}

	recordPacket(st, packet.StatusNeedsSolReview, packet.RiskHigh, ParentReviewProducer{Role: "reviewer", Model: "sonnet"})

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentReviewOpen == nil {
		t.Fatalf("open opportunityが記録されていません: %#v", stats)
	}
	if stats.ParentReviewOpen.PacketStatus != string(packet.StatusNeedsSolReview) ||
		stats.ParentReviewOpen.Role != "reviewer" ||
		stats.ParentReviewOpen.ModelAlias != "sonnet" ||
		stats.ParentReviewOpen.Risk != string(packet.RiskHigh) {
		t.Fatalf("open opportunity = %#v", stats.ParentReviewOpen)
	}
	if len(stats.ParentOutcomes) != 0 {
		t.Fatalf("opportunity open時点でoutcomeが計上されています: %#v", stats.ParentOutcomes)
	}
	if st.OpenParentReviewLabel() != string(packet.StatusNeedsSolReview) {
		t.Fatalf("open label = %q", st.OpenParentReviewLabel())
	}
}

func TestRecordParentOutcomeResolvesOncePerOpportunity(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusNeedsSolReview, packet.RiskLow, ParentReviewProducer{Role: "reviewer", Model: "haiku"})

	resolved, err := st.RecordParentOutcome(ParentOutcomeFix, ParentOriginCodexReview)
	if err != nil || !resolved {
		t.Fatalf("fix outcome確定失敗: resolved=%v err=%v", resolved, err)
	}
	resolved, err = st.RecordParentOutcome(ParentOutcomeFix, ParentOriginCodexReview)
	if err != nil || resolved {
		t.Fatalf("同一opportunityの再確定が二重計上されました: resolved=%v err=%v", resolved, err)
	}

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeFix] != 1 || stats.ParentFixOrigins[ParentOriginCodexReview] != 1 {
		t.Fatalf("outcome集計 = outcomes:%#v origins:%#v", stats.ParentOutcomes, stats.ParentFixOrigins)
	}
	if stats.ParentOutcomesByModel["haiku"] != 1 || stats.ParentOutcomesByRisk["LOW"] != 1 {
		t.Fatalf("model/risk別集計 = %#v / %#v", stats.ParentOutcomesByModel, stats.ParentOutcomesByRisk)
	}
	if stats.ParentReviewOpen != nil {
		t.Fatalf("確定後もopenが残っています: %#v", stats.ParentReviewOpen)
	}
	if stats.FixCommands != 0 {
		t.Fatalf("parent outcome確定はfix_commandsへ影響しません: %#v", stats)
	}
}

func TestRecordParentOutcomeDefaultsUndeclaredOriginToUnknown(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusNeedsSolReview, packet.RiskLow, ParentReviewProducer{})

	if _, err := st.RecordParentOutcome(ParentOutcomeFix, ""); err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentFixOrigins[ParentOriginUnknown] != 1 || len(stats.ParentFixOrigins) != 1 {
		t.Fatalf("未宣言origin = %#v", stats.ParentFixOrigins)
	}
	if stats.ParentOutcomesByModel[ParentOriginUnknown] != 1 {
		t.Fatalf("producer未観測modelはunknownへ倒す: %#v", stats.ParentOutcomesByModel)
	}
	if stats.ParentOutcomesByRisk[string(packet.RiskLow)] != 1 {
		t.Fatalf("riskはpacket値をそのまま使う: %#v", stats.ParentOutcomesByRisk)
	}
}

func TestRecordParentOutcomeRejectsInvalidKindAndOrigin(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusNeedsSolReview, packet.RiskLow, ParentReviewProducer{})

	if _, err := st.RecordParentOutcome("adopted", ""); err == nil {
		t.Fatal("集合外outcome kindを拒否する必要があります")
	}
	if _, err := st.RecordParentOutcome(ParentOutcomeFix, "vibe"); err == nil {
		t.Fatal("集合外originを拒否する必要があります")
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.ParentOutcomes) != 0 || stats.ParentReviewOpen == nil {
		t.Fatalf("拒否時はstateを変更しません: %#v", stats)
	}
}

func TestRecordParentOutcomeAcceptRejectsDecisionPacket(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusNeedsSolDecision, packet.RiskLow, ParentReviewProducer{})

	if _, err := st.RecordParentOutcome(ParentOutcomeAccepted, ""); err == nil {
		t.Fatal("decision packetへの--acceptを拒否する必要があります")
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.ParentOutcomes) != 0 || stats.ParentReviewOpen == nil {
		t.Fatalf("拒否時はstateを変更しません: %#v", stats)
	}
}

func TestArchiveClosesOpenParentReviewAsUnknown(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	firstTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusPass, packet.RiskLow, ParentReviewProducer{Role: "reviewer", Model: "haiku"})

	secondTask, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if secondTask == firstTask {
		t.Fatalf("task IDが切り替わっていません: %s", firstTask)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	var archived TaskStats
	for _, stats := range all {
		if stats.TaskID == firstTask {
			archived = stats
		}
	}
	if archived.TaskID != firstTask {
		t.Fatalf("archived statsが見つかりません: %#v", all)
	}
	if archived.ParentOutcomes[ParentOutcomeUnknown] != 1 || archived.ParentOutcomes[ParentOutcomeAccepted] != 0 {
		t.Fatalf("close時のunknown確定 = %#v", archived.ParentOutcomes)
	}
	if archived.ParentReviewOpen != nil {
		t.Fatalf("archived statsにopenが残っています: %#v", archived.ParentReviewOpen)
	}

	current, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if current.ParentReviewOpen != nil || len(current.ParentOutcomes) != 0 {
		t.Fatalf("新taskへ旧taskの観測が持ち越されています: %#v", current)
	}
}

func TestSupersededOpenOpportunityCountsUnknown(t *testing.T) {
	st := &StateStore{dir: t.TempDir()}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	recordPacket(st, packet.StatusNeedsSolReview, packet.RiskLow, ParentReviewProducer{Role: "reviewer", Model: "haiku"})
	recordPacket(st, packet.StatusPass, packet.RiskHigh, ParentReviewProducer{Role: "reviewer", Model: "sonnet"})

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[ParentOutcomeUnknown] != 1 {
		t.Fatalf("上書きされたopportunityがunknown確定されていません: %#v", stats.ParentOutcomes)
	}
	if stats.ParentReviewOpen.PacketStatus != string(packet.StatusPass) {
		t.Fatalf("最新packetがopenになっていません: %#v", stats.ParentReviewOpen)
	}
	if stats.NeedsSolReviewPackets+stats.PassPackets != 2 || outcomeTotal(stats) != 1 {
		t.Fatalf("opportunity総数とoutcome総数の整合: packets=%d outcomes=%d", stats.NeedsSolReviewPackets+stats.PassPackets, outcomeTotal(stats))
	}
	if stats.ParentOutcomesByModel["haiku"] != 1 || len(stats.ParentOutcomesByModel) != 1 {
		t.Fatalf("supersede確定のmodel帰属 = %#v", stats.ParentOutcomesByModel)
	}
	if stats.ParentOutcomesByRisk[string(packet.RiskLow)] != 1 || len(stats.ParentOutcomesByRisk) != 1 {
		t.Fatalf("supersede確定のrisk帰属 = %#v", stats.ParentOutcomesByRisk)
	}
	if intMapTotal(stats.ParentOutcomesByModel) != outcomeTotal(stats) || intMapTotal(stats.ParentOutcomesByRisk) != outcomeTotal(stats) {
		t.Fatalf("内訳集計の総和がoutcome総数と不一致: model=%#v risk=%#v outcomes=%#v", stats.ParentOutcomesByModel, stats.ParentOutcomesByRisk, stats.ParentOutcomes)
	}
}

func intMapTotal(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func outcomeTotal(stats TaskStats) int {
	total := 0
	for _, value := range stats.ParentOutcomes {
		total += value
	}
	return total
}

func TestOldTaskStatsArchiveStaysUnknownWithoutBackfill(t *testing.T) {
	dir := t.TempDir()
	st := &StateStore{dir: dir}
	legacy := `{
  "version": 3,
  "task_id": "legacy-task",
  "started_at": "2026-08-01T00:00:00Z",
  "status": "complete",
  "model_calls": 2,
  "pass_packets": 1,
  "needs_sol_review_packets": 1,
  "decision_commands": 0,
  "fix_commands": 1,
  "resume_commands": 0,
  "auto_fix_rounds": 0,
  "needs_sol_decision_packets": 0,
  "rate_limits": 0,
  "packet_compactions": 0,
  "sol_packet_bytes": 100,
  "provider_unavailable": 0
}
`
	historyDir := filepath.Join(dir, "stats")
	if err := os.MkdirAll(historyDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(historyDir, "legacy-task.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].TaskID != "legacy-task" {
		t.Fatalf("legacy archiveが読めていません: %#v", all)
	}
	if outcomeTotal(all[0]) != 0 || all[0].ParentReviewOpen != nil {
		t.Fatalf("旧archiveへoutcomeを補完していません: %#v", all[0])
	}
	if all[0].PassPackets != 1 || all[0].FixCommands != 1 {
		t.Fatalf("旧archiveの既存計数が変わっています: %#v", all[0])
	}
}

func TestComputeParentReworkSegmentsByOrigin(t *testing.T) {
	dir := t.TempDir()
	st := &StateStore{dir: dir}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	taskCall := func(role SessionRole, model string, turns int, input int64, wall int64) ModelCallLog {
		return ModelCallLog{
			Version:        modelCallLogVersion,
			CallType:       CallTypeTask,
			TaskID:         taskID,
			Phase:          "phase-" + model,
			Role:           role,
			ModelAlias:     model,
			StartedAt:      now,
			CompletedAt:    now,
			Outcome:        "success",
			TopLevelUsage:  TokenUsage{InputTokens: input, OutputTokens: input / 2},
			TopLevelTurns:  turns,
			WallDurationMS: wall,
		}
	}
	parentEvent := func(phase string, outcome string, origin string) ModelCallLog {
		return ModelCallLog{
			Version:      modelCallLogVersion,
			CallType:     CallTypeEvent,
			TaskID:       taskID,
			Phase:        phase,
			Outcome:      outcome,
			ParentOrigin: origin,
			StartedAt:    now,
			CompletedAt:  now,
		}
	}
	records := []ModelCallLog{
		taskCall(WorkerRole, "opus", 3, 100, 5000),
		taskCall(ReviewerRole, "haiku", 1, 50, 1000),
		parentEvent(ParentPhaseFix, ParentOutcomeFix, ParentOriginCodexReview),
		taskCall(WorkerRole, "opus", 4, 200, 8000),
		taskCall(ReviewerRole, "sonnet", 2, 80, 2000),
		parentEvent(ParentPhaseFix, ParentOutcomeFix, ParentOriginUserAmendment),
		taskCall(WorkerRole, "opus", 1, 10, 500),
		parentEvent(ParentPhaseAccept, ParentOutcomeAccepted, ""),
		taskCall(WorkerRole, "opus", 9, 999, 9900),
	}
	path := st.ModelCallLogPath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	var lines []byte
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, data...)
		lines = append(lines, '\n')
	}
	if err := os.WriteFile(path, lines, 0o600); err != nil {
		t.Fatal(err)
	}

	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ModelCalls = 6
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	summary := st.ComputeParentRework(all)
	if summary.Coverage != ParentReworkCoverageComplete {
		t.Fatalf("coverage = %q", summary.Coverage)
	}
	codexReview := summary.ByOrigin[ParentOriginCodexReview]
	if codexReview.Calls != 2 || codexReview.WorkerCalls != 1 || codexReview.ReviewerCalls != 1 {
		t.Fatalf("codex-review rework calls = %#v", codexReview)
	}
	if codexReview.Turns != 6 || codexReview.TreeInputTokens != 280 || codexReview.TreeOutputTokens != 140 {
		t.Fatalf("codex-review rework tokens/turns = %#v", codexReview)
	}
	if codexReview.WallDurationMS != 10000 {
		t.Fatalf("codex-review wall = %#v", codexReview)
	}
	userAmendment := summary.ByOrigin[ParentOriginUserAmendment]
	if userAmendment.Calls != 1 || userAmendment.WorkerCalls != 1 || userAmendment.Turns != 1 {
		t.Fatalf("user-amendment rework = %#v", userAmendment)
	}
	if _, exists := summary.ByOrigin[ParentOriginUnknown]; exists {
		t.Fatalf("accept後の呼出はどのoriginへも帰属しません: %#v", summary.ByOrigin)
	}
}

func TestComputeParentReworkMarksMissingRecordsUnknown(t *testing.T) {
	dir := t.TempDir()
	st := &StateStore{dir: dir}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.loadTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	stats.ModelCalls = 3
	if err := st.writeTaskStats(stats); err != nil {
		t.Fatal(err)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	summary := st.ComputeParentRework(all)
	if summary.Coverage != ParentReworkCoverageUnknown {
		t.Fatalf("record欠損taskのcoverage = %q", summary.Coverage)
	}
	if taskID == "" {
		t.Fatal("task IDが空です")
	}
}
