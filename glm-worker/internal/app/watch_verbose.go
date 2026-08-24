package app

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// defaultWatchStatusIntervalはverbose live eventの定期再出力間隔。follow tickより長く、
// 長時間toolのelapsed更新を追える鮮度に保つ。
const defaultWatchStatusInterval = 5 * time.Second

// defaultWatchChangeIntervalは現在tool集合の変化を検出した際の再出力最小間隔。busyな
// stream中にlive eventが割り込み続けないための下限。
const defaultWatchChangeInterval = time.Second

// watchVerboseLastToolMinDurationは「直前に完了した長時間tool」としてlastに載せる最小
// 所要時間。短いtoolの連続でlastが意味を失わないための閾値。
const watchVerboseLastToolMinDuration = 10 * time.Second

// watchDetailMaxRunesはlive eventへ出すcommand・purpose本文の出力上限。切詰めはこの
// 出力時だけに行い、event log・live snapshot側の保存内容を変更しない。
const watchDetailMaxRunes = 200

// watchPendingToolはevent logのtool_use blockから組立中の実行中tool。
type watchPendingTool struct {
	toolID    string
	name      string
	callID    string
	startedAt time.Time
}

// watchCompletedToolは測定済み所要時間を持つ完了tool。last表示の対象。
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

// watchToolTrackerは流したevent recordから--watch --verbose用のtool状態を組立る。
// tool_use/tool_resultをtool IDで対応付け、未対応のresult eventで同一callのpendingを
// 除く(呼出終端後も実行し続けているとは限らないため)。model activity時刻は
// state.IsModelActivityEventの共有契約に従い、event logへ現れるassistant側の
// thinking/text/tool_use観測を基準に更新する。
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
	if state.IsModelActivityEvent(record) && record.Timestamp.After(t.lastModelActivityAt) {
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

// signatureはlive event再出力要否判定用の現在状態要約。表示対象の変化(pending集合・last・
// error)だけを含め、経過時間の変化だけで再出力しない。
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

// watchLiveStatusはverbose live eventの出力間隔を制御する。state・live snapshotは毎回
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

// refreshは出力条件を満たすときlive eventを出す。statusInterval経過、または出力対象の
// 変化をchangeInterval経過後に検出したときに出す。forceは初回出力の呼出。
func (w *watchLiveStatus) refresh(force bool) error {
	if !w.opts.verbose {
		return nil
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
			return nil
		}
	}
	if err := writeWatchLiveStatus(w.st, w.taskID, w.stdout, w.tracker, now); err != nil {
		return err
	}
	w.printed = true
	w.lastPrint = now
	w.lastSignature = signature
	return nil
}

// watchLiveEventはverbose時のlive tool観測1件。実行中tool・直前の長時間tool・直近の
// tool errorを1つの型付きeventへ載せる。
type watchLiveEvent struct {
	Type        string             `json:"type"`
	TaskAgeMS   *int64             `json:"task_age_ms,omitempty"`
	ModelIdleMS *int64             `json:"model_idle_ms,omitempty"`
	Current     []watchLiveTool    `json:"current"`
	Last        *watchLiveLastTool `json:"last,omitempty"`
	ToolError   *watchLiveToolErr  `json:"tool_error,omitempty"`
}

type watchLiveTool struct {
	Name       string `json:"name"`
	ElapsedMS  int64  `json:"elapsed_ms"`
	Command    string `json:"command,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	Background bool   `json:"background,omitempty"`
	WaitTaskID string `json:"wait_task_id,omitempty"`
}

type watchLiveLastTool struct {
	Name       string `json:"name"`
	DurationMS int64  `json:"duration_ms"`
}

type watchLiveToolErr struct {
	Name  string `json:"name"`
	AgeMS int64  `json:"age_ms"`
}

// writeWatchLiveStatusは現時点のlive tool状態をlive eventへ出す。tool種別・経過・lastは
// event logのrecordから、command・purpose・background待ちはrunnerが書いたlive snapshot
// からtool ID対応で取り、snapshot欠損時はそれらのfieldを省く(出力全体は失敗させない)。
func writeWatchLiveStatus(st *state.StateStore, taskID string, stdout io.Writer, tracker *watchToolTracker, now time.Time) error {
	event := watchLiveEvent{Type: "live", Current: []watchLiveTool{}}
	if startedAt, ok := watchTaskStartedAt(st, tracker); ok {
		event.TaskAgeMS = msPtr(now.Sub(startedAt))
	}
	details := liveToolDetails(st, taskID, tracker)
	if !details.modelIdleAt.IsZero() {
		event.ModelIdleMS = msPtr(now.Sub(details.modelIdleAt))
	}
	for _, tool := range tracker.pendingTools() {
		liveTool := watchLiveTool{
			Name:      tool.name,
			ElapsedMS: elapsedMS(now.Sub(tool.startedAt)),
		}
		if detail, ok := details.tools[tool.toolID]; ok {
			liveTool.Command = truncateWatchDetail(detail.Command)
			liveTool.Purpose = truncateWatchDetail(detail.Purpose)
			liveTool.WaitTaskID = detail.WaitTaskID
			liveTool.Background = detail.Background
		}
		event.Current = append(event.Current, liveTool)
	}
	if tracker.lastLongTool != nil {
		event.Last = &watchLiveLastTool{
			Name:       tracker.lastLongTool.name,
			DurationMS: tracker.lastLongTool.duration.Milliseconds(),
		}
	}
	if tracker.lastError != nil {
		event.ToolError = &watchLiveToolErr{
			Name:  tracker.lastError.name,
			AgeMS: elapsedMS(now.Sub(tracker.lastError.at)),
		}
	}
	return writeWatchEvent(stdout, event)
}

// elapsedMSは経過durationをmillisecond整数へ寄せる。負値は観測境界の揺れとして0にする。
func elapsedMS(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return d.Milliseconds()
}

// liveToolDetailsはlive snapshotを読み、MODEL_IDLE基準のmodel activity時刻とtool ID対応の
// 詳細を返す。MODEL_IDLE基準はevent log側trackerのmodel activity時刻とsnapshotの
// last_model_activity_atの新しい方で、event logへ保存されないsystem/thinking_tokensの
// 観測はsnapshot側だけが持つ。snapshotのlast_event_atはtool_progress等の非model event
// 観測でも進むため基準には使わない。新field導入前の旧snapshotではlast_model_activity_atが
// zeroとして読め、tracker側の時刻へ落ちる(未知はassistant観測基準の安全側扱い)。読めない
// ときは詳細無しで復帰する。
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
	if status.LastModelActivityAt.After(details.modelIdleAt) {
		details.modelIdleAt = status.LastModelActivityAt
	}
	for _, tool := range status.Tools {
		details.tools[tool.ToolID] = tool
	}
	return details
}

// watchTaskStartedAtはtask年齢の基準時刻を観測用stats mirrorから、無ければ先頭eventから
// 取る。どちらも無いときはtask_age_msを出さない。
func watchTaskStartedAt(st *state.StateStore, tracker *watchToolTracker) (time.Time, bool) {
	if stats, err := st.CurrentTaskStats(); err == nil && !stats.StartedAt.IsZero() {
		return stats.StartedAt, true
	}
	if !tracker.firstEventAt.IsZero() {
		return tracker.firstEventAt, true
	}
	return time.Time{}, false
}

// truncateWatchDetailはlive詳細本文を出力上限runesへ切詰める。改行は1 event本文へ
// 収まるようescapeする。保存側ではなく出力時だけのtruncateである。
func truncateWatchDetail(text string) string {
	single := strings.ReplaceAll(text, "\r", "")
	single = strings.ReplaceAll(single, "\n", "\\n")
	runes := []rune(single)
	if len(runes) <= watchDetailMaxRunes {
		return single
	}
	return string(runes[:watchDetailMaxRunes]) + "..."
}
