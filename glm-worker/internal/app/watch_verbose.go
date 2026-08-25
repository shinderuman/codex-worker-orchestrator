package app

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const defaultWatchStatusInterval = 5 * time.Second

const defaultWatchChangeInterval = time.Second

const watchVerboseLastToolMinDuration = 10 * time.Second

const watchDetailMaxRunes = 200

type watchPendingTool struct {
	toolID    string
	name      string
	callID    string
	startedAt time.Time
}

type watchCompletedTool struct {
	name       string
	duration   time.Duration
	finishedAt time.Time
}

type watchToolError struct {
	name string
	at   time.Time
}

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

func elapsedMS(d time.Duration) int64 {
	if d < 0 {
		return 0
	}
	return d.Milliseconds()
}

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

func watchTaskStartedAt(st *state.StateStore, tracker *watchToolTracker) (time.Time, bool) {
	if stats, err := st.CurrentTaskStats(); err == nil && !stats.StartedAt.IsZero() {
		return stats.StartedAt, true
	}
	if !tracker.firstEventAt.IsZero() {
		return tracker.firstEventAt, true
	}
	return time.Time{}, false
}

func truncateWatchDetail(text string) string {
	single := strings.ReplaceAll(text, "\r", "")
	single = strings.ReplaceAll(single, "\n", "\\n")
	runes := []rune(single)
	if len(runes) <= watchDetailMaxRunes {
		return single
	}
	return string(runes[:watchDetailMaxRunes]) + "..."
}
