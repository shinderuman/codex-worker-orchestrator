package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRetiredSuccessorStartupCost(t *testing.T) {
	cfg, admission, store, policy := retiredWorkflowCorpus(t, true)
	cfg.ClaudeConfigDir = t.TempDir()
	payload := make([]byte, 2048)
	random := rand.New(rand.NewSource(1))
	for index := range 260 {
		if _, err := random.Read(payload); err != nil {
			t.Fatal(err)
		}
		writeAppTestFile(t, cfg.RepoRoot, fmt.Sprintf("startup/%03d.bin", index), string(payload))
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	admission, err = store.RecordAdmittedMutation(admission, "test:startup-source", "success", snapshot)
	if err != nil {
		t.Fatal(err)
	}
	evidence := canonicalWorkflowEvidence(t, cfg, store, admission, policy)
	accepted, err := store.AcceptExecutionCandidate(admission, controller.CandidateAcceptanceInput{RequiredValidations: declaredNoValidations(), Message: "startup predecessor", Policy: policy, Evidence: evidence})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	completeAwaitedRuntimeThroughProductionAPIs(t, cfg, admission)
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.PublishAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}

	trace := filepath.Join(t.TempDir(), "startup-git.jsonl")
	t.Setenv("GIT_TRACE2_EVENT", trace)
	stubQualityPreflight(t, func(string) error { return nil })
	started := time.Now()
	run := runCanonicalWorkflow(t, cfg, ModeNewTask, "startup cost regression", "IMPLEMENTATION_TASKS/successor.md", "")
	elapsed := time.Since(started)
	st := state.AttachStateStore(run.cfg)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	stages, err := st.ReadStageLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range stages {
		if record.Stage != nil && record.Outcome == state.StageOutcomeCompleted {
			t.Logf("startup stage=%s duration_ms=%d", record.Stage.Stage, record.WallDurationMS)
		}
	}
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var event struct {
			Event string   `json:"event"`
			Argv  []string `json:"argv"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Event == "start" && len(event.Argv) > 0 {
			for _, arg := range event.Argv {
				if arg == "cat-file" {
					count++
					break
				}
			}
		}
	}
	t.Logf("startup boundary=Execute-to-first-model-run elapsed_ms=%d cat_file_processes=%d model_stub_calls=%d provider_calls=0", elapsed.Milliseconds(), count, len(run.prompts))
	if count == 0 || count > 100 {
		t.Fatalf("startup blob work scales with file count: cat-file processes=%d", count)
	}
	for index := range 260 {
		path := fmt.Sprintf("startup/%03d.bin", index)
		want, err := os.ReadFile(filepath.Join(cfg.RepoRoot, path))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(run.cfg.RepoRoot, path))
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("startup source changed at %s: %v", path, err)
		}
	}
}
