package app

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeStatusOutput(t *testing.T, cfg config.AppConfig) statusOutput {
	t.Helper()
	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeStatus}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var output statusOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("--status出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	return output
}

func statusString(t *testing.T, name string, value *string, want string) {
	t.Helper()
	if value == nil {
		t.Fatalf("status出力の%sがnullです", name)
	}
	if *value != want {
		t.Fatalf("status出力の%s = %q want %q", name, *value, want)
	}
}

func statusNullString(t *testing.T, name string, value *string) {
	t.Helper()
	if value != nil {
		t.Fatalf("status出力の%s = %q want null", name, *value)
	}
}

func statusInt64MS(t *testing.T, name string, value *int64, want int64) {
	t.Helper()
	if value == nil {
		t.Fatalf("status出力の%sがnullです", name)
	}
	if *value != want {
		t.Fatalf("status出力の%s = %d want %d", name, *value, want)
	}
}

func TestExecuteStatusShowsDetailFromEventLogAndTelemetry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}

	writeTaskEventLines(t, st, taskID,
		state.TaskEventRecord{TaskID: taskID, CallID: "call-1", Role: "worker", Phase: "worker-new", ModelAlias: "opus", Kind: "system", Subtype: "init", MessageModel: "glm-5.3"},
		state.TaskEventRecord{
			TaskID:     taskID,
			CallID:     "call-1",
			Role:       "worker",
			Phase:      "worker-new",
			ModelAlias: "opus",
			Kind:       "user",
			Timestamp:  time.Now().UTC(),
			Blocks:     []state.TaskBlockSummary{{Type: "tool_result", Name: "Bash", ToolID: "toolu_1", Bytes: 123, DurationMS: 456}},
		},
	)

	startedAt := time.Now().UTC().Add(-time.Hour)
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:         taskID,
		CallType:       state.CallTypeTask,
		SessionID:      "sess-worker",
		Role:           state.WorkerRole,
		ModelAlias:     "opus",
		StartedAt:      startedAt,
		CompletedAt:    startedAt.Add(8 * time.Second),
		TopLevelTurns:  10,
		TreeUsage:      state.TokenUsage{InputTokens: 100, CacheReadInputTokens: 50, OutputTokens: 20},
		WallDurationMS: 8000,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:         taskID,
		CallType:       state.CallTypeTask,
		SessionID:      "sess-worker",
		Role:           state.WorkerRole,
		ModelAlias:     "opus",
		Resumed:        true,
		StartedAt:      startedAt.Add(10 * time.Minute),
		CompletedAt:    startedAt.Add(10*time.Minute + 9*time.Second),
		TopLevelTurns:  12,
		TreeUsage:      state.TokenUsage{InputTokens: 300, OutputTokens: 30},
		WallDurationMS: 9000,
	})
	st.RecordModelCallLog(state.ModelCallLog{
		TaskID:         taskID,
		CallType:       state.CallTypeProbe,
		SessionID:      "none",
		Role:           state.WorkerRole,
		ModelAlias:     "opus",
		StartedAt:      startedAt.Add(20 * time.Minute),
		CompletedAt:    startedAt.Add(20 * time.Minute),
		Outcome:        "probe_failure",
		ProbeAttempt:   2,
		WallDurationMS: 1500,
	})

	output := executeStatusOutput(t, cfg)
	statusString(t, "current_phase", output.CurrentPhase, "worker-new")
	statusString(t, "current_role", output.CurrentRole, "worker")
	statusString(t, "current_model", output.CurrentModel, "opus")
	if output.TaskStartedAt == nil || output.TaskElapsedMS == nil || output.LastEventAgeMS == nil {
		t.Fatalf("観測できる値がnullです: %#v", output)
	}
	if output.LastEvent == nil || len(output.LastEvent.Blocks) != 1 {
		t.Fatalf("last_event = %#v", output.LastEvent)
	}
	block := output.LastEvent.Blocks[0]
	if block.Type != "tool_result" || block.Name != "Bash" || block.Bytes != 123 || block.DurationMS != 456 {
		t.Fatalf("last_eventのblock = %#v", block)
	}
	if len(output.SessionAging) != 1 {
		t.Fatalf("session_aging = %#v", output.SessionAging)
	}
	aging := output.SessionAging[0]
	if aging.SessionID != "sess-worker" || aging.Role != state.WorkerRole || aging.Calls != 2 || aging.ResumedCalls != 1 ||
		aging.CumulativeTurns != 22 || aging.CumulativeInputTokens != 450 || aging.CumulativeOutputTokens != 50 {
		t.Fatalf("session_aging = %#v", aging)
	}
	if len(aging.Models) != 1 || aging.Models[0] != "opus" {
		t.Fatalf("session_agingのmodels = %#v", aging.Models)
	}
	if len(aging.CallLatencyMS) != 2 || aging.CallLatencyMS[0] != 8000 || aging.CallLatencyMS[1] != 9000 {
		t.Fatalf("session_agingのcall_latency_ms = %#v", aging.CallLatencyMS)
	}
	if output.Probes == nil || output.Probes.Count != 1 {
		t.Fatalf("probes = %#v", output.Probes)
	}
	if output.Probes.LastOutcome == nil || *output.Probes.LastOutcome != "probe_failure" {
		t.Fatalf("probes.last_outcome = %#v", output.Probes.LastOutcome)
	}
	if output.Probes.LastAttempt != 2 {
		t.Fatalf("probes.last_attempt = %d", output.Probes.LastAttempt)
	}
}

func TestExecuteStatusDetailFallsBackToCheckpoint(t *testing.T) {
	cases := []struct {
		name   string
		seed   func(st *state.StateStore)
		status state.TaskStatus
		check  func(t *testing.T, output statusOutput)
	}{
		{
			name: "provider unavailable",
			seed: func(st *state.StateStore) {
				if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
					Stage:                             state.ResumeStageWorker,
					Phase:                             "worker-new",
					Role:                              state.WorkerRole,
					Model:                             "opus",
					Prompt:                            "p",
					Request:                           "req",
					ProviderUnavailable:               true,
					ProviderUnavailableClassification: "http-503",
					ProviderUnavailableProbes:         3,
					ProviderUnavailableStartedAt:      time.Now().UTC().Add(-20 * time.Minute),
				}); err != nil {
					t.Fatal(err)
				}
			},
			status: state.TaskStatusProviderUnavailable,
			check: func(t *testing.T, output statusOutput) {
				t.Helper()
				if !output.ProviderUnavailable.Unavailable {
					t.Fatalf("provider_unavailable = %#v", output.ProviderUnavailable)
				}
				statusString(t, "provider_unavailable.phase", &output.ProviderUnavailable.Phase, "worker-new")
				if output.ProviderUnavailable.Classification == nil || *output.ProviderUnavailable.Classification != "http-503" {
					t.Fatalf("provider_unavailable.classification = %#v", output.ProviderUnavailable.Classification)
				}
				if output.ProviderUnavailable.Probes != 3 {
					t.Fatalf("provider_unavailable.probes = %d", output.ProviderUnavailable.Probes)
				}
				if !output.ResumeAvailable {
					t.Fatal("provider停止中はresume_availableが必要です")
				}
			},
		},
		{
			name: "rate limited",
			seed: func(st *state.StateStore) {
				if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
					Stage:          state.ResumeStageReview,
					Phase:          "reviewer-1",
					Role:           state.ReviewerRole,
					Model:          "haiku",
					Prompt:         "p",
					Request:        "req",
					RateLimited:    true,
					ResetAtCST:     "2026-08-16 14:06:34",
					ResetAtRFC3339: "2026-08-16T14:06:34+08:00",
				}); err != nil {
					t.Fatal(err)
				}
			},
			status: state.TaskStatusRateLimited,
			check: func(t *testing.T, output statusOutput) {
				t.Helper()
				if !output.RateLimited.Limited {
					t.Fatalf("rate_limited = %#v", output.RateLimited)
				}
				statusString(t, "rate_limited.phase", &output.RateLimited.Phase, "reviewer-1")
				if output.RateLimited.ResetAtRFC3339 == nil || *output.RateLimited.ResetAtRFC3339 != "2026-08-16T14:06:34+08:00" {
					t.Fatalf("rate_limited.reset_at_rfc3339 = %#v", output.RateLimited.ResetAtRFC3339)
				}
				if !output.ResumeAvailable {
					t.Fatal("rate limit中はresume_availableが必要です")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			c.seed(st)
			if err := st.SetTaskStatus(c.status); err != nil {
				t.Fatal(err)
			}

			output := executeStatusOutput(t, cfg)
			if output.LastEvent != nil {
				t.Fatalf("event logがないのにlast_event = %#v", output.LastEvent)
			}
			if len(output.SessionAging) != 0 {
				t.Fatalf("呼出記録がないのにsession_aging = %#v", output.SessionAging)
			}
			if output.CurrentPhase == nil {
				t.Fatal("checkpointがあるのにcurrent_phaseがnullです")
			}
			c.check(t, output)
		})
	}
}

func TestExecuteStatusEmptyTaskDetailIsExplicitUnknown(t *testing.T) {
	cfg := newAppConfig(t)
	output := executeStatusOutput(t, cfg)
	if output.TaskStartedAt != nil || output.TaskElapsedMS != nil {
		t.Fatalf("空状態の開始観測 = %#v", output)
	}
	if output.CurrentPhase != nil || output.CurrentRole != nil || output.CurrentModel != nil {
		t.Fatalf("空状態のcurrent表示 = %#v", output)
	}
	if output.LastEvent != nil {
		t.Fatalf("空状態のlast_event = %#v", output.LastEvent)
	}
	if output.SessionAging != nil {
		t.Fatalf("空状態のsession_aging = %#v", output.SessionAging)
	}
	if output.Probes != nil {
		t.Fatalf("probe記録がないのにprobe表示 = %#v", output.Probes)
	}
}
