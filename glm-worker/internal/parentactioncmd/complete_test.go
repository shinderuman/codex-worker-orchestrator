package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type completeFixture struct {
	cfg    config.AppConfig
	st     *state.StateStore
	repo   string
	remote string
}

func TestCompleteFinalizesAfterParentPush(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.RemoteSync == nil || output.RemoteSync.State != completeRemoteStateVerified || !output.RemoteSync.PostconditionMet ||
		output.RemoteSync.RemoteName != "origin" || output.RemoteSync.RemoteRef != "refs/heads/main" ||
		output.RemoteSync.ExpectedOID != completeFixtureHead(t, fixture.repo) {
		t.Fatalf("remote sync = %#v", output.RemoteSync)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("status = %q", got)
	}
	marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID)
	if err != nil || marker == nil {
		t.Fatalf("completion後のrotation marker = %#v err=%v", marker, err)
	}
}

func TestCompleteKeepsAwaitingWhileFinalHeadIsAhead(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Stage != "remote_sync" || output.Failure.Reason != pushBindingClassificationLocalAhead {
		t.Fatalf("failure = %#v", output.Failure)
	}
	if output.RemoteSync == nil || output.RemoteSync.State != pushBindingClassificationLocalAhead ||
		!output.RemoteSync.Applicable || output.RemoteSync.PostconditionMet ||
		output.RemoteSync.RemoteName != "origin" || output.RemoteSync.RemoteRef != "refs/heads/main" ||
		output.RemoteSync.ExpectedOID != completeFixtureHead(t, fixture.repo) || output.RemoteSync.RemoteOID == "" {
		t.Fatalf("remote sync = %#v", output.RemoteSync)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("status = %q", got)
	}

	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	retry := runCompleteCommand(t, fixture)
	if retry.Status != completeStatusComplete || !retry.Completed {
		t.Fatalf("push後の再試行 = %#v", retry)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("再試行後のstatus = %q", got)
	}
}

func TestCompleteKeepsAwaitingForRemoteFailures(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, fixture *completeFixture)
		reason  string
	}{
		{
			name: "network failure",
			arrange: func(t *testing.T, fixture *completeFixture) {
				runFinalizationGit(t, fixture.repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
			},
			reason: pushBindingClassificationNetworkFailure,
		},
		{
			name: "non fast forward",
			arrange: func(t *testing.T, fixture *completeFixture) {
				writePushBindingFile(t, fixture.repo, "side.txt", "side\n")
				runFinalizationGit(t, fixture.repo, "add", "side.txt")
				runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "side")
				runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
				runFinalizationGit(t, fixture.repo, "reset", "-q", "--hard", "HEAD~1")
			},
			reason: pushBindingClassificationNonFastForward,
		},
		{
			name: "remote ref mismatch",
			arrange: func(t *testing.T, fixture *completeFixture) {
				runFinalizationGit(t, fixture.remote, "update-ref", "-d", "refs/heads/main")
			},
			reason: pushBindingClassificationRemoteRefMismatch,
		},
		{
			name: "remote unresolvable",
			arrange: func(t *testing.T, fixture *completeFixture) {
				runFinalizationGit(t, fixture.repo, "config", "--unset", "remote.origin.url")
			},
			reason: completeTargetRemoteUnresolvable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCompleteFixture(t)
			fixture.commitParentMetadataSync(t)
			runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
			tc.arrange(t, fixture)

			output := runCompleteCommand(t, fixture)
			if output.Status != completeStatusAwaiting || output.Completed {
				t.Fatalf("output = %#v", output)
			}
			if output.Failure == nil || output.Failure.Reason != tc.reason {
				t.Fatalf("failure = %#v want %s", output.Failure, tc.reason)
			}
			if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
				t.Fatalf("status = %q", got)
			}
		})
	}
}

func TestCompleteKeepsAwaitingForLocalBoundaries(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, fixture *completeFixture)
		reason  string
	}{
		{
			name: "dirty tree",
			arrange: func(t *testing.T, fixture *completeFixture) {
				fixture.commitParentMetadataSync(t)
				runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
				writePushBindingFile(t, fixture.repo, "impl.txt", "impl\n")
			},
			reason: "tree_not_clean",
		},
		{
			name: "completed task file still tracked",
			arrange: func(t *testing.T, fixture *completeFixture) {
				writePushBindingFile(t, fixture.repo, "impl.txt", "impl\n")
				runFinalizationGit(t, fixture.repo, "add", "impl.txt")
				runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "impl")
				runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
			},
			reason: "completed_task_file_still_tracked",
		},
		{
			name: "metadata transition invalid",
			arrange: func(t *testing.T, fixture *completeFixture) {
				writePushBindingFile(t, fixture.repo, "impl.txt", "impl\n")
				runFinalizationGit(t, fixture.repo, "add", "-A")
				runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "impl")
				runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
				runFinalizationGit(t, fixture.repo, "rm", "-q", "IMPLEMENTATION_TASKS/active.md")
				writePushBindingFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", completeUnscheduledPlan())
				runFinalizationGit(t, fixture.repo, "add", "-A")
				runFinalizationGit(t, fixture.repo, "commit", "-q", "-m", "broken sync")
				runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
			},
			reason: "completion_transition_invalid",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newCompleteFixture(t)
			tc.arrange(t, fixture)

			output := runCompleteCommand(t, fixture)
			if output.Status != completeStatusAwaiting || output.Completed {
				t.Fatalf("output = %#v", output)
			}
			if output.Failure == nil || output.Failure.Reason != tc.reason {
				t.Fatalf("failure = %#v want %s", output.Failure, tc.reason)
			}
			if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
				t.Fatalf("status = %q", got)
			}
		})
	}
}

func TestCompleteTreatsMissingUpstreamAsNotApplicable(t *testing.T) {
	fixture := newCompleteFixture(t)
	runFinalizationGit(t, fixture.repo, "config", "--unset", "branch.main.remote")
	fixture.commitParentMetadataSync(t)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.RemoteSync == nil || output.RemoteSync.Applicable || output.RemoteSync.State != completeRemoteStateNotApplicable {
		t.Fatalf("remote sync = %#v", output.RemoteSync)
	}
}

func TestCompleteTreatsDetachedHeadWithoutPlanContractAsNotApplicable(t *testing.T) {
	cfg, st := newCompleteDetachedFixture(t)
	var stdout bytes.Buffer
	if err := runComplete(cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	var output completeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.RemoteSync == nil || output.RemoteSync.Applicable || output.RemoteSync.State != completeRemoteStateNotApplicable {
		t.Fatalf("remote sync = %#v", output.RemoteSync)
	}
	if got := st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("status = %q", got)
	}
}

func TestCompleteRejectsNonAwaitingStateAndArguments(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	if err := fixture.st.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	if err := runComplete(fixture.cfg, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "not admitted") {
		t.Fatalf("non-awaiting complete = %v", err)
	}

	if err := execute(fixture.cfg, []string{"complete", "extra"}, &bytes.Buffer{}, nil); err == nil ||
		!strings.Contains(err.Error(), "usage: glm-parent-action complete") {
		t.Fatalf("extra-argument complete = %v", err)
	}
}

func TestCompleteOutputNeverLeaksCredentialBearingRemoteURL(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	sentinel := "glmpc-sentinel-credential-0123"
	runFinalizationGit(t, fixture.repo, "remote", "set-url", "origin", "https://worker:"+sentinel+"@127.0.0.1:1/leak-probe.git")

	var stdout bytes.Buffer
	if err := runComplete(fixture.cfg, &stdout); err != nil {
		t.Fatal(err)
	}
	if raw := stdout.String(); strings.Contains(raw, sentinel) || strings.Contains(raw, "remote_url") {
		t.Fatalf("complete出力がcredentialを漏らしました: %s", raw)
	}
}

func TestCompleteBindsNoGoTerminalToParentPush(t *testing.T) {
	fixture := newNoGoCompleteFixture(t)
	fixture.commitParentMetadataSync(t)

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("local-only metadataでのoutput = %#v", output)
	}
	if output.Failure == nil || output.Failure.Reason != pushBindingClassificationLocalAhead {
		t.Fatalf("failure = %#v", output.Failure)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("status = %q", got)
	}
	if _, admitted, err := fixture.st.AdmitNewTask(); err != nil || admitted {
		t.Fatalf("awaiting中新taskが許可されました: admitted=%v err=%v", admitted, err)
	}
	if marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID); err != nil || marker != nil {
		t.Fatalf("remote未同期のno-goでrotation marker = %#v err=%v", marker, err)
	}

	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	finalized := runCompleteCommand(t, fixture)
	if finalized.Status != completeStatusComplete || !finalized.Completed {
		t.Fatalf("push後のno-go completion = %#v", finalized)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusComplete {
		t.Fatalf("completion status = %q", got)
	}
	marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID)
	if err != nil || marker == nil || marker.LastEvaluation == nil || marker.LastEvaluation.Terminal != state.SessionRotationTerminalNoGo {
		t.Fatalf("no-go completion後のmarker = %#v err=%v", marker, err)
	}
}

func TestVerifyCompletionUnchangedDetectsRaceBeforeTransition(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	verifiedHead := completeFixtureHead(t, fixture.repo)

	if failure := verifyCompletionUnchanged(fixture.repo, true, verifiedHead); failure != nil {
		t.Fatalf("不変状態の再確認 = %#v", failure)
	}
	if failure := verifyCompletionUnchanged(fixture.repo, false, verifiedHead); failure != nil {
		t.Fatalf("非Git再確認 = %#v", failure)
	}

	writePushBindingFile(t, fixture.repo, "race.txt", "race\n")
	if failure := verifyCompletionUnchanged(fixture.repo, true, verifiedHead); failure == nil || failure.Reason != completeFailureTreeChanged {
		t.Fatalf("tree変化の再確認 = %#v", failure)
	}

	runFinalizationGit(t, fixture.repo, "rm", "-q", "--cached", "--ignore-unmatch", "race.txt")
	if err := os.Remove(filepath.Join(fixture.repo, "race.txt")); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, fixture.repo, "commit", "-q", "--allow-empty", "-m", "moved")
	movedHead := completeFixtureHead(t, fixture.repo)
	if failure := verifyCompletionUnchanged(fixture.repo, true, verifiedHead); failure == nil || failure.Reason != completeFailureHeadChanged {
		t.Fatalf("HEAD移動の再確認 = %#v", failure)
	}
	if failure := verifyCompletionUnchanged(fixture.repo, true, movedHead); failure != nil {
		t.Fatalf("移動後HEADの再確認 = %#v", failure)
	}
}

func TestCompleteRestoresNoGoTerminalFromOutcomeEvidenceAfterStatsLoss(t *testing.T) {
	fixture := newNoGoCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	fixture.st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.CompletionTerminal = ""
		stats.AcceptedRisk = ""
	})

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("evidence復旧後のoutput = %#v", output)
	}
	stats, err := fixture.st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.CompletionTerminal != state.SessionRotationTerminalNoGo || stats.AcceptedRisk != string(packet.RiskHigh) {
		t.Fatalf("復旧後stats = terminal:%s risk:%s", stats.CompletionTerminal, stats.AcceptedRisk)
	}
	marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID)
	if err != nil || marker == nil || marker.LastEvaluation == nil || marker.LastEvaluation.Terminal != state.SessionRotationTerminalNoGo {
		t.Fatalf("復旧後marker = %#v err=%v", marker, err)
	}
}

func TestCompleteFailsClosedWhenTerminalEvidenceIsMissing(t *testing.T) {
	fixture := newCompleteRepositoryFixture(t)
	if err := fixture.st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Stage != "state" || output.Failure.Reason != completeFailureTerminalUnrecoverable {
		t.Fatalf("failure = %#v", output.Failure)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("status = %q", got)
	}
	if marker, err := fixture.st.LoadSessionRotationMarker(codexIdentityTestThreadID); err != nil || marker != nil {
		t.Fatalf("fail closed後のmarker = %#v err=%v", marker, err)
	}
}

func TestCompleteFailsClosedWhenAwaitingStatsAreUnreadable(t *testing.T) {
	fixture := newCompleteFixture(t)
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")
	if err := os.WriteFile(fixture.st.Path("task-stats.json"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusAwaiting || output.Completed {
		t.Fatalf("output = %#v", output)
	}
	if output.Failure == nil || output.Failure.Reason != completeFailureStatsUnreadable {
		t.Fatalf("failure = %#v", output.Failure)
	}
	if got := fixture.st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("status = %q", got)
	}
}

func newNoGoCompleteFixture(t *testing.T) *completeFixture {
	t.Helper()
	fixture := newCompleteRepositoryFixture(t)
	st := fixture.st
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/active.md", []byte("# active\n\n## External feasibility\n\nstatus: observation\nassumption: representative producer behavior\n")); err != nil {
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
	if err := runNoGo(fixture.cfg, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func newCompleteRepositoryFixture(t *testing.T) *completeFixture {
	t.Helper()
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)

	repo := t.TempDir()
	remote := filepath.Join(t.TempDir(), "remote.git")
	if err := os.MkdirAll(remote, 0o700); err != nil {
		t.Fatal(err)
	}
	runFinalizationGit(t, repo, "init", "-q", "-b", "main")
	runFinalizationGit(t, repo, "config", "user.name", "complete test")
	runFinalizationGit(t, repo, "config", "user.email", "complete@example.invalid")
	runFinalizationGit(t, remote, "init", "-q", "--bare", "-b", "main")
	if err := os.MkdirAll(filepath.Join(repo, "IMPLEMENTATION_TASKS"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, repo, "IMPLEMENTATION_PLAN.local.md", completeInitialPlan())
	writePushBindingFile(t, repo, "IMPLEMENTATION_TASKS/active.md", "# active\n\n## External feasibility\n\nstatus: not-applicable\n")
	writePushBindingFile(t, repo, "IMPLEMENTATION_TASKS/next.md", "# next\n\n## External feasibility\n\nstatus: not-applicable\n")
	runFinalizationGit(t, repo, "add", "-A")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "initial")
	runFinalizationGit(t, repo, "remote", "add", "origin", remote)
	runFinalizationGit(t, repo, "push", "-q", "-u", "origin", "main")

	cfg, _ := newParentActionTestState(t)
	cfg.RepoRoot = repo
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexIdentityTestThreadID, codexIdentityTestThreadID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/active.md"); err != nil {
		t.Fatal(err)
	}
	return &completeFixture{cfg: cfg, st: st, repo: repo, remote: remote}
}

func newCompleteFixture(t *testing.T) *completeFixture {
	t.Helper()
	fixture := newCompleteRepositoryFixture(t)
	if err := fixture.st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := fixture.st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func newCompleteDetachedFixture(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	t.Setenv("CODEX_THREAD_ID", codexIdentityTestThreadID)
	t.Setenv("CODEX_SESSION_ID", codexIdentityTestThreadID)

	repo := t.TempDir()
	runFinalizationGit(t, repo, "init", "-q", "-b", "main")
	runFinalizationGit(t, repo, "config", "user.name", "complete test")
	runFinalizationGit(t, repo, "config", "user.email", "complete@example.invalid")
	writePushBindingFile(t, repo, "impl.txt", "impl\n")
	runFinalizationGit(t, repo, "add", "-A")
	runFinalizationGit(t, repo, "commit", "-q", "-m", "impl")
	runFinalizationGit(t, repo, "checkout", "-q", "--detach")

	cfg, _ := newParentActionTestState(t)
	cfg.RepoRoot = repo
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexIdentityTestThreadID, codexIdentityTestThreadID, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func (f *completeFixture) commitParentMetadataSync(t *testing.T) {
	t.Helper()
	if err := os.Remove(filepath.Join(f.repo, "IMPLEMENTATION_TASKS", "active.md")); err != nil {
		t.Fatal(err)
	}
	writePushBindingFile(t, f.repo, "IMPLEMENTATION_PLAN.local.md", completePromotedPlan())
	runFinalizationGit(t, f.repo, "add", "-A")
	runFinalizationGit(t, f.repo, "commit", "-q", "-m", "completion sync")
}

func runCompleteCommand(t *testing.T, fixture *completeFixture) completeOutput {
	t.Helper()
	var stdout bytes.Buffer
	if err := runComplete(fixture.cfg, &stdout); err != nil {
		t.Fatalf("runComplete: %v: %s", err, stdout.String())
	}
	var output completeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("complete出力がmachine JSONではありません: %v: %s", err, stdout.String())
	}
	return output
}

func completeInitialPlan() string {
	return "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/active.md`\n\n" +
		"## NEXT（優先順）\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
		"## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在の停止理由\n\nなし\n"
}

func completePromotedPlan() string {
	return "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/next.md`\n\n" +
		"## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在の停止理由\n\nなし\n"
}

func completeUnscheduledPlan() string {
	return "# plan\n\n## ACTIVE\n\n## NEXT（優先順）\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n" +
		"## 現在の停止理由\n\nなし\n"
}

func completeFixtureHead(t *testing.T, repo string) string {
	t.Helper()
	return strings.TrimSpace(pushBindingGitOutput(t, repo, "rev-parse", "HEAD"))
}
