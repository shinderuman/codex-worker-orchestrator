package state

import (
	"sort"
	"time"
)

type CallTimelineTool struct {
	Name          string
	Uses          int
	Results       int
	Measured      int
	MeasuredSumMS int64
	MeasuredMaxMS int64
	Unmeasured    int
	Errors        int
}

type CallTimelineEntry struct {
	CallID           string
	Role             string
	Phase            string
	SessionID        string
	Resumed          bool
	ModelAlias       string
	MessageModel     string
	SessionCallIndex int
	SessionCalls     int
	FirstAt          time.Time
	LastAt           time.Time
	Events           int
	ResultObserved   bool
	ResultSubtype    string
	IsError          bool
	DurationMS       int64
	DurationAPIMS    int64
	NumTurns         int
	TotalCostUSD     float64
	Usage            *TaskEventUsage
	Tools            []CallTimelineTool
}

func CallsFromTaskEvents(records []TaskEventRecord) []CallTimelineEntry {
	order := make([]string, 0)
	byCall := make(map[string]*CallTimelineEntry)
	toolsByCall := make(map[string]map[string]*CallTimelineTool)
	for _, record := range records {
		entry, ok := byCall[record.CallID]
		if !ok {
			entry = &CallTimelineEntry{CallID: record.CallID}
			byCall[record.CallID] = entry
			toolsByCall[record.CallID] = make(map[string]*CallTimelineTool)
			order = append(order, record.CallID)
		}
		absorbTaskEvent(entry, toolsByCall[record.CallID], record)
	}
	numberSessionCalls(byCall, order)
	result := make([]CallTimelineEntry, 0, len(order))
	for _, callID := range order {
		entry := byCall[callID]
		entry.Tools = sortedTimelineTools(toolsByCall[callID])
		result = append(result, *entry)
	}
	return result
}

func absorbTaskEvent(entry *CallTimelineEntry, tools map[string]*CallTimelineTool, record TaskEventRecord) {
	entry.Events++
	absorbTaskEventIdentity(entry, record)
	absorbTaskEventTimestamp(entry, record.Timestamp)
	absorbTaskEventResult(entry, record)
	for _, block := range record.Blocks {
		absorbTaskBlock(tools, block)
	}
}

func absorbTaskEventIdentity(entry *CallTimelineEntry, record TaskEventRecord) {
	if entry.Role == "" {
		entry.Role = record.Role
	}
	if entry.Phase == "" {
		entry.Phase = record.Phase
	}
	if entry.SessionID == "" {
		entry.SessionID = record.SessionID
	}
	if entry.ModelAlias == "" {
		entry.ModelAlias = record.ModelAlias
	}
	entry.Resumed = entry.Resumed || record.Resumed
	if record.MessageModel != "" {
		entry.MessageModel = record.MessageModel
	}
}

func absorbTaskEventTimestamp(entry *CallTimelineEntry, timestamp time.Time) {
	if timestamp.IsZero() {
		return
	}
	if entry.FirstAt.IsZero() || timestamp.Before(entry.FirstAt) {
		entry.FirstAt = timestamp
	}
	if timestamp.After(entry.LastAt) {
		entry.LastAt = timestamp
	}
}

func absorbTaskEventResult(entry *CallTimelineEntry, record TaskEventRecord) {
	if record.Kind != "result" {
		return
	}
	entry.ResultObserved = true
	entry.ResultSubtype = record.Subtype
	entry.IsError = record.IsError
	entry.DurationMS = record.DurationMS
	entry.DurationAPIMS = record.DurationAPIMS
	entry.NumTurns = record.NumTurns
	entry.TotalCostUSD = record.TotalCostUSD
	entry.Usage = record.Usage
}

func absorbTaskBlock(tools map[string]*CallTimelineTool, block TaskBlockSummary) {
	switch block.Type {
	case "tool_use":
		timelineTool(tools, block.Name).Uses++
	case "tool_result":
		tool := timelineTool(tools, block.Name)
		tool.Results++
		if block.IsError {
			tool.Errors++
		}
		if block.DurationMS > 0 {
			tool.Measured++
			tool.MeasuredSumMS += block.DurationMS
			if block.DurationMS > tool.MeasuredMaxMS {
				tool.MeasuredMaxMS = block.DurationMS
			}
			return
		}
		tool.Unmeasured++
	}
}

func timelineTool(tools map[string]*CallTimelineTool, name string) *CallTimelineTool {
	if name == "" {
		name = "unknown"
	}
	tool, ok := tools[name]
	if !ok {
		tool = &CallTimelineTool{Name: name}
		tools[name] = tool
	}
	return tool
}

func numberSessionCalls(byCall map[string]*CallTimelineEntry, order []string) {
	counts := make(map[string]int)
	for _, callID := range order {
		entry := byCall[callID]
		counts[entry.SessionID]++
		entry.SessionCallIndex = counts[entry.SessionID]
	}
	for _, entry := range byCall {
		entry.SessionCalls = counts[entry.SessionID]
	}
}

func sortedTimelineTools(tools map[string]*CallTimelineTool) []CallTimelineTool {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]CallTimelineTool, 0, len(names))
	for _, name := range names {
		result = append(result, *tools[name])
	}
	return result
}

func SumCallTimelineTools(entries []CallTimelineEntry) []CallTimelineTool {
	tools := make(map[string]*CallTimelineTool)
	for _, entry := range entries {
		for _, tool := range entry.Tools {
			total, ok := tools[tool.Name]
			if !ok {
				total = &CallTimelineTool{Name: tool.Name}
				tools[tool.Name] = total
			}
			total.Uses += tool.Uses
			total.Results += tool.Results
			total.Measured += tool.Measured
			total.MeasuredSumMS += tool.MeasuredSumMS
			if tool.MeasuredMaxMS > total.MeasuredMaxMS {
				total.MeasuredMaxMS = tool.MeasuredMaxMS
			}
			total.Unmeasured += tool.Unmeasured
			total.Errors += tool.Errors
		}
	}
	return sortedTimelineTools(tools)
}
