package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandAccept(t *testing.T) {
	command, err := ParseCommand([]string{"--accept"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeAccept || command.Payload != "" {
		t.Fatalf("command = %#v", command)
	}
	if _, err := ParseCommand([]string{"--accept", "extra"}); err == nil {
		t.Fatal("--acceptは追加引数を拒否する必要があります")
	}
}

func TestParseCommandFixStdinOrigin(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	command, err := ParseCommand([]string{"--fix-stdin", "2507", "--origin", "metadata-repair"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeFix || command.StdinBytes != 2507 || command.Origin != "metadata-repair" {
		t.Fatalf("command = %#v", command)
	}

	command, err = ParseCommand([]string{"--fix-stdin", "1", "--sha256", digest, "--origin", "user-amendment"})
	if err != nil {
		t.Fatal(err)
	}
	if command.SHA256 != digest || command.Origin != "user-amendment" {
		t.Fatalf("command = %#v", command)
	}

	command, err = ParseCommand([]string{"--fix-stdin", "1", "--origin", "external-review", "--sha256", strings.ToUpper(digest)})
	if err != nil {
		t.Fatal(err)
	}
	if command.SHA256 != digest || command.Origin != "external-review" {
		t.Fatalf("command = %#v", command)
	}

	for _, args := range [][]string{
		{"--fix-stdin", "1", "--origin", "codex-review", "--origin", "user-amendment"},
		{"--fix-stdin", "1", "--origin"},
		{"--fix-stdin", "1", "--origin", "vibe-check"},
		{"--fix-stdin", "1", "--origin", "reviewer"},
		{"--fix-stdin", "1", "--sha256"},
		{"--decision-stdin", "1", "--origin", "codex-review"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("不正なstdin option組み合わせを拒否する必要があります: %#v", args)
		}
	}
}

func reviewFixPacketApp() string {
	return appPacketBody(packet.Result{
		Status:              packet.StatusNeedsSolReview,
		Risk:                packet.RiskHigh,
		Summary:             "review",
		RequirementCoverage: "covered",
		Invariants:          "preserved",
		TestEvidence:        "ev",
		Issues:              "i",
		ResidualRisk:        "r",
		Targets:             []string{"t"},
		SolQuestion:         "q",
	})
}

func needsSolDecisionPacketApp() string {
	return appPacketBody(packet.Result{
		Status:          packet.StatusNeedsSolDecision,
		Risk:            packet.RiskHigh,
		Decision:        "d",
		Evidence:        "e",
		Options:         "o",
		Recommendation:  "r",
		TestObligations: "tests",
		Targets:         []string{"t"},
	})
}

func newParentReviewOpportunity(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	cfg := newAppConfig(t)
	review := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: reviewFixPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func applyParentReviewFix(t *testing.T, cfg config.AppConfig) {
	t.Helper()
	fix := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("fixed")},
		{structured: passPacketApp()},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeFix, Payload: "指摘を修正", Origin: "glm-reviewer"}, cfg, fix.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func executeAccept(t *testing.T, cfg config.AppConfig) acceptOutput {
	t.Helper()
	var accept acceptOutput
	executeCommandOutput(t, cfg, ModeAccept, &accept, "accept")
	return accept
}

func TestExecuteParentReviewOpensOpportunity(t *testing.T) {
	cfg, st := newParentReviewOpportunity(t)

	stats, err := st.CurrentTaskStats()
	if err != nil || stats.ParentReviewOpen == nil {
		t.Fatalf("NEEDS_SOL_REVIEW後にopportunityがopenではありません: %#v err=%v", stats, err)
	}
	if stats.ParentReviewOpen.Role != "reviewer" || stats.ParentReviewOpen.ModelAlias != "haiku" {
		t.Fatalf("producer対応付け = %#v", stats.ParentReviewOpen)
	}
	if st.OpenParentReviewLabel() != "NEEDS_SOL_REVIEW" {
		t.Fatalf("open label = %q", st.OpenParentReviewLabel())
	}

	var statusOut bytes.Buffer
	if err := Execute(Command{Mode: ModeStatus}, cfg, nil, &statusOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	status := executeStatusOutput(t, cfg)
	if status.ParentReviewOpen == nil || *status.ParentReviewOpen != "NEEDS_SOL_REVIEW" {
		t.Fatalf("status出力のparent_review_open = %#v: %q", status.ParentReviewOpen, statusOut.String())
	}
}

func TestExecuteParentReviewFixThenAcceptRecordsOutcomes(t *testing.T) {
	cfg, st := newParentReviewOpportunity(t)
	applyParentReviewFix(t, cfg)
	if accept := executeAccept(t, cfg); !accept.Accepted {
		t.Fatal("open opportunityへのacceptが確定されませんでした")
	}

	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[state.ParentOutcomeFix] != 1 || stats.ParentOutcomes[state.ParentOutcomeAccepted] != 1 {
		t.Fatalf("outcome集計 = %#v", stats.ParentOutcomes)
	}
	if stats.ParentFixOrigins[state.ParentOriginGLMReviewer] != 1 {
		t.Fatalf("origin集計 = %#v", stats.ParentFixOrigins)
	}
	if stats.FixCommands != 1 {
		t.Fatalf("fix_commands = %d", stats.FixCommands)
	}
	outcomeTotal := 0
	for _, value := range stats.ParentOutcomes {
		outcomeTotal += value
	}
	packets := stats.PassPackets + stats.NeedsSolReviewPackets + stats.NeedsSolDecisionPackets
	if outcomeTotal != packets || stats.ParentReviewOpen != nil {
		t.Fatalf("加法整合: outcomes=%d packets=%d open=%#v", outcomeTotal, packets, stats.ParentReviewOpen)
	}
}

func TestExecuteParentReviewAcceptIsSingleUse(t *testing.T) {
	cfg, _ := newParentReviewOpportunity(t)
	applyParentReviewFix(t, cfg)
	if first := executeAccept(t, cfg); !first.Accepted {
		t.Fatal("最初のacceptが確定されませんでした")
	}
	if retry := executeAccept(t, cfg); retry.Accepted {
		t.Fatal("同一opportunityのaccept再実行が二重確定されました")
	}
}

func TestExecuteParentReviewStatsExposeRework(t *testing.T) {
	cfg, st := newParentReviewOpportunity(t)
	applyParentReviewFix(t, cfg)
	if accept := executeAccept(t, cfg); !accept.Accepted {
		t.Fatal("acceptが確定されませんでした")
	}

	var statsOut bytes.Buffer
	if err := Execute(Command{Mode: ModeStats}, cfg, nil, &statsOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	output := executeStatsOutput(t, st)
	if output.ParentOutcomes[state.ParentOutcomeAccepted] != 1 || output.ParentOutcomes[state.ParentOutcomeFix] != 1 {
		t.Fatalf("parent_outcomes = %#v: %q", output.ParentOutcomes, statsOut.String())
	}
	if output.ParentFixOrigins[state.ParentOriginGLMReviewer] != 1 {
		t.Fatalf("parent_fix_origins = %#v", output.ParentFixOrigins)
	}
	if output.ParentOutcomesByModel["haiku"] != 1 || output.ParentOutcomesByModel["sonnet"] != 1 {
		t.Fatalf("parent_outcomes_by_model = %#v", output.ParentOutcomesByModel)
	}
	if output.ParentOutcomesByRisk["HIGH"] != 2 {
		t.Fatalf("parent_outcomes_by_risk = %#v", output.ParentOutcomesByRisk)
	}
	if len(output.ParentFixRework) != 1 {
		t.Fatalf("parent_fix_rework = %#v", output.ParentFixRework)
	}
	rework := output.ParentFixRework[0]
	if rework.Origin != state.ParentOriginGLMReviewer || rework.Calls != 3 || rework.WorkerCalls != 1 || rework.ReviewerCalls != 2 {
		t.Fatalf("parent_fix_rework = %#v", rework)
	}
	if output.ParentFixReworkCoverage != "complete" {
		t.Fatalf("parent_fix_rework_coverage = %q", output.ParentFixReworkCoverage)
	}
}

func TestExecuteAcceptWithoutOpenOpportunityIsNoOp(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var accept acceptOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &accept); err != nil {
		t.Fatalf("accept出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if accept.Accepted {
		t.Fatalf("accept出力 = %q", out.String())
	}
}

func TestExecuteNewTaskRejectsOpenParentReviewUntilAccepted(t *testing.T) {
	cfg, st := newParentReviewOpportunity(t)
	before, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if before.ParentReviewOpen == nil || before.ParentReviewOpen.PacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("前taskのparent reviewがopenではありません: %#v", before.ParentReviewOpen)
	}
	beforeLogs, err := st.ReadModelCallLogs(before.TaskID)
	if err != nil {
		t.Fatal(err)
	}

	rejected := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("must not run")},
		{structured: passPacketApp()},
	}}
	err = Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, rejected.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "unresolved parent review") || !strings.Contains(err.Error(), "--accept") {
		t.Fatalf("未解決parent reviewの新task開始を明示的に拒否する必要があります: %v", err)
	}
	if len(rejected.prompts) != 0 {
		t.Fatalf("拒否時はmodelを呼び出してはいけません: %d", len(rejected.prompts))
	}

	after, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.TaskID != before.TaskID || after.ParentReviewOpen == nil || after.ParentReviewOpen.PacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("拒否時にcurrent task/reviewが変化しました: before=%#v after=%#v", before, after)
	}
	if after.ParentOutcomes[state.ParentOutcomeUnknown] != 0 {
		t.Fatalf("拒否時にunknown outcomeを記録してはいけません: %#v", after.ParentOutcomes)
	}
	afterLogs, err := st.ReadModelCallLogs(after.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterLogs) != len(beforeLogs) {
		t.Fatalf("拒否時にtelemetryが変化しました: before=%d after=%d", len(beforeLogs), len(afterLogs))
	}

	if accept := executeAccept(t, cfg); !accept.Accepted {
		t.Fatal("open parent reviewをacceptできませんでした")
	}
	if got := st.TaskStatus(); got != state.TaskStatusWaitingSolReview {
		t.Fatalf("accept後のstale lifecycle status = %q want %q", got, state.TaskStatusWaitingSolReview)
	}

	next := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("next")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, next.factory(), io.Discard, io.Discard); err != nil {
		t.Fatalf("parent review解決後はstale statusだけで新taskを拒否してはいけません: %v", err)
	}

	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	var archived state.TaskStats
	for _, stats := range all {
		if stats.TaskID == before.TaskID {
			archived = stats
			break
		}
	}
	if archived.TaskID == "" || archived.ArchivedAt == nil {
		t.Fatalf("前task statsがarchiveされていません: %#v", all)
	}
	if archived.ParentOutcomes[state.ParentOutcomeAccepted] != 1 || archived.ParentOutcomes[state.ParentOutcomeUnknown] != 0 {
		t.Fatalf("明示parent outcomeが保持されていません: %#v", archived.ParentOutcomes)
	}
}
func TestExecuteAcceptRejectsPendingDecisionOpportunity(t *testing.T) {
	cfg := newAppConfig(t)
	decision := &fakeRunner{steps: []fakeStep{
		{structured: needsSolDecisionPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, decision.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "--decision") {
		t.Fatalf("decision待ちへの--acceptを拒否する必要があります: %v", err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if len(stats.ParentOutcomes) != 0 || stats.ParentReviewOpen == nil {
		t.Fatalf("拒否時はstateを変更しません: %#v", stats)
	}
}

func TestExecuteDecisionRecordsParentOutcome(t *testing.T) {
	cfg := newAppConfig(t)
	decisionWait := &fakeRunner{steps: []fakeStep{
		{structured: needsSolDecisionPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, decisionWait.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	answer := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("resolved")},
		{structured: reviewFixPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeDecision, Payload: "A案で進める"}, cfg, answer.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[state.ParentOutcomeDecision] != 1 || stats.DecisionCommands != 1 {
		t.Fatalf("decision outcome = %#v commands=%d", stats.ParentOutcomes, stats.DecisionCommands)
	}
	if stats.ParentReviewOpen == nil || stats.ParentReviewOpen.PacketStatus != "NEEDS_SOL_REVIEW" {
		t.Fatalf("decision後の新terminal packetがopenではありません: %#v", stats.ParentReviewOpen)
	}
}

func TestExecuteFixWithoutOriginRecordsUnknownOrigin(t *testing.T) {
	cfg := newAppConfig(t)
	review := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("done")},
		{structured: reviewFixPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	fix := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("fixed")},
		{structured: passPacketApp()},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeFix, Payload: "直したい"}, cfg, fix.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentFixOrigins[state.ParentOriginUnknown] != 1 {
		t.Fatalf("--fix単独はorigin unknown: %#v", stats.ParentFixOrigins)
	}
	if stats.ParentOutcomes[state.ParentOutcomeFix] != 1 || stats.ParentOutcomes[state.ParentOutcomeAccepted] != 0 {
		t.Fatalf("--fix単独をacceptedへ推定しません: %#v", stats.ParentOutcomes)
	}
}

func TestExecuteFixOriginValuesRecorded(t *testing.T) {
	for _, origin := range []string{
		state.ParentOriginCodexReview,
		state.ParentOriginGLMReviewer,
		state.ParentOriginUserAmendment,
		state.ParentOriginExternalReview,
		state.ParentOriginMetadataRepair,
	} {
		cfg := newAppConfig(t)
		review := &fakeRunner{steps: []fakeStep{
			{structured: implementedPacketApp("done")},
			{structured: reviewFixPacketApp()},
		}}
		if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}

		fix := &fakeRunner{steps: []fakeStep{
			{structured: implementedPacketApp("fixed")},
			{structured: passPacketApp()},
			{structured: passPacketApp()},
		}}
		if err := Execute(Command{Mode: ModeFix, Payload: "直したい", Origin: origin}, cfg, fix.factory(), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}

		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		stats, err := st.CurrentTaskStats()
		if err != nil {
			t.Fatal(err)
		}
		if stats.ParentFixOrigins[origin] != 1 || len(stats.ParentFixOrigins) != 1 {
			t.Fatalf("origin %s = %#v", origin, stats.ParentFixOrigins)
		}
		if stats.ParentOutcomes[state.ParentOutcomeFix] != 1 {
			t.Fatalf("origin %s outcome = %#v", origin, stats.ParentOutcomes)
		}
	}
}
