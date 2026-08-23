package app

import (
	"bytes"
	"io"
	"strings"
	"testing"

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

func TestParseCommandFixOrigin(t *testing.T) {
	for _, origin := range []string{"codex-review", "glm-reviewer"} {
		command, err := ParseCommand([]string{"--fix", "--origin", origin, "指摘を修正"})
		if err != nil {
			t.Fatal(err)
		}
		if command.Mode != ModeFix || command.Payload != "指摘を修正" || command.Origin != origin {
			t.Fatalf("command = %#v", command)
		}
	}

	command, err := ParseCommand([]string{"--fix", "指摘を修正"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Origin != "" {
		t.Fatalf("未宣言originは空のまま: %#v", command)
	}

	for _, args := range [][]string{
		{"--fix", "--origin", "codex-review"},
		{"--fix", "--origin", "vibe-check", "指摘を修正"},
		{"--fix", "--origin", "reviewer", "指摘を修正"},
		{"--fix", "--origin"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("不正な--fix引数を拒否する必要があります: %#v", args)
		}
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
		{"--fix-stdin", "1", "--sha256"},
		{"--decision-stdin", "1", "--origin", "codex-review"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("不正なstdin option組み合わせを拒否する必要があります: %#v", args)
		}
	}
}

// reviewFixPacketAppはreviewerがSol確認へ昇格するpacket。
func reviewFixPacketApp() string {
	return "PACKET_BEGIN\nSTATUS: NEEDS_SOL_REVIEW\nRISK: HIGH\nSUMMARY: review\nREQUIREMENT_COVERAGE: covered\nINVARIANTS: preserved\nTEST_EVIDENCE: ev\nISSUES: i\nRESIDUAL_RISK: r\nTARGETS: t\nARTIFACTS: none\nSOL_QUESTION: q\nPACKET_END\n"
}

// needsSolDecisionPacketAppはworkerがSol判断を要求するpacket。
func needsSolDecisionPacketApp() string {
	return "PACKET_BEGIN\nSTATUS: NEEDS_SOL_DECISION\nRISK: HIGH\nDECISION: d\nEVIDENCE: e\nOPTIONS: o\nRECOMMENDATION: r\nTEST_OBLIGATIONS: tests\nTARGETS: t\nARTIFACTS: none\nPACKET_END\n"
}

func TestExecuteParentReviewFixThenAcceptFlow(t *testing.T) {
	cfg := newAppConfig(t)
	review := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("done")},
		{output: reviewFixPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if stats, err := st.CurrentTaskStats(); err != nil || stats.ParentReviewOpen == nil {
		t.Fatalf("NEEDS_SOL_REVIEW後にopportunityがopenではありません: %#v err=%v", stats, err)
	} else if stats.ParentReviewOpen.Role != "reviewer" || stats.ParentReviewOpen.ModelAlias != "haiku" {
		t.Fatalf("producer対応付け = %#v", stats.ParentReviewOpen)
	}
	if st.OpenParentReviewLabel() != "NEEDS_SOL_REVIEW" {
		t.Fatalf("open label = %q", st.OpenParentReviewLabel())
	}

	var statusOut bytes.Buffer
	if err := Execute(Command{Mode: ModeStatus}, cfg, nil, &statusOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(statusOut.String(), "PARENT_REVIEW_OPEN: NEEDS_SOL_REVIEW") {
		t.Fatalf("status出力 = %q", statusOut.String())
	}

	fix := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("fixed")},
		{output: passPacketApp()},
		{output: passPacketApp()},
	}}
	// reviewer terminal resultのNEEDS_SOL_REVIEW指摘をそのまま差し戻すためglm-reviewer。
	if err := Execute(Command{Mode: ModeFix, Payload: "指摘を修正", Origin: "glm-reviewer"}, cfg, fix.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	var acceptOut bytes.Buffer
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &acceptOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(acceptOut.String(), "PARENT_REVIEW: accepted") {
		t.Fatalf("accept出力 = %q", acceptOut.String())
	}

	var retryOut bytes.Buffer
	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &retryOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(retryOut.String(), "PARENT_REVIEW: no open terminal result") {
		t.Fatalf("accept再実行 = %q", retryOut.String())
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

	var statsOut bytes.Buffer
	if err := Execute(Command{Mode: ModeStats}, cfg, nil, &statsOut, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PARENT_OUTCOMES: accepted=1,fix=1",
		"PARENT_FIX_ORIGINS: glm-reviewer=1",
		"PARENT_OUTCOMES_BY_MODEL: haiku=1,sonnet=1",
		"PARENT_OUTCOMES_BY_RISK: HIGH=2",
		"PARENT_FIX_REWORK: origin=glm-reviewer calls=3 worker_calls=1 reviewer_calls=2",
		"PARENT_FIX_REWORK_COVERAGE: complete",
		"PARENT_REVIEW_NOTE: glm-worker-side parent action observation only",
	} {
		if !strings.Contains(statsOut.String(), want) {
			t.Fatalf("stats出力に%qがありません: %q", want, statsOut.String())
		}
	}
}

func TestExecuteAcceptWithoutOpenOpportunityIsNoOp(t *testing.T) {
	cfg := newAppConfig(t)
	var out bytes.Buffer

	if err := Execute(Command{Mode: ModeAccept}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "PARENT_REVIEW: no open terminal result") {
		t.Fatalf("accept出力 = %q", out.String())
	}
}

func TestExecuteNewTaskClosesOpenOpportunityAsUnknown(t *testing.T) {
	cfg := newAppConfig(t)
	first := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("done")},
		{output: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, first.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	second := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("next")},
		{output: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, second.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.AllTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	var archived state.TaskStats
	for _, stats := range all {
		if stats.ArchivedAt != nil {
			archived = stats
		}
	}
	if archived.TaskID == "" {
		t.Fatalf("archived statsが見つかりません: %#v", all)
	}
	if archived.ParentOutcomes[state.ParentOutcomeUnknown] != 1 || archived.ParentOutcomes[state.ParentOutcomeAccepted] != 0 {
		t.Fatalf("新task開始で未確定opportunityはunknown: %#v", archived.ParentOutcomes)
	}
}

func TestExecuteAcceptRejectsPendingDecisionOpportunity(t *testing.T) {
	cfg := newAppConfig(t)
	decision := &fakeRunner{steps: []fakeStep{
		{output: needsSolDecisionPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, decision.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	err := Execute(Command{Mode: ModeAccept}, cfg, nil, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "STATUS: WORKER_ERROR") {
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
		{output: needsSolDecisionPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, decisionWait.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	answer := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("resolved")},
		{output: reviewFixPacketApp()},
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
		{output: implementedPacketApp("done")},
		{output: reviewFixPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}

	fix := &fakeRunner{steps: []fakeStep{
		{output: implementedPacketApp("fixed")},
		{output: passPacketApp()},
		{output: passPacketApp()},
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
			{output: implementedPacketApp("done")},
			{output: reviewFixPacketApp()},
		}}
		if err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, review.factory(), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}

		fix := &fakeRunner{steps: []fakeStep{
			{output: implementedPacketApp("fixed")},
			{output: passPacketApp()},
			{output: passPacketApp()},
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
