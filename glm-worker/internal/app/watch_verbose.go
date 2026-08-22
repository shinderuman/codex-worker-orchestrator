package app

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// defaultWatchStatusIntervalはverbose LIVE表示の定期再表示間隔。follow tickより長く、
// 長時間toolのelapsed更新を人間が追える鮮度に保つ。
const defaultWatchStatusInterval = 5 * time.Second

// defaultWatchChangeIntervalは現在tool集合の変化を検出した際の再表示最小間隔。busyな
// stream中にLIVE blockがevent行へ割り込み続けないための下限。
const defaultWatchChangeInterval = time.Second

// watchVerboseLastToolMinDurationは「直前に完了した長時間tool」としてLAST表示する最小
// 所要時間。短いtoolの連続でLASTが意味を失わないための閾値。
const watchVerboseLastToolMinDuration = 10 * time.Second

// watchDetailMaxRunesはLIVE行へ出すcommand・purpose本文の表示上限。切詰めはこの表示時
// だけに行い、event log・live snapshot側の保存内容を変更しない。
const watchDetailMaxRunes = 200

// watchPendingToolはevent logのtool_use blockから組立中の実行中tool。
type watchPendingTool struct {
	toolID    string
	name      string
	callID    string
	startedAt time.Time
}

// watchCompletedToolは測定済み所要時間を持つ完了tool。LAST表示の対象。
type watchCompletedTool struct {
	name       string
	duration   time.Duration
	finishedAt time.Time
}

// watchToolErrorは直近に観測したtool error。
type watchToolError struct {
	name string
	at   time.Time
}

// watchToolTrackerは表示済みevent recordから--watch --verbose表示用のtool状態を組立る。
// tool_use/tool_resultをtool IDで対応付け、未対応のresult eventで同一callのpendingを
// 除く(呼出終端後も実行し続けているとは限らないため)。model activity時刻は
// assistant側のthinking/text/tool_use観測だけを基準に更新する。
type watchToolTracker struct {
	pending             map[string]watchPendingTool
	lastLongTool        *watchCompletedTool
	lastError           *watchToolError
	lastModelActivityAt time.Time
	firstEventAt        time.Time
}

func newWatchToolTracker() *watchToolTracker {
	return &watchToolTracker{pending: make(map[string]watchPendingTool)}
}

func (t *watchToolTracker) observe(record state.TaskEventRecord) {
	if t.firstEventAt.IsZero() {
		t.firstEventAt = record.Timestamp
	}
	if watchModelActivityRecord(record) && record.Timestamp.After(t.lastModelActivityAt) {
		t.lastModelActivityAt = record.Timestamp
	}
	for index := range record.Blocks {
		t.observeBlock(record, &record.Blocks[index])
	}
	if record.Kind == "result" {
		for id, tool := range t.pending {
			if tool.callID == record.CallID {
				delete(t.pending, id)
			}
		}
	}
}

// watchModelActivityRecordはrecordがmodel activityの観測かを判定する。MODEL_IDLEは
// 「最後のmodel activityからの経過時間」のため、assistant側のthinking・text・tool_use
// blockだけを基準にし、system tool_progress・task_notification・user tool_result・
// background task状態通知では増え続ける経過を止めない。
func watchModelActivityRecord(record state.TaskEventRecord) bool {
	if record.Kind != "assistant" {
		return false
	}
	for _, block := range record.Blocks {
		switch block.Type {
		case "thinking", "text", "tool_use":
			return true
		}
	}
	return false
}

func (t *watchToolTracker) observeBlock(record state.TaskEventRecord, block *state.TaskBlockSummary) {
	switch block.Type {
	case "tool_use":
		if block.ToolID == "" {
			return
		}
		t.pending[block.ToolID] = watchPendingTool{
			toolID:    block.ToolID,
			name:      block.Name,
			callID:    record.CallID,
			startedAt: record.Timestamp,
		}
	case "tool_result":
		if block.ToolID == "" {
			return
		}
		observed, ok := t.pending[block.ToolID]
		if !ok {
			t.rememberError(block.Name, record.Timestamp, block.IsError)
			return
		}
		duration := time.Duration(block.DurationMS) * time.Millisecond
		if duration == 0 {
			duration = record.Timestamp.Sub(observed.startedAt)
		}
		if duration >= watchVerboseLastToolMinDuration {
			t.lastLongTool = &watchCompletedTool{
				name:       toolDisplayName(block.Name, observed.name),
				duration:   duration,
				finishedAt: record.Timestamp,
			}
		}
		t.rememberError(toolDisplayName(block.Name, observed.name), record.Timestamp, block.IsError)
		delete(t.pending, block.ToolID)
	}
}

func (t *watchToolTracker) rememberError(name string, at time.Time, isError bool) {
	if !isError {
		return
	}
	t.lastError = &watchToolError{name: name, at: at}
}

func toolDisplayName(blockName string, observedName string) string {
	if blockName != "" {
		return blockName
	}
	return observedName
}

// pendingToolsは実行中toolを開始順で返す。
func (t *watchToolTracker) pendingTools() []watchPendingTool {
	tools := make([]watchPendingTool, 0, len(t.pending))
	for _, tool := range t.pending {
		tools = append(tools, tool)
	}
	sort.Slice(tools, func(i, j int) bool {
		if !tools[i].startedAt.Equal(tools[j].startedAt) {
			return tools[i].startedAt.Before(tools[j].startedAt)
		}
		return tools[i].toolID < tools[j].toolID
	})
	return tools
}

// signatureはLIVE再表示要否判定用の現在状態要約。表示対象の変化(pending集合・LAST・
// ERROR)だけを含め、経過時間の変化だけで再表示しない。
func (t *watchToolTracker) signature() string {
	tools := t.pendingTools()
	parts := make([]string, 0, len(tools)+2)
	for _, tool := range tools {
		parts = append(parts, tool.toolID)
	}
	if t.lastLongTool != nil {
		parts = append(parts, fmt.Sprintf("last:%s:%d", t.lastLongTool.name, t.lastLongTool.duration.Milliseconds()))
	}
	if t.lastError != nil {
		parts = append(parts, "error:"+t.lastError.name)
	}
	return strings.Join(parts, ",")
}

// watchLiveStatusはverbose LIVE blockの出力間隔を制御する。state・live snapshotは毎回
// 読み取り専用で参照し、watch側から何も書き込まない。
type watchLiveStatus struct {
	st            *state.StateStore
	taskID        string
	stdout        io.Writer
	tracker       *watchToolTracker
	opts          watchOptions
	printed       bool
	lastPrint     time.Time
	lastSignature string
}

// refreshは表示条件を満たすときLIVE blockを出す。statusInterval経過、または表示対象の
// 変化をchangeInterval経過後に検出したときに出す。forceは初回表示の呼出。
func (w *watchLiveStatus) refresh(force bool) {
	if !w.opts.verbose {
		return
	}
	now := w.opts.now()
	signature := w.tracker.signature()
	if !force && w.printed {
		elapsed := now.Sub(w.lastPrint)
		due := elapsed >= w.opts.statusInterval
		if !due && signature != w.lastSignature && elapsed >= w.opts.changeInterval {
			due = true
		}
		if !due {
			return
		}
	}
	renderWatchLiveStatus(w.st, w.taskID, w.stdout, w.tracker, now)
	w.printed = true
	w.lastPrint = now
	w.lastSignature = signature
}

// renderWatchLiveStatusは現時点のlive tool状態をLIVE行へ出す。tool種別・経過・LASTは
// event logのrecordから、command・purpose・background待ちはrunnerが書いたlive snapshot
// からtool ID対応で取り、snapshot欠損時はそれらの行を省く(表示全体は失敗させない)。
func renderWatchLiveStatus(st *state.StateStore, taskID string, stdout io.Writer, tracker *watchToolTracker, now time.Time) {
	if startedAt, ok := watchTaskStartedAt(st, tracker); ok {
		fmt.Fprintf(stdout, "LIVE TASK_AGE %s\n", formatWatchAge(now.Sub(startedAt)))
	}
	details := liveToolDetails(st, taskID, tracker)
	if !details.modelIdleAt.IsZero() {
		fmt.Fprintf(stdout, "LIVE MODEL_IDLE %s\n", formatWatchElapsed(now.Sub(details.modelIdleAt)))
	}
	pending := tracker.pendingTools()
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "LIVE CURRENT none")
	}
	for _, tool := range pending {
		fmt.Fprintf(stdout, "LIVE CURRENT %s %s elapsed\n", tool.name, formatWatchElapsed(now.Sub(tool.startedAt)))
		detail, ok := details.tools[tool.toolID]
		if !ok {
			continue
		}
		if detail.Command != "" {
			fmt.Fprintf(stdout, "LIVE COMMAND %s\n", truncateWatchDetail(detail.Command))
		}
		if detail.Purpose != "" {
			fmt.Fprintf(stdout, "LIVE PURPOSE %s\n", truncateWatchDetail(detail.Purpose))
		}
		if detail.WaitTaskID != "" {
			fmt.Fprintf(stdout, "LIVE BACKGROUND_WAIT task=%s\n", detail.WaitTaskID)
		}
		if detail.Background {
			fmt.Fprintln(stdout, "LIVE BACKGROUND starting")
		}
	}
	if tracker.lastLongTool != nil {
		fmt.Fprintf(stdout, "LIVE LAST %s completed %s\n", tracker.lastLongTool.name, formatWatchLastDuration(tracker.lastLongTool.duration))
	}
	if tracker.lastError != nil {
		fmt.Fprintf(stdout, "LIVE TOOL_ERROR %s %s ago\n", tracker.lastError.name, formatWatchElapsed(now.Sub(tracker.lastError.at)))
	}
}

// liveToolDetailsはlive snapshotを読み、MODEL_IDLE基準のmodel activity時刻とtool ID対応の
// 詳細を返す。snapshotのlast_event_atはtool_progress等の非model event観測でも進むため
// MODEL_IDLEの基準には使わない。model activity eventはevent logへ必ず記録されるため、
// 基準時刻はlog側のtrackerだけで組立てる。読めないときは詳細無しで復帰する。
type watchLiveDetails struct {
	tools       map[string]state.TaskLiveTool
	modelIdleAt time.Time
}

func liveToolDetails(st *state.StateStore, taskID string, tracker *watchToolTracker) watchLiveDetails {
	details := watchLiveDetails{tools: map[string]state.TaskLiveTool{}, modelIdleAt: tracker.lastModelActivityAt}
	status, err := st.ReadTaskLiveStatus(taskID)
	if err != nil {
		return details
	}
	for _, tool := range status.Tools {
		details.tools[tool.ToolID] = tool
	}
	return details
}

// watchTaskStartedAtはtask年齢の基準時刻を観測用stats mirrorから、無ければ先頭eventから
// 取る。どちらも無いときはTASK_AGE行を出さない。
func watchTaskStartedAt(st *state.StateStore, tracker *watchToolTracker) (time.Time, bool) {
	if stats, err := st.CurrentTaskStats(); err == nil && !stats.StartedAt.IsZero() {
		return stats.StartedAt, true
	}
	if !tracker.firstEventAt.IsZero() {
		return tracker.firstEventAt, true
	}
	return time.Time{}, false
}

// truncateWatchDetailはlive詳細本文を表示上限runesへ切詰める。改行は1行表示へ収まる
// ようescapeする。保存側ではなく表示時だけのtruncateである。
func truncateWatchDetail(text string) string {
	single := strings.ReplaceAll(text, "\r", "")
	single = strings.ReplaceAll(single, "\n", "\\n")
	runes := []rune(single)
	if len(runes) <= watchDetailMaxRunes {
		return single
	}
	return string(runes[:watchDetailMaxRunes]) + "..."
}

// formatWatchAgeはtask年齢のH:MM:SS形式。
func formatWatchAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d.Seconds())
	return fmt.Sprintf("%d:%02d:%02d", total/3600, (total%3600)/60, total%60)
}

// formatWatchElapsedは経過時間のM:SS形式。1時間以上はH:MM:SSへ切り替える。
func formatWatchElapsed(d time.Duration) string {
	if d >= time.Hour {
		return formatWatchAge(d)
	}
	if d < 0 {
		d = 0
	}
	total := int64(d.Seconds())
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// formatWatchLastDurationは完了toolの所要時間。1時間未満は秒表示、以上はH:MM:SS。
func formatWatchLastDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return formatWatchAge(d)
}
