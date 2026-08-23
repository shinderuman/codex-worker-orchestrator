package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// writeIngesterLinesはingesterへstream-json行を流し込む。test fixture構築の補助。
func writeIngesterLines(t *testing.T, ingester *streamEventIngester, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if _, err := ingester.Write([]byte(line + "\n")); err != nil {
			t.Fatal(err)
		}
	}
	ingester.flush()
}

func readLiveStatus(t *testing.T, st *state.StateStore, taskID string) state.TaskLiveStatus {
	t.Helper()
	status, err := st.ReadTaskLiveStatus(taskID)
	if err != nil {
		t.Fatalf("live status読み込み: %v", err)
	}
	return status
}

// TestStreamEventIngesterPublishesLiveToolDetailsはtool_useの実行中だけcommand・purpose・
// background待ち詳細がlive snapshotへ乗り、tool_resultで外れることを検証する。
func TestStreamEventIngesterPublishesLiveToolDetails(t *testing.T) {
	st := newTestStateStore(t)
	ingester := newFakeClockIngester(t, st, 3*time.Second)

	writeIngesterLines(t, ingester,
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_bash","name":"Bash","input":{"command":"sleep 295; grep -c GATE /tmp/instr4.log","description":"Check fourth run at gate section"}}]}}`,
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_wait","name":"TaskOutput","input":{"task_id":"bash_7"}}]}}`,
	)

	pending := readLiveStatus(t, st, "t")
	if len(pending.Tools) != 2 {
		t.Fatalf("pending tools = %#v", pending.Tools)
	}
	if pending.Tools[0].ToolID != "toolu_bash" || pending.Tools[0].Command != "sleep 295; grep -c GATE /tmp/instr4.log" || pending.Tools[0].Purpose != "Check fourth run at gate section" {
		t.Fatalf("Bash詳細 = %#v", pending.Tools[0])
	}
	if pending.Tools[1].ToolID != "toolu_wait" || pending.Tools[1].WaitTaskID != "bash_7" {
		t.Fatalf("background待ち詳細 = %#v", pending.Tools[1])
	}
	if pending.LastEventAt.IsZero() {
		t.Fatalf("last_event_atが未設定: %#v", pending)
	}

	writeIngesterLines(t, ingester,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_bash","content":"done"}]}}`,
	)
	afterResult := readLiveStatus(t, st, "t")
	if len(afterResult.Tools) != 1 || afterResult.Tools[0].ToolID != "toolu_wait" {
		t.Fatalf("tool_result後のpending = %#v", afterResult.Tools)
	}

	writeIngesterLines(t, ingester,
		`{"type":"result","subtype":"success","is_error":false}`,
	)
	cleared := readLiveStatus(t, st, "t")
	if len(cleared.Tools) != 0 {
		t.Fatalf("result event後もpendingが残っています: %#v", cleared.Tools)
	}
}

// TestStreamEventIngesterLiveProgressEventsAdvanceIdleBaseはevent logへ抑止される
// thinking_tokens進捗eventもlive snapshotのlast_event_atとmodel activity専用時刻の
// 両方へ反映されることを検証する。initは非model activityのため専用時刻の起点に
// ならない。
func TestStreamEventIngesterLiveProgressEventsAdvanceIdleBase(t *testing.T) {
	st := newTestStateStore(t)
	ingester := newFakeClockIngester(t, st, 2*time.Second)

	writeIngesterLines(t, ingester,
		`{"type":"system","subtype":"init","model":"glm-5.3"}`,
		`{"type":"system","subtype":"thinking_tokens"}`,
		`{"type":"system","subtype":"thinking_tokens"}`,
	)

	records := readTaskEventLines(t, st, "t")
	if len(records) != 1 {
		t.Fatalf("進捗eventがevent logへ記録されています: %d件", len(records))
	}
	status := readLiveStatus(t, st, "t")
	if !status.LastEventAt.Equal(records[0].Timestamp.Add(4 * time.Second)) {
		t.Fatalf("last_event_at = %v, want 3件目の観測時刻 %v", status.LastEventAt, records[0].Timestamp.Add(4*time.Second))
	}
	if !status.LastModelActivityAt.Equal(records[0].Timestamp.Add(4 * time.Second)) {
		t.Fatalf("last_model_activity_at = %v, want 最終thinking_tokens観測時刻 %v", status.LastModelActivityAt, records[0].Timestamp.Add(4*time.Second))
	}
}

// TestStreamEventIngesterToolProgressAdvancesOnlyGenericActivityはsystem/tool_progressが
// genericなlast_event_atだけを進め、model activity専用時刻を進めないことを検証する。
// 長時間tool実行中にMODEL_IDLEが誤ってリセットされない境界。
func TestStreamEventIngesterToolProgressAdvancesOnlyGenericActivity(t *testing.T) {
	st := newTestStateStore(t)
	ingester := newFakeClockIngester(t, st, 2*time.Second)

	writeIngesterLines(t, ingester,
		`{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"sleep 295"}}]}}`,
		`{"type":"system","subtype":"tool_progress"}`,
		`{"type":"system","subtype":"tool_progress"}`,
	)

	records := readTaskEventLines(t, st, "t")
	if len(records) != 3 {
		t.Fatalf("記録件数 = %d", len(records))
	}
	status := readLiveStatus(t, st, "t")
	if !status.LastEventAt.Equal(records[2].Timestamp) {
		t.Fatalf("last_event_at = %v, want tool_progress観測時刻 %v", status.LastEventAt, records[2].Timestamp)
	}
	if !status.LastModelActivityAt.Equal(records[0].Timestamp) {
		t.Fatalf("tool_progressがmodel activity時刻を進めました: %v, want assistant観測時刻 %v", status.LastModelActivityAt, records[0].Timestamp)
	}
}

// TestStreamEventIngesterModelActivityAcceptanceSetはstate.IsModelActivityEventの共有
// 契約どおりのeventだけがlive snapshotのmodel activity専用時刻を進めることを、
// 実stream-json行の摄取経路で検証する。
func TestStreamEventIngesterModelActivityAcceptanceSet(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		adv    bool
		logged bool
	}{
		{name: "assistant thinking", line: `{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"thinking","thinking":"plan"}]}}`, adv: true, logged: true},
		{name: "assistant text", line: `{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"text","text":"answer"}]}}`, adv: true, logged: true},
		{name: "assistant tool_use", line: `{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{}}]}}`, adv: true, logged: true},
		{name: "system thinking_tokens", line: `{"type":"system","subtype":"thinking_tokens"}`, adv: true, logged: false},
		{name: "system init", line: `{"type":"system","subtype":"init","model":"glm-5.3"}`, adv: false, logged: true},
		{name: "system tool_progress", line: `{"type":"system","subtype":"tool_progress"}`, adv: false, logged: true},
		{name: "system task_notification", line: `{"type":"system","subtype":"task_notification"}`, adv: false, logged: true},
		{name: "user tool_result", line: `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"done"}]}}`, adv: false, logged: true},
		{name: "result", line: lineOfKind(streamFixtureLines, "result"), adv: false, logged: true},
		{name: "assistant server_tool_use only", line: `{"type":"assistant","message":{"model":"glm-5.3","content":[{"type":"server_tool_use","name":"WebSearch"}]}}`, adv: false, logged: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := newTestStateStore(t)
			ingester := newFakeClockIngester(t, st, 2*time.Second)
			writeIngesterLines(t, ingester, c.line)

			records := []state.TaskEventRecord{}
			if _, err := os.Stat(st.TaskEventLogPath("t")); err == nil {
				records = readTaskEventLines(t, st, "t")
			}
			logged := len(records) == 1
			if logged != c.logged {
				t.Fatalf("event log記録 = %v, want %v", logged, c.logged)
			}
			status := readLiveStatus(t, st, "t")
			if c.adv {
				var want time.Time
				if c.logged {
					want = records[0].Timestamp
				} else {
					want = status.LastEventAt
				}
				if status.LastModelActivityAt.IsZero() || !status.LastModelActivityAt.Equal(want) {
					t.Fatalf("model activity時刻 = %v, want %v", status.LastModelActivityAt, want)
				}
				return
			}
			if !status.LastModelActivityAt.IsZero() {
				t.Fatalf("非model activityがmodel activity時刻を進めました: %v", status.LastModelActivityAt)
			}
			if status.LastEventAt.IsZero() {
				t.Fatalf("generic活動時刻が進んでいません: %#v", status)
			}
		})
	}
}

// TestStreamEventIngesterBoundsLiveDetailTextはlive snapshot本文が上限bytesでUTF-8境界に
// 合わせて切詰められることを検証する。watch表示でのtruncateとは別のfile size上限。
func TestStreamEventIngesterBoundsLiveDetailText(t *testing.T) {
	st := newTestStateStore(t)
	ingester := newFakeClockIngester(t, st, 3*time.Second)

	longCommand := strings.Repeat("あ", liveCommandMaxBytes) + "tail"
	longPurpose := strings.Repeat("p", livePurposeMaxBytes+10)
	line := `{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"` + longCommand + `","description":"` + longPurpose + `"}}]}}`
	writeIngesterLines(t, ingester, line)

	status := readLiveStatus(t, st, "t")
	if len(status.Tools) != 1 {
		t.Fatalf("tools = %#v", status.Tools)
	}
	command := status.Tools[0].Command
	if len(command) > liveCommandMaxBytes || strings.Contains(command, "tail") {
		t.Fatalf("command上限 = %d bytes: %q...", len(command), command[:min(30, len(command))])
	}
	if len(status.Tools[0].Purpose) > livePurposeMaxBytes {
		t.Fatalf("purpose上限 = %d bytes", len(status.Tools[0].Purpose))
	}
}

// TestStreamEventIngesterLiveWriteFailureKeepsEventLogはlive snapshotへ書けないときも
// event log追記・tool timing観測が継続することを検証する。
func TestStreamEventIngesterLiveWriteFailureKeepsEventLog(t *testing.T) {
	st := newTestStateStore(t)
	if err := os.MkdirAll(filepath.Join(st.Path("events"), "t.live.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	ingester := newFakeClockIngester(t, st, 3*time.Second)

	writeIngesterLines(t, ingester,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"echo x"}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"x"}]}}`,
	)

	records := readTaskEventLines(t, st, "t")
	if len(records) != 2 {
		t.Fatalf("event log記録 = %d件", len(records))
	}
	if records[1].Blocks[0].DurationMS != 3000 {
		t.Fatalf("live書込み失敗でtool timingが落ちました: %#v", records[1].Blocks[0])
	}
}

// TestClaudeRunnerLiveStatusSnapshotBoundaryはproduction Run経路で、live snapshotにtool
// 実行中のcommand詳細が置かれる期間があってもevent logへcommand本文・秘密情報が
// 保存されないことを検証する。
func TestClaudeRunnerLiveStatusSnapshotBoundary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}

	commandPath := writeStreamFixtureClaude(t, streamFixtureLines)
	r, st, taskID := newStreamFixtureRunner(t, commandPath)

	outputPath := filepath.Join(t.TempDir(), "out.log")
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", outputPath); err != nil {
		t.Fatal(err)
	}

	rawLog, err := os.ReadFile(st.TaskEventLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawLog), streamFixtureSecret) || strings.Contains(string(rawLog), `"command"`) {
		t.Fatalf("event logへcommand本文が新規保存されました: %s", rawLog)
	}
	status := readLiveStatus(t, st, taskID)
	if len(status.Tools) != 0 {
		t.Fatalf("call終端後のlive snapshotにtoolが残っています: %#v", status.Tools)
	}
}

// writeTimedStreamScriptはstream-json行を1行ずつ1秒間隔で出すfake claude scriptを書く。
// 実時間の観測時刻差を保証し、production Run経路のsnapshot時刻検証を決定的にする。
func writeTimedStreamScript(t *testing.T, lines ...string) string {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line, `'`) {
			t.Fatalf("単一quoteを含む行はscript化できません: %s", line)
		}
	}
	script := "#!/bin/sh\n"
	for index, line := range lines {
		if index > 0 {
			script += "sleep 1\n"
		}
		script += fmt.Sprintf("printf '%%s\\n' '%s'\n", line)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude-timed")
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return commandPath
}

// TestClaudeRunnerLiveModelActivityFromSuppressedThinkingTokensはproduction Run経路で、
// event logへ保存されないsystem/thinking_tokensがlive snapshotのmodel activity専用時刻を
// 進め、最後のresult eventは専用時刻を進めないことを検証する。行間1秒の実時間差で
// assistant観測 < thinking_tokens観測 < result観測の順序を固定する。
func TestClaudeRunnerLiveModelActivityFromSuppressedThinkingTokens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}

	commandPath := writeTimedStreamScript(t,
		lineOfKind(streamFixtureLines, "assistant"),
		`{"type":"system","subtype":"thinking_tokens"}`,
		lineOfKind(streamFixtureLines, "result"),
	)
	r, st, taskID := newStreamFixtureRunner(t, commandPath)

	outputPath := filepath.Join(t.TempDir(), "out.log")
	if _, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", outputPath); err != nil {
		t.Fatal(err)
	}

	records := readTaskEventLines(t, st, taskID)
	if len(records) != 2 || records[0].Kind != "assistant" || records[1].Kind != "result" {
		t.Fatalf("event log = %#v(assistantとresultだけが記録される)", records)
	}
	status := readLiveStatus(t, st, taskID)
	if status.LastModelActivityAt.Sub(records[0].Timestamp) < time.Second {
		t.Fatalf("thinking_tokensがmodel activity専用時刻を進めていません: %v(assistant = %v)", status.LastModelActivityAt, records[0].Timestamp)
	}
	if !status.LastEventAt.Equal(records[1].Timestamp) {
		t.Fatalf("last_event_at = %v, want result観測時刻 %v", status.LastEventAt, records[1].Timestamp)
	}
	if status.LastEventAt.Sub(status.LastModelActivityAt) < time.Second {
		t.Fatalf("resultがmodel activity専用時刻を進めています: %v(result = %v)", status.LastModelActivityAt, records[1].Timestamp)
	}
}
