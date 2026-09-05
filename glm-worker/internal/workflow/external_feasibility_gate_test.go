package workflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	feasibilityNotApplicableDecl  = "## External feasibility\n\nstatus: not-applicable"
	feasibilityPoCDecl            = "## External feasibility\n\nstatus: poc\nassumption: 実producerがapi_retry中のretry前にexact signalを公開するか未検証"
	feasibilityObservationDecl    = "## External feasibility\n\nstatus: observation\nassumption: 外部serviceのevent timingがproduction correctnessの前提になる"
	feasibilityImplementationDecl = "## External feasibility\n\nstatus: implementation\n" +
		"assumption: 実producerがapi_retry中のretry前にexact signalを公開する\n" +
		"evidence-source: producer\n" +
		"evidence: 実Claude CLI session logでretry前のapi_retry eventにexact signalを観測\n" +
		"go: 2026-08-24 親Codexが実producer観測に基づきGo判断"
	feasibilityUnverifiedDecl = "## External feasibility\n\nstatus: implementation\nassumption: 実producerのfield timingが未検証"
	feasibilityFixtureDecl    = "## External feasibility\n\nstatus: implementation\n" +
		"assumption: 実producerのfield timing\n" +
		"evidence-source: fixture\n" +
		"evidence: 人工fixtureへexact本文を直接与えたtest\n" +
		"go: なし"
)

const pocOmissionMarker = "[前方を省略] "

func writeFeasibilityActiveTask(t *testing.T, repoRoot string, declaration string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(repoRoot, implementationTasksDir), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "# ACTIVE task\n\n"
	if declaration != "" {
		content += declaration + "\n\n"
	}
	content += "## Contract\n\n- gate検証用seed\n"
	if err := os.WriteFile(filepath.Join(repoRoot, activeTaskGuardPath), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRoot, implementationPlanFile), []byte(planGuardSeed), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExternalFeasibilityDeclarationParsing(t *testing.T) {
	taskWith := func(decl string) []byte {
		return []byte("# task\n\n" + decl + "\n\n## Contract\n\n- seed\n")
	}
	cases := []struct {
		name string
		task string
		want string
	}{
		{"not-applicable", feasibilityNotApplicableDecl, "not-applicable"},
		{"poc", feasibilityPoCDecl, "poc"},
		{"observation", feasibilityObservationDecl, "observation"},
		{"implementation with producer evidence", feasibilityImplementationDecl, "implementation"},
		{"section missing", "", ""},
		{"status missing", "## External feasibility\n\nassumption: x", ""},
		{"unknown status", "## External feasibility\n\nstatus: verified", ""},
		{"duplicate section", "## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nx\n\n## External feasibility\n\nstatus: poc\nassumption: y", ""},
		{"prose line", "## External feasibility\n\nstatus: not-applicable\n通常の説明文", ""},
		{"unknown key", "## External feasibility\n\nstatus: poc\nassumption: x\nmemo: y", ""},
		{"duplicate key", "## External feasibility\n\nstatus: poc\nassumption: x\nassumption: y", ""},
		{"empty value", "## External feasibility\n\nstatus: poc\nassumption:  ", ""},
		{"poc without assumption", "## External feasibility\n\nstatus: poc", ""},
		{"observation without assumption", "## External feasibility\n\nstatus: observation", ""},
		{"not-applicable with assumption", "## External feasibility\n\nstatus: not-applicable\nassumption: x", ""},
		{"poc with evidence keys", "## External feasibility\n\nstatus: poc\nassumption: x\nevidence: y", ""},
		{"implementation missing evidence fields", feasibilityUnverifiedDecl, ""},
		{"implementation fixture evidence", feasibilityFixtureDecl, ""},
		{"implementation scripted evidence", "## External feasibility\n\nstatus: implementation\nassumption: a\nevidence-source: scripted-packet\nevidence: e\ngo: g", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decl, err := parseExternalFeasibilityDeclaration(taskWith(tc.task))
			if tc.want == "" {
				if err == nil {
					t.Fatalf("拒否すべき宣言が受理されました: %+v", decl)
				}
				return
			}
			if err != nil {
				t.Fatalf("受理すべき宣言が拒否されました: %v", err)
			}
			if decl.status != tc.want {
				t.Fatalf("status = %q want %q", decl.status, tc.want)
			}
		})
	}
	if _, err := parseExternalFeasibilityDeclaration([]byte("# plan無しtask\n")); err == nil {
		t.Fatal("節の無いtask fileは拒否されるべきです")
	}
}

func TestExternalFeasibilityDeclarationIgnoresFencedBlocks(t *testing.T) {

	fencedExample := "```markdown\n## External feasibility\n\nstatus: poc\nassumption: fence内の例\n```"
	withTopLevel := "# task\n\n````text\n" + fencedExample + "\n````\n\n" + feasibilityNotApplicableDecl + "\n\n## Contract\n\n- seed\n"
	decl, err := parseExternalFeasibilityDeclaration([]byte(withTopLevel))
	if err != nil {
		t.Fatalf("fence外に宣言があるtaskは受理されるべきです: %v", err)
	}
	if decl.status != externalFeasibilityStatusNotApplicable {
		t.Fatalf("status = %q want not-applicable(fence外のtop-level宣言が使われるべき)", decl.status)
	}

	fenceOnly := "# task\n\n````text\n" + fencedExample + "\n````\n\n## Contract\n\n- seed\n"
	_, err = parseExternalFeasibilityDeclaration([]byte(fenceOnly))
	var reject *externalFeasibilityParseError
	if !errors.As(err, &reject) || reject.kind != externalFeasibilityRejectMissing {
		t.Fatalf("fence内宣言だけのtaskはmissing拒否されるべきです: %v", err)
	}
}

func requireFeasibilityFailClosed(t *testing.T, st *state.StateStore, r *mutatingRunner, out *bytes.Buffer, wantOutcome string, wantStatus state.TaskStatus) {
	t.Helper()
	if len(r.prompts) != 0 {
		t.Fatalf("宣言gate拒否時はmodel呼出0回であるべき: %d(%v)", len(r.prompts), r.phases)
	}
	if st.TaskStatus() != wantStatus {
		t.Fatalf("task status = %q want %q", st.TaskStatus(), wantStatus)
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
	events := 0
	for _, l := range taskLogs(t, st) {
		if l.CallType == state.CallTypeTask {
			t.Fatalf("呼出前停止なのにtask記録が残っています: %+v", l)
		}
		if l.CallType == state.CallTypeEvent && l.Outcome == wantOutcome && strings.HasSuffix(l.Phase, externalFeasibilityGuardSurface.eventSuffix) {
			events++
		}
	}
	if events != 1 {
		t.Fatalf("event outcome %s = %d want 1", wantOutcome, events)
	}
}

func TestExternalFeasibilityMissingDeclarationFailsClosedBeforeWorker(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, "")
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.missingOutcome(), state.TaskStatusWaitingSolReview)
	if !strings.Contains(out.String(), externalFeasibilitySectionHeading) {
		t.Fatalf("回復手続き(宣言節の追加)が出力されていません:\n%s", out.String())
	}
}

func TestExternalFeasibilityUnverifiedImplementationFailsClosed(t *testing.T) {
	for name, decl := range map[string]string{
		"evidence欠落":  feasibilityUnverifiedDecl,
		"人工fixtureだけ": feasibilityFixtureDecl,
	} {
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writeFeasibilityActiveTask(t, repoRoot, decl)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.unverifiedOutcome(), state.TaskStatusWaitingSolReview)
		})
	}
}

func TestExternalFeasibilityMalformedDeclarationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, "## External feasibility\n\nstatus: verified")
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("done")},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.malformedOutcome(), state.TaskStatusWaitingSolReview)
}

func TestExternalFeasibilityVerifiedImplementationProceedsNormally(t *testing.T) {
	cases := []struct {
		name string
		decl string
	}{
		{name: "not applicable", decl: feasibilityNotApplicableDecl},
		{name: "producer evidence", decl: feasibilityImplementationDecl},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writeFeasibilityActiveTask(t, repoRoot, tc.decl)
			w, r, _, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("done")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if st.TaskStatus() != state.TaskStatusComplete {
				t.Fatalf("status = %q want complete", st.TaskStatus())
			}
			if len(r.prompts) != 2 {
				t.Fatalf("worker/reviewer 2呼出が必要: %d", len(r.prompts))
			}
			if r.readOnlyCalls[0] {
				t.Fatal("verified implementation taskのworkerは書き込み可能であるべきです")
			}
		})
	}
}

func TestExternalFeasibilityPoCTaskRunsReadOnlyAndReturnsGoNoGo(t *testing.T) {
	for name, decl := range map[string]string{"poc": feasibilityPoCDecl, "observation": feasibilityObservationDecl} {
		t.Run(name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writeFeasibilityActiveTask(t, repoRoot, decl)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: implementedPacket("observed")},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("PoC taskはworker 1呼出だけでreviewerを呼ばない: %d(%v)", len(r.prompts), r.phases)
			}
			if !r.readOnlyCalls[0] {
				t.Fatal("PoC taskのworkerはread-only capabilityで実行されるべきです")
			}
			if st.TaskStatus() != state.TaskStatusWaitingDecision {
				t.Fatalf("status = %q want waiting-decision", st.TaskStatus())
			}
			if !st.Exists("pending-decision") {
				t.Fatal("親Go/No-Go待ち(pending-decision)が残っているべきです")
			}
			pkt := lastPacketFromOutput(t, out.String())
			if pkt.Status != "NEEDS_SOL_DECISION" || pkt.Risk != "HIGH" {
				t.Fatalf("packet = %s/%s want NEEDS_SOL_DECISION/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
			}
			if !strings.Contains(pkt.Decision, "implementation") || !strings.Contains(pkt.Decision, "書き換え") {
				t.Fatalf("decision fieldが親宣言migration以外の昇格手段を許しています: %q", pkt.Decision)
			}
		})
	}
}

func TestExternalFeasibilityPoCWorkerMutationFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("observed")},
		{structured: passPacket()},
	}, "worker-new", 0, func(repoRoot string) error {
		return os.WriteFile(filepath.Join(repoRoot, "tracked.txt"), []byte("glm production diff\n"), 0o644)
	})

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("PoC違反時はreviewerを呼ばない: %d(%v)", len(r.prompts), r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if !strings.Contains(out.String(), "production diff禁止違反") {
		t.Fatalf("PoC不変性違反理由が出力されていません:\n%s", out.String())
	}
	if content, err := os.ReadFile(filepath.Join(repoRoot, "tracked.txt")); err != nil || string(content) != "glm production diff\n" {
		t.Fatalf("PoC違反diffはSol確認へ現物のまま残すべき: %q %v", content, err)
	}
}

func TestExternalFeasibilityDecisionGatePreservesPendingDecision(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityUnverifiedDecl)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)

	if err := w.ExecuteDecision("decision"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("decision消費前拒否時はmodel呼出0回であるべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("task status = %q want waiting-decision(decisionを消費していない)", st.TaskStatus())
	}
	if !st.Exists("pending-decision") {
		t.Fatal("pending decisionが残っているべきです")
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, out.String())
	}
}

func TestExternalFeasibilityPoCToImplementationDecisionResumesWritable(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: implementedPacket("observed")},
		{structured: implementedPacket("implemented after Go")},
		{structured: passPacket()},
		{structured: needsSolReviewPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("PoC後のtask status = %q want waiting-decision", st.TaskStatus())
	}
	if !r.readOnlyCalls[0] {
		t.Fatal("PoC workerはread-only capabilityで実行されるべきです")
	}

	writeFeasibilityActiveTask(t, repoRoot, feasibilityImplementationDecl)
	if err := w.ExecuteDecision("Go: 実装へ進める"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 4 {
		t.Fatalf("migration後のdecisionはwritable worker・reviewer・risk floor再出力の3呼出が追加されるべき: %d(%v)", len(r.prompts), r.phases)
	}
	if r.readOnlyCalls[1] {
		t.Fatal("implementation migration後のdecision workerは書き込み可能であるべきです")
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("migration後のtask status = %q want waiting-sol-review(risk floor経路)", st.TaskStatus())
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" {
		t.Fatalf("最終packet = %s want NEEDS_SOL_REVIEW:\n%s", pkt.Status, out.String())
	}
}

func TestExternalFeasibilityFixGateFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityFixtureDecl)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)

	if err := w.ExecuteExplicitFix("fix", "", ""); err != nil {
		t.Fatal(err)
	}
	requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.unverifiedOutcome(), state.TaskStatusWaitingSolReview)
}

func TestExternalFeasibilityReviewResumeGateFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, "")
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	workerResult := workerResultFromBody(implementedPacket("done"))
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageReview,
		Phase:          "reviewer-1",
		Role:           state.ReviewerRole,
		Model:          "haiku",
		Effort:         "high",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "request",
		WorkerResult:   workerResult,
		ReviewNumber:   1,
		StopKind:       state.ResumeStopRateLimited,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}

	requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.missingOutcome(), state.TaskStatusRateLimited)
	saved, err := st.LoadResumeCheckpoint()
	if err != nil {
		t.Fatalf("拒否後にresume checkpointが保持されているべきです: %v", err)
	}
	if saved.StopKind != state.ResumeStopRateLimited || saved.WorkerResult == nil || saved.WorkerResult.Status != workerResult.Status || saved.WorkerResult.Summary != workerResult.Summary {
		t.Fatalf("拒否がreviewer resumeのcheckpoint内容を壊しています: %+v", saved)
	}
}

func TestExternalFeasibilityResumeGateFailsClosedBeforeProbe(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, "")
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		Effort:                            "high",
		Prompt:                            "p",
		OriginalPrompt:                    "p",
		Request:                           "request",
		StopKind:                          state.ResumeStopProviderUnavailable,
		ProviderUnavailableClassification: "http_503",
		ProviderUnavailableStartedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}

	requireFeasibilityFailClosed(t, st, r, out, externalFeasibilityGuardSurface.missingOutcome(), state.TaskStatusProviderUnavailable)
	if len(r.probes) != 0 {
		t.Fatalf("宣言gate拒否時はprobeも実行しない: %d", len(r.probes))
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || saved.StopKind != state.ResumeStopProviderUnavailable {
		t.Fatalf("拒否後にresume checkpointが保持されているべきです: %v %+v", err, saved)
	}
}

func TestExternalFeasibilityInterruptedResumeRejectThenRepairUsesCanonicalAdmission(t *testing.T) {
	repo := newRetentionGitRepo(t)
	taskPath := filepath.Join(repo, activeTaskGuardPath)
	validTask := "# ACTIVE task\n\n" + feasibilityNotApplicableDecl + "\n\n## Contract\n\n- gate検証用seed\n"
	writeFeasibilityActiveTask(t, repo, feasibilityNotApplicableDecl)
	st := newGitStateStoreT(t, repo)
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	stopRunner := &scriptedRunner{steps: []runnerStep{{
		result: runner.RunResult{SessionID: "sess-retention"},
		runErr: &runner.InterruptedCallError{Phase: "worker-new"},
	}}}
	stopW := newGitWorkflowT(t, st, stopRunner, repo)
	stopWorkflowInCall(t, stopW, st, workerCheckpoint())

	if err := os.WriteFile(taskPath, []byte("# ACTIVE task\n\n## Contract\n\n- 宣言が消えた\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rejectRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	rejectW := newGitWorkflowT(t, st, rejectRunner, repo)
	var rejectOut bytes.Buffer
	rejectW.output = &rejectOut
	if err := rejectW.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(rejectRunner.prompts) != 0 {
		t.Fatalf("宣言欠損resume拒否時はmodel呼出0回であるべき: %d", len(rejectRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("拒否後のtask status = %q want interrupted(停止理由を保持)", st.TaskStatus())
	}
	saved, err := st.LoadResumeCheckpoint()
	if err != nil || saved.StopKind != state.ResumeStopInterrupted || saved.StopGitSnapshot == nil {
		t.Fatalf("拒否後にinterrupted checkpointと保持基準が残っているべきです: %v %+v", err, saved)
	}
	pkt := lastPacketFromOutput(t, rejectOut.String())
	if pkt.Status != "NEEDS_SOL_REVIEW" || pkt.Risk != "HIGH" {
		t.Fatalf("拒否packet = %s/%s want NEEDS_SOL_REVIEW/HIGH:\n%s", pkt.Status, pkt.Risk, rejectOut.String())
	}

	if err := os.WriteFile(taskPath, []byte(validTask), 0o644); err != nil {
		t.Fatal(err)
	}
	resumeRunner := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("resumed")}}}
	resumeW := newGitWorkflowT(t, st, resumeRunner, repo)
	err = resumeW.ExecuteResume()
	if err == nil || !strings.Contains(err.Error(), "lifecycle inconsistency") {
		t.Fatalf("fail-closed review後のresume再実行error = %v", err)
	}
	if len(resumeRunner.prompts) != 0 {
		t.Fatalf("canonical admission拒否後にmodelが呼ばれました: %d", len(resumeRunner.prompts))
	}
	if st.TaskStatus() != state.TaskStatusInterrupted {
		t.Fatalf("canonical admission拒否後のtask status = %q want interrupted", st.TaskStatus())
	}
}

func TestExternalFeasibilityPoCResumeUsesSavedSnapshot(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "request",
		StopKind:       state.ResumeStopRateLimited,
		ReadOnly:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	start, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SavePoCStartSnapshot(start); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)
	r.steps = []runnerStep{{structured: implementedPacket("resumed")}, {structured: passPacket()}}

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("PoC resumeはworker再開1呼出だけでreviewerを呼ばない: %d(%v)", len(r.prompts), r.phases)
	}
	if !r.readOnlyCalls[0] {
		t.Fatal("PoC resumeのworkerはread-only capabilityであるべきです")
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %q want waiting-decision", st.TaskStatus())
	}
	pkt := lastPacketFromOutput(t, out.String())
	if pkt.Status != "NEEDS_SOL_DECISION" {
		t.Fatalf("packet = %s want NEEDS_SOL_DECISION:\n%s", pkt.Status, out.String())
	}
}

func TestExternalFeasibilityPoCResumeWithoutSnapshotFailsClosed(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
	st := newStateStoreT(t)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(activeTaskStateKey, activeTaskGuardPath); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-new",
		Role:           state.WorkerRole,
		Model:          "opus",
		Effort:         "high",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "request",
		StopKind:       state.ResumeStopRateLimited,
		ReadOnly:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	w, r, out := planFileDecisionWorkflow(t, st, repoRoot, "", nil)

	if err := w.ExecuteResume(); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("基準snapshot欠損時はmodel呼出0回であるべき: %d", len(r.prompts))
	}
	if st.TaskStatus() != state.TaskStatusWaitingSolReview {
		t.Fatalf("status = %q want waiting-sol-review", st.TaskStatus())
	}
	if !strings.Contains(out.String(), "PoC開始前snapshotが欠損") {
		t.Fatalf("snapshot欠損理由が出力されていません:\n%s", out.String())
	}
}

func TestExternalFeasibilityDeclarationContextBudget(t *testing.T) {
	notApplicable := "## External feasibility\n\nstatus: not-applicable\n"
	implementation := feasibilityImplementationDecl + "\n"
	if got := len(notApplicable); got > 64 {
		t.Fatalf("not-applicable宣言 = %d bytes(<=64, token proxy %d)に収めてください", got, got/4)
	}
	if got := len(implementation); got > 512 {
		t.Fatalf("implementation宣言 = %d bytes(<=512, token proxy %d)に収めてください", got, got/4)
	}
}

func requireGoNoGoContract(t *testing.T, result packet.Result) {
	t.Helper()
	if err := packet.ValidateWorkerResult(result); err != nil {
		t.Fatalf("Go/No-Go結果が現行packet validatorを通りません: %v", err)
	}
	if result.Status != packet.StatusNeedsSolDecision || result.Risk != packet.RiskHigh {
		t.Fatalf("Go/No-Go結果 = %s/%s want NEEDS_SOL_DECISION/HIGH", result.Status, result.Risk)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"decision", result.Decision},
		{"evidence", result.Evidence},
		{"options", result.Options},
		{"recommendation", result.Recommendation},
		{"test_obligations", result.TestObligations},
	} {
		if field.value == "" {
			t.Fatalf("必須field %sが空です", field.name)
		}
		if len(field.value) > packet.MaxFieldBytes {
			t.Fatalf("field %sが%d bytes上限を超えています: %d bytes", field.name, packet.MaxFieldBytes, len(field.value))
		}
		if strings.ContainsAny(field.value, "\n\r") {
			t.Fatalf("field %sに改行を含めています: %q", field.name, field.value)
		}
		if !utf8.ValidString(field.value) {
			t.Fatalf("field %sが不正UTF-8です", field.name)
		}
	}
	if !strings.Contains(result.Decision, "Go/No-Go") || !strings.Contains(result.Options, "No-Go") || !strings.Contains(result.Options, "観測継続") || result.Recommendation == "" {
		t.Fatalf("親Go/No-Go判断に必要な意味情報が失われています: decision=%q options=%q", result.Decision, result.Options)
	}
}

func TestPoCGoNoGoResultKeepsValidatorContract(t *testing.T) {
	for _, tc := range []struct {
		name       string
		summary    string
		tests      string
		unverified string
	}{
		{"短い観測結果", "観測した", "test pass", "none"},
		{"1536 bytes超の観測結果", strings.Repeat("観", 500), strings.Repeat("検", 400), strings.Repeat("残", 100)},
		{"切詰め境界がrune途中の観測結果", strings.Repeat("あ", 500), strings.Repeat("い", 399), "x" + strings.Repeat("う", 99)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := packet.Result{
				Status:              packet.StatusImplemented,
				Risk:                packet.RiskLow,
				Summary:             tc.summary,
				RequirementCoverage: "covered",
				Tests:               tc.tests,
				Unverified:          tc.unverified,
				Targets:             []string{"src/observe.go:Run"},
			}
			if err := packet.ValidateWorkerResult(worker); err != nil {
				t.Fatalf("前提: PoC worker resultは現行validatorを通るべき: %v", err)
			}
			joined := "観測結果: " + tc.summary + "; " + tc.tests + "; " + tc.unverified
			result := pocGoNoGoResult(worker)
			requireGoNoGoContract(t, result)
			if !slices.Equal(result.Targets, worker.Targets) {
				t.Fatalf("対象targetsが保持されていません: %v", result.Targets)
			}
			if result.TestObligations != tc.tests {
				t.Fatalf("test obligationsは上限内ならそのまま保持すべき: %q", result.TestObligations)
			}
			if len(joined) > packet.MaxFieldBytes {
				if !strings.HasPrefix(result.Evidence, pocOmissionMarker) {
					t.Fatalf("切詰めevidenceには省略markerが必要です: %q", result.Evidence)
				}
				if !strings.HasSuffix(joined, result.Evidence[len(pocOmissionMarker):]) {
					t.Fatal("切詰めevidenceは観測内容の末尾を保持すべきです")
				}
			} else if result.Evidence != joined {
				t.Fatalf("上限内の観測結果はそのまま保持すべきです: %q", result.Evidence)
			}
		})
	}
}

func pocHeavyTargets(t *testing.T, count int, fillerLength int) []string {
	t.Helper()
	targets := make([]string, 0, count)
	for i := range count {
		targets = append(targets, fmt.Sprintf("glm-worker/internal/workflow/observation/evidence/path%02d.go:ObserveSymbol%02d-%s", i, i, strings.Repeat("x", fillerLength)))
		if len(targets[i]) > packet.MaxFieldBytes {
			t.Fatalf("前提: target要素は%d bytes以内であるべき: %d bytes", packet.MaxFieldBytes, len(targets[i]))
		}
	}
	return targets
}

func TestPoCGoNoGoResultPassesValidatorOnTargetHeavyInput(t *testing.T) {
	for _, tc := range []struct {
		name         string
		summary      string
		tests        string
		unverified   string
		targetCount  int
		targetFiller int
	}{
		{"観測長文とtargets併存", strings.Repeat("観", 100), strings.Repeat("検", 512), strings.Repeat("残", 100), 14, 110},
		{"tests上限と多量targets", "観測した", strings.Repeat("検", 512), "none", 12, 250},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := packet.Result{
				Status:              packet.StatusImplemented,
				Risk:                packet.RiskLow,
				Summary:             tc.summary,
				RequirementCoverage: "covered",
				Tests:               tc.tests,
				Unverified:          tc.unverified,
				Targets:             pocHeavyTargets(t, tc.targetCount, tc.targetFiller),
				Artifacts:           []string{"/artifacts/poc/observation-report.txt"},
			}
			if err := packet.ValidateWorkerResult(worker); err != nil {
				t.Fatalf("前提: PoC worker resultは現行validatorを通るべき: %v", err)
			}
			joined := "観測結果: " + tc.summary + "; " + tc.tests + "; " + tc.unverified
			result := pocGoNoGoResult(worker)
			requireGoNoGoContract(t, result)
			if size := result.ByteSize(); size > packet.MaxPacketBytes {
				t.Fatalf("packet全体が%d bytes上限を超えています: %d bytes", packet.MaxPacketBytes, size)
			}
			if !slices.Equal(result.Targets, worker.Targets) || !slices.Equal(result.Artifacts, worker.Artifacts) {
				t.Fatal("通常帯の対象targetsとartifactsはそのまま保持すべきです")
			}
			if len(joined) > packet.MaxFieldBytes {
				if len(result.Evidence) >= len(joined) {
					t.Fatal("packet上限超過時はevidenceを縮小すべきです")
				}
				if !strings.HasPrefix(result.Evidence, pocOmissionMarker) || !strings.HasSuffix(joined, result.Evidence[len(pocOmissionMarker):]) {
					t.Fatalf("縮約後もevidenceは省略markerと観測末尾を保持すべきです: %q", result.Evidence)
				}
			}
		})
	}
}

func requirePoCPassthroughSummarized(t *testing.T, result packet.Result, worker packet.Result) {
	t.Helper()
	if len(result.Targets) == 0 || len(result.Targets) > len(worker.Targets) {
		t.Fatalf("完全なtarget entryを最低1件保持すべきです: %v", result.Targets)
	}
	if !slices.Equal(result.Targets, worker.Targets[:len(result.Targets)]) {
		t.Fatalf("保持したtargetsは元のlistの先頭完全entryのままにすべきです: %v", result.Targets)
	}
	if len(result.Artifacts) > len(worker.Artifacts) || !slices.Equal(result.Artifacts, worker.Artifacts[:len(result.Artifacts)]) {
		t.Fatalf("保持したartifactsは元のlistの先頭完全entryだけにすべきです: %v", result.Artifacts)
	}
	omittedTargets := len(worker.Targets) - len(result.Targets)
	omittedArtifacts := len(worker.Artifacts) - len(result.Artifacts)
	if omittedTargets > 0 && !strings.Contains(result.Evidence, fmt.Sprintf("targets省略%d件", omittedTargets)) {
		t.Fatalf("省略したtargets件数がevidenceへ正確に明示されていません: %q", result.Evidence)
	}
	if omittedArtifacts > 0 && !strings.Contains(result.Evidence, fmt.Sprintf("artifacts省略%d件", omittedArtifacts)) {
		t.Fatalf("省略したartifacts件数がevidenceへ正確に明示されていません: %q", result.Evidence)
	}
}

func TestPoCGoNoGoResultSummarizesUnfittablePassthrough(t *testing.T) {
	for _, tc := range []struct {
		name           string
		targetCount    int
		targetFiller   int
		artifacts      []string
		wantAllTargets bool
	}{
		{"targets極大でartifacts優先縮約", 4, 1374, []string{"/artifacts/poc/report-a.txt", "/artifacts/poc/report-b.txt"}, false},
		{"artifactsだけの縮約で収まる", 19, 217, []string{
			"/artifacts/poc/observation-report-" + strings.Repeat("a", 100) + ".txt",
			"/artifacts/poc/observation-report-" + strings.Repeat("b", 100) + ".txt",
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker := packet.Result{
				Status:              packet.StatusImplemented,
				Risk:                packet.RiskLow,
				Summary:             "観測した",
				RequirementCoverage: "covered",
				Tests:               "検証した",
				Unverified:          "none",
				Targets:             pocHeavyTargets(t, tc.targetCount, tc.targetFiller),
				Artifacts:           tc.artifacts,
			}
			if err := packet.ValidateWorkerResult(worker); err != nil {
				t.Fatalf("前提: PoC worker resultは現行validatorを通るべき: %v", err)
			}
			result := pocGoNoGoResult(worker)
			requireGoNoGoContract(t, result)
			if size := result.ByteSize(); size > packet.MaxPacketBytes {
				t.Fatalf("packet全体が%d bytes上限を超えています: %d bytes", packet.MaxPacketBytes, size)
			}
			requirePoCPassthroughSummarized(t, result, worker)
			if tc.wantAllTargets {
				if !slices.Equal(result.Targets, worker.Targets) {
					t.Fatalf("artifacts優先縮約では全targetsを完全保持すべきです: %d/%d", len(result.Targets), len(worker.Targets))
				}
				if len(result.Artifacts) >= len(worker.Artifacts) {
					t.Fatal("この形状ではartifactsの省略が発生しているべきです")
				}
			}
			if result.TestObligations != worker.Tests {
				t.Fatalf("短いtest obligationsはそのまま保持すべき: %q", result.TestObligations)
			}
		})
	}
}

func TestExternalFeasibilityPoCOverlongObservationEmitsValidGoNoGoPacket(t *testing.T) {
	for _, tc := range []struct {
		name   string
		worker packet.Result
	}{
		{"観測長文", packet.Result{
			Status:              packet.StatusImplemented,
			Risk:                packet.RiskLow,
			Summary:             strings.Repeat("観", 500),
			RequirementCoverage: "covered",
			Tests:               strings.Repeat("検", 400),
			Unverified:          strings.Repeat("残", 100),
		}},
		{"target-heavy観測", packet.Result{
			Status:              packet.StatusImplemented,
			Risk:                packet.RiskLow,
			Summary:             "観測した",
			RequirementCoverage: "covered",
			Tests:               strings.Repeat("検", 512),
			Unverified:          "none",
			Targets:             pocHeavyTargets(t, 12, 250),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repoRoot := initMutationRepo(t)
			writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
			w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
				{structured: packetBody(tc.worker)},
				{structured: passPacket()},
			}, "", 0, nil)

			if err := w.ExecuteNewTask("request"); err != nil {
				t.Fatal(err)
			}
			if len(r.prompts) != 1 {
				t.Fatalf("PoC taskはworker 1呼出だけ: %d(%v)", len(r.prompts), r.phases)
			}
			if st.TaskStatus() != state.TaskStatusWaitingDecision {
				t.Fatalf("status = %q want waiting-decision", st.TaskStatus())
			}
			pkt := lastPacketFromOutput(t, out.String())
			requireGoNoGoContract(t, pkt)
			wantTargets := tc.worker.Targets
			if len(wantTargets) == 0 {
				wantTargets = []string{"none"}
			}
			if !slices.Equal(pkt.Targets, wantTargets) {
				t.Fatalf("対象targetsが保持されていません: %v", pkt.Targets)
			}
			if !strings.HasPrefix(pkt.Evidence, pocOmissionMarker) {
				t.Fatalf("1536 bytes超の観測から生成されたevidenceには省略markerが必要です: %q", pkt.Evidence)
			}
		})
	}
}

func TestExternalFeasibilityPoCUnfittableTargetsEmitsValidGoNoGoPacket(t *testing.T) {
	repoRoot := initMutationRepo(t)
	writeFeasibilityActiveTask(t, repoRoot, feasibilityPoCDecl)
	worker := packet.Result{
		Status:              packet.StatusImplemented,
		Risk:                packet.RiskLow,
		Summary:             "観測した",
		RequirementCoverage: "covered",
		Tests:               "検証した",
		Unverified:          "none",
		Targets:             pocHeavyTargets(t, 4, 1394),
	}
	w, r, out, st := newPlanFileWorkflow(t, repoRoot, []runnerStep{
		{structured: packetBody(worker)},
		{structured: passPacket()},
	}, "", 0, nil)

	if err := w.ExecuteNewTask("request"); err != nil {
		t.Fatal(err)
	}
	if len(r.prompts) != 1 {
		t.Fatalf("PoC taskはworker 1呼出だけ: %d(%v)", len(r.prompts), r.phases)
	}
	if st.TaskStatus() != state.TaskStatusWaitingDecision {
		t.Fatalf("status = %q want waiting-decision", st.TaskStatus())
	}
	pkt := lastPacketFromOutput(t, out.String())
	requireGoNoGoContract(t, pkt)
	if size := pkt.ByteSize(); size > packet.MaxPacketBytes {
		t.Fatalf("packet全体が%d bytes上限を超えています: %d bytes", packet.MaxPacketBytes, size)
	}
	requirePoCPassthroughSummarized(t, pkt, withPoCDefaultTargets(worker))
	if len(pkt.Targets) == len(worker.Targets) {
		t.Fatal("極端帯ではtargetsの省略が発生しているべきです")
	}
}

func withPoCDefaultTargets(worker packet.Result) packet.Result {
	if len(worker.Targets) == 0 {
		worker.Targets = []string{"none"}
	}
	return worker
}
