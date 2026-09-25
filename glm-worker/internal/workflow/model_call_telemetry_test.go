package workflow

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunModelRecordsPromptResponseAndUsage(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{
		structured: implementedPacket("done"),
		result: runner.RunResult{
			SessionID: "worker-session",
			TopLevelUsage: runner.TokenUsage{
				InputTokens:          1,
				CacheReadInputTokens: 2,
				OutputTokens:         3,
			},
			ModelUsage: map[string]runner.ModelUsage{
				"glm-5.3": {InputTokens: 10, CacheCreationInputTokens: 20, CacheReadInputTokens: 30, OutputTokens: 40},
				"glm-4.7": {InputTokens: 5, CacheReadInputTokens: 7, OutputTokens: 8},
			},
			DurationMS:    1200,
			DurationAPIMS: 900,
			TopLevelTurns: 2,
			SystemPrompt:  "worker system instruction",
		},
	}}}
	w := newWorkflowT(t, st, r)
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageWorker,
		Phase:  "worker-new",
		Role:   state.WorkerRole,
		Model:  "opus",
		Effort: "high",
		Prompt: "implementation instruction",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("telemetry logs = %#v", logs)
	}
	got := logs[0]
	wantResponse := implementedPacket("done")
	if !strings.HasPrefix(got.Prompt, "implementation instruction\n\nREPORT_ARTIFACT_DIR: ") || got.SystemPrompt != "worker system instruction" || got.Response != wantResponse {
		t.Fatalf("telemetry content = %#v", got)
	}
	if !strings.Contains(r.prompts[0], artifactPromptMarker) {
		t.Fatalf("artifact保存先がrunner promptにありません: %q", r.prompts[0])
	}
	if len(r.phases) != 1 || r.phases[0] != "worker-new" {
		t.Fatalf("runnerへ渡したphase = %#v", r.phases)
	}
	if got.TopLevelUsage.CacheReadInputTokens != 2 || got.TreeUsage.CacheReadInputTokens != 37 || got.ResolvedModelUsage["glm-5.3"].OutputTokens != 40 {
		t.Fatalf("telemetry usage = %#v", got)
	}
	stats := currentStats(t, st)
	if stats.CacheReadInputTokensByAlias["opus"] != 37 || stats.OutputTokensByAlias["opus"] != 48 || stats.OutputTokensByResolvedModel["glm-5.3"] != 40 {
		t.Fatalf("token stats = %#v", stats)
	}
}

func TestRunModelCanOmitTelemetryContent(t *testing.T) {
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: []runnerStep{{structured: implementedPacket("done")}}}
	w := newWorkflowT(t, st, r)
	w.config.TelemetryContent = false
	w.temp = t.TempDir()

	_, err := w.runModel(state.ResumeCheckpoint{
		Stage:  state.ResumeStageWorker,
		Phase:  "worker-new",
		Role:   state.WorkerRole,
		Model:  "opus",
		Effort: "high",
		Prompt: "secret instruction",
	})
	if err != nil {
		t.Fatal(err)
	}
	taskID, _ := st.TaskID()
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if logs[0].Prompt != "" || logs[0].SystemPrompt != "" || logs[0].Response != "" || logs[0].PromptSHA256 == "" || logs[0].ResponseSHA256 == "" {
		t.Fatalf("content無効時のtelemetry = %#v", logs[0])
	}
}
