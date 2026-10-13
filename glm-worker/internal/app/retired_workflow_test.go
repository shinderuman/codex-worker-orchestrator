package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactioncmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRetiredWorkflowBootstrapAndFreshHandoffAdmitMetadataSyncThenStart(t *testing.T) {
	cfg, store, retired, policy := retiredWorkflowFixture(t)
	t.Setenv("GLM_WORKER_HOME", filepath.Dir(cfg.StateBase))
	assertRetiredBootstrap(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md")
	cfg, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	st := state.AttachStateStore(cfg)
	if st.ReadOr("repo-root", "") != "" {
		t.Fatal("successor fixture already initialized runtime")
	}
	handoff := buildParentHandoffWithConfig(cfg, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" {
		t.Fatalf("retired successor handoff: %#v", handoff)
	}
	content, err := os.ReadFile(filepath.Join(cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md", string(content)+"\n親metadataの追加要求\n")
	runControllerActivationGit(t, cfg.RepoRoot, "add", "IMPLEMENTATION_TASKS/successor.md")
	assertRetiredParentMaintenanceGateAdmits(t, cfg.RepoRoot)
	runControllerActivationGit(t, cfg.RepoRoot, "commit", "-qm", "successor amendment")
	assertRetiredBootstrap(t, cfg.RepoRoot, "IMPLEMENTATION_TASKS/successor.md")
	handoff = buildParentHandoffWithConfig(cfg, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "controller-publication" {
		t.Fatalf("metadata synchronization handoff: %#v", handoff)
	}
	raw, err := json.Marshal(handoff)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		AllowedActions []string `json:"allowed_actions"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	if containsParentAction(projected.AllowedActions, "start") || containsParentAction(projected.AllowedActions, "start-milestones") {
		t.Fatal("start bypassed pending project synchronization")
	}
	adopted, err := store.AdoptExternalAdvancement(controller.ExternalAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	handoff = buildParentHandoffWithConfig(cfg, st)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != "start" || adopted.Head.LiveLeaseID != "" {
		t.Fatalf("synchronized successor handoff: %#v", handoff)
	}
	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetireTerminalTask(controller.TerminalTaskInput{ExpectedGeneration: retired.Head.ControllerGeneration, ProjectSnapshotID: retired.Head.ProjectSnapshotID, CandidateID: "stale", TaskRef: *retired.Head.RootTaskRef}); err == nil {
		t.Fatal("retired controller accepted a stale terminal operation")
	}
}

func assertRetiredParentMaintenanceGateAdmits(t *testing.T, root string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	code := parentactioncmd.Run([]string{"continuation-metadata-guard"}, io.Discard, io.Discard)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("continuation metadata guard rejected post-retire parent maintenance: exit=%d", code)
	}
}

func assertRetiredBootstrap(t *testing.T, root, task string) {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, runErr := runAuthorityBootstrap([]string{"--authority", "bootstrap"}, &out)
	if err := os.Chdir(original); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatal(runErr)
	}
	var result struct {
		ActiveTask string `json:"active_task"`
		Hash       string `json:"authority_snapshot_sha256"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ActiveTask != task || result.Hash == "" {
		t.Fatalf("bootstrap authority: %#v", result)
	}
}

func retiredWorkflowFixture(t *testing.T) (config.AppConfig, *controller.Store, controller.ExecutionOperationResult, controller.PublicationPolicy) {
	t.Helper()
	cfg, admission, store, policy := retiredWorkflowCorpus(t)
	evidence := canonicalWorkflowEvidence(t, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{Message: "root result", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	retired, err := store.RetireTerminalTask(controller.TerminalTaskInput{ExpectedGeneration: published.Head.ControllerGeneration, ProjectSnapshotID: published.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	runControllerActivationGit(t, cfg.RepoRoot, "add", "--all")
	return cfg, store, retired, policy
}
