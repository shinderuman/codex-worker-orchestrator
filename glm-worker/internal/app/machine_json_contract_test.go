package app

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// decodeSingleLineJSONはmachine JSON 1行をdecode済みmapへ返す。typed structのzero
// valueとJSON null・field omissionを区別するため、契約確認はこのraw JSONに対して行う。
func decodeSingleLineJSON(t *testing.T, rendered string) map[string]any {
	t.Helper()
	raw := strings.TrimSpace(rendered)
	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("machine JSON 1行としてdecodeできません: %v: %q", err, rendered)
	}
	return decoded
}

// statusRawJSONは--status出力1行のraw JSONを返す。
func statusRawJSON(t *testing.T, cfg config.AppConfig) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeStatus}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

// statsRawJSONは--stats出力1行のraw JSONを返す。
func statsRawJSON(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printStats(st, &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

// requireJSONKeyはkeyの省略ではなくnullとして存在することを検査する。
func requireJSONKey(t *testing.T, decoded map[string]any, key string) any {
	t.Helper()
	value, ok := decoded[key]
	if !ok {
		t.Fatalf("machine JSONの%qが省略されています: %v", key, decoded)
	}
	return value
}

// assertNullJSONValueは値がJSON nullであることを検査する。
func assertNullJSONValue(t *testing.T, key string, value any) {
	t.Helper()
	if value != nil {
		t.Fatalf("machine JSONの%q = %#v want null", key, value)
	}
}

// assertEnumJSONValueは値が指定enum集合内の文字列であることを検査する。
func assertEnumJSONValue(t *testing.T, key string, value any, allowed ...string) {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("machine JSONの%q = %#v want enum %v", key, value, allowed)
	}
	for _, candidate := range allowed {
		if text == candidate {
			return
		}
	}
	t.Fatalf("machine JSONの%q = %q want enum %v", key, text, allowed)
}

// assertNoPresentationSentinelは対象fieldへ"none"/"unknown"のpresentation sentinelが
// 漏れていないことを検査する。
func assertNoPresentationSentinel(t *testing.T, decoded map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		text, _ := decoded[key].(string)
		if text == "none" || text == "unknown" {
			t.Fatalf("machine JSONの%qにpresentation sentinel %qが出ています", key, text)
		}
	}
}

// TestStatusRawJSONContractは--status機械契約の対象enum fieldをraw JSONで固定する。
// task不在・非active・activeの全境界で、観測できない値がJSON文字列のsentinelでも
// key omissionでもなくnullであること、観測できる値がenum値であることを検証する。
func TestStatusRawJSONContract(t *testing.T) {
	statusFields := []string{"task_status", "task_liveness", "repository_lock", "lock_pid"}

	t.Run("task不在", func(t *testing.T) {
		cfg := newAppConfig(t)
		decoded := statusRawJSON(t, cfg)
		assertNullJSONValue(t, "task_status", requireJSONKey(t, decoded, "task_status"))
		assertNullJSONValue(t, "task_liveness", requireJSONKey(t, decoded, "task_liveness"))
		assertEnumJSONValue(t, "repository_lock", requireJSONKey(t, decoded, "repository_lock"), "held", "free")
		assertNullJSONValue(t, "lock_pid", requireJSONKey(t, decoded, "lock_pid"))
		assertNoPresentationSentinel(t, decoded, statusFields...)
	})

	t.Run("非active task", func(t *testing.T) {
		cfg := newAppConfig(t)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
			t.Fatal(err)
		}

		decoded := statusRawJSON(t, cfg)
		assertEnumJSONValue(t, "task_status", requireJSONKey(t, decoded, "task_status"),
			"active", "waiting-decision", "waiting-sol-review", "complete", "rate-limited", "provider-unavailable")
		assertNullJSONValue(t, "task_liveness", requireJSONKey(t, decoded, "task_liveness"))
		assertEnumJSONValue(t, "repository_lock", requireJSONKey(t, decoded, "repository_lock"), "held", "free")
		assertNoPresentationSentinel(t, decoded, statusFields...)
	})

	t.Run("active + lock free", func(t *testing.T) {
		cfg := newAppConfig(t)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}

		decoded := statusRawJSON(t, cfg)
		assertEnumJSONValue(t, "task_status", requireJSONKey(t, decoded, "task_status"), "active")
		assertEnumJSONValue(t, "task_liveness", requireJSONKey(t, decoded, "task_liveness"), "running", "stale")
		assertEnumJSONValue(t, "repository_lock", requireJSONKey(t, decoded, "repository_lock"), "held", "free")
		assertNoPresentationSentinel(t, decoded, statusFields...)
	})
}

// knownTaskStatusesはtask_statusの現行外部enum。--status・--stats・--timeline・
// --convergenceのtask status受理集合はこの7値とnullだけである。
var knownTaskStatuses = []string{
	"active",
	"waiting-decision",
	"waiting-sol-review",
	"complete",
	"rate-limited",
	"provider-unavailable",
	"interrupted",
}

// timelineRawJSONは--timeline出力1行(現在task)のraw JSONを返す。
func timelineRawJSON(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printTimeline(st, "", &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

// convergenceRawJSONは--convergence出力1行(現在task)のraw JSONを返す。
func convergenceRawJSON(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := printConvergence(st, "", &out); err != nil {
		t.Fatal(err)
	}
	return decodeSingleLineJSON(t, out.String())
}

// statsCurrentTaskJSONは--stats出力のcurrent_task objectを返す。
func statsCurrentTaskJSON(t *testing.T, st *state.StateStore) map[string]any {
	t.Helper()
	decoded := statsRawJSON(t, st)
	currentTask, ok := decoded["current_task"].(map[string]any)
	if !ok {
		t.Fatalf("current_taskがJSON objectではありません: %#v", decoded["current_task"])
	}
	return currentTask
}

// taskStatusSurfacesは同一意味のtask statusをmachine JSONへ出す全production surfaceの
// raw JSON読み取り。
var taskStatusSurfaces = []struct {
	name string
	read func(t *testing.T, cfg config.AppConfig, st *state.StateStore) any
}{
	{"--status.task_status", func(t *testing.T, cfg config.AppConfig, st *state.StateStore) any {
		return requireJSONKey(t, statusRawJSON(t, cfg), "task_status")
	}},
	{"--stats.current_task.status", func(t *testing.T, cfg config.AppConfig, st *state.StateStore) any {
		return requireJSONKey(t, statsCurrentTaskJSON(t, st), "status")
	}},
	{"--timeline.task_status", func(t *testing.T, cfg config.AppConfig, st *state.StateStore) any {
		return requireJSONKey(t, timelineRawJSON(t, st), "task_status")
	}},
	{"--convergence.task_status", func(t *testing.T, cfg config.AppConfig, st *state.StateStore) any {
		return requireJSONKey(t, convergenceRawJSON(t, st), "task_status")
	}},
}

// TestTaskStatusFiniteEnumBoundaryはtask_status外部受理集合を現行7値とnullだけへ固定する。
// --status・--stats・--timeline・--convergenceの全producerで、既知7値がそのまま出ることと、
// 永続task.statusへ直接書いた未知値が契約外string・presentation sentinelとして漏れないことを
// raw JSONで検証する。
func TestTaskStatusFiniteEnumBoundary(t *testing.T) {
	for _, known := range knownTaskStatuses {
		t.Run(known, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatus(known)); err != nil {
				t.Fatal(err)
			}
			for _, surface := range taskStatusSurfaces {
				if got := surface.read(t, cfg, st); got != known {
					t.Fatalf("%s = %#v want %q", surface.name, got, known)
				}
			}
		})
	}

	// 未知永続値は未観測扱いへ正規化され、presentation sentinelへも変換されない。
	// "none"は内部sentinel相当の永続値が外部へ出ないことの境界確認用。
	for _, unknown := range []string{"legacy-unknown-status", "none"} {
		t.Run("未知永続値 "+unknown, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.StartNewTask(); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("task.status", unknown); err != nil {
				t.Fatal(err)
			}
			for _, surface := range taskStatusSurfaces {
				if got := surface.read(t, cfg, st); got != nil {
					t.Fatalf("%s = %#v want null", surface.name, got)
				}
			}
		})
	}

	// stats履歴archiveのstatus値も同じ受理集合を通る。明示指定taskのarchive値が
	// 未知のとき契約外stringが出ないことを--timelineで検証する。
	t.Run("未知archive値", func(t *testing.T) {
		cfg := newAppConfig(t)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		archivedID, err := state.NewUUID()
		if err != nil {
			t.Fatal(err)
		}
		archive := `{"version":3,"task_id":"` + archivedID + `","status":"legacy-unknown-status"}`
		if err := st.Write(filepath.Join("stats", archivedID+".json"), archive); err != nil {
			t.Fatal(err)
		}
		if err := st.Write(filepath.Join("events", archivedID+".jsonl"), ""); err != nil {
			t.Fatal(err)
		}

		var out bytes.Buffer
		if err := printTimeline(st, archivedID, &out); err != nil {
			t.Fatal(err)
		}
		assertNullJSONValue(t, "task_status", requireJSONKey(t, decodeSingleLineJSON(t, out.String()), "task_status"))
	})
}

// statsMapAggregateFieldsは--stats出力のmap集計field全件のJSON key。
var statsMapAggregateFields = []string{
	"model_calls_by_alias",
	"model_duration_ms_by_alias",
	"input_tokens_by_alias",
	"cache_creation_input_tokens_by_alias",
	"cache_read_input_tokens_by_alias",
	"total_prompt_tokens_by_alias",
	"output_tokens_by_alias",
	"top_level_turns_by_alias",
	"call_trees_by_resolved_model",
	"input_tokens_by_resolved_model",
	"cache_creation_input_tokens_by_resolved_model",
	"cache_read_input_tokens_by_resolved_model",
	"output_tokens_by_resolved_model",
	"rate_limits_by_alias",
	"provider_unavailable_by_alias",
	"risk_floor_by_category",
	"snapshot_mismatch_by_axis",
	"packet_reject_by_category",
	"probe_outcome",
	"parent_outcomes",
	"parent_fix_origins",
	"parent_outcomes_by_model",
	"parent_outcomes_by_risk",
}

// assertStatsMapFieldsAreObjectsは全map集計fieldがJSON nullではなくobjectであることを
// raw JSONで検査する。typed decodeのlen(nilMap)==0ではnullと{}を区別できない。
func assertStatsMapFieldsAreObjects(t *testing.T, decoded map[string]any) {
	t.Helper()
	for _, key := range statsMapAggregateFields {
		if _, ok := decoded[key].(map[string]any); !ok {
			t.Fatalf("stats出力の%qがJSON objectではありません: %#v", key, decoded[key])
		}
	}
}

// TestStatsRawJSONContractは--stats機械契約をraw JSONで固定する。task 0件でも全map
// 集計fieldが空objectで存在し、current_task.statusがtask不在時nullになる。
func TestStatsRawJSONContract(t *testing.T) {
	t.Run("task 0件", func(t *testing.T) {
		cfg := newAppConfig(t)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}

		decoded := statsRawJSON(t, st)
		assertStatsMapFieldsAreObjects(t, decoded)
		for _, key := range statsMapAggregateFields {
			if entries := decoded[key].(map[string]any); len(entries) != 0 {
				t.Fatalf("task 0件なのに%q = %#v", key, entries)
			}
		}
		currentTask, ok := decoded["current_task"].(map[string]any)
		if !ok {
			t.Fatalf("current_taskがJSON objectではありません: %#v", decoded["current_task"])
		}
		assertNullJSONValue(t, "current_task.id", requireJSONKey(t, currentTask, "id"))
		assertNullJSONValue(t, "current_task.status", requireJSONKey(t, currentTask, "status"))
		assertNoPresentationSentinel(t, currentTask, "status")
	})

	t.Run("集計あり", func(t *testing.T) {
		cfg := newAppConfig(t)
		st, err := state.NewStateStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.StartNewTask(); err != nil {
			t.Fatal(err)
		}
		st.RecordModelCall(state.WorkerRole, "opus")
		st.RecordRateLimit("opus")

		decoded := statsRawJSON(t, st)
		assertStatsMapFieldsAreObjects(t, decoded)
		modelCalls, ok := decoded["model_calls_by_alias"].(map[string]any)
		if !ok || modelCalls["opus"] != float64(1) {
			t.Fatalf("model_calls_by_alias = %#v", decoded["model_calls_by_alias"])
		}
		rateLimits, ok := decoded["rate_limits_by_alias"].(map[string]any)
		if !ok || rateLimits["opus"] != float64(1) {
			t.Fatalf("rate_limits_by_alias = %#v", decoded["rate_limits_by_alias"])
		}
		currentTask, ok := decoded["current_task"].(map[string]any)
		if !ok {
			t.Fatalf("current_taskがJSON objectではありません: %#v", decoded["current_task"])
		}
		assertEnumJSONValue(t, "current_task.status", requireJSONKey(t, currentTask, "status"),
			"active", "waiting-decision", "waiting-sol-review", "complete", "rate-limited", "provider-unavailable")
		assertNoPresentationSentinel(t, currentTask, "status")
	})
}
