package state

import (
	"sort"
	"time"
)

type timelineMeasure struct {
	Uses          int
	Results       int
	Measured      int
	MeasuredSumMS int64
	MeasuredMaxMS int64
	Unmeasured    int
	Errors        int
}

type CallTimelineTool struct {
	Name string
	timelineMeasure
}

type CallTimelineOperation struct {
	Category string
	timelineMeasure
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
	Operations       []CallTimelineOperation
}

func (m *timelineMeasure) absorb(other timelineMeasure) {
	m.Uses += other.Uses
	m.Results += other.Results
	m.Measured += other.Measured
	m.MeasuredSumMS += other.MeasuredSumMS
	if other.MeasuredMaxMS > m.MeasuredMaxMS {
		m.MeasuredMaxMS = other.MeasuredMaxMS
	}
	m.Unmeasured += other.Unmeasured
	m.Errors += other.Errors
}

func CallsFromTaskEvents(records []TaskEventRecord) []CallTimelineEntry {
	order := make([]string, 0)
	byCall := make(map[string]*CallTimelineEntry)
	toolsByCall := make(map[string]map[string]*CallTimelineTool)
	operationsByCall := make(map[string]map[string]*CallTimelineOperation)
	for _, record := range records {
		entry, ok := byCall[record.CallID]
		if !ok {
			entry = &CallTimelineEntry{CallID: record.CallID}
			byCall[record.CallID] = entry
			toolsByCall[record.CallID] = make(map[string]*CallTimelineTool)
			operationsByCall[record.CallID] = make(map[string]*CallTimelineOperation)
			order = append(order, record.CallID)
		}
		absorbTaskEvent(entry, toolsByCall[record.CallID], operationsByCall[record.CallID], record)
	}
	numberSessionCalls(byCall, order)
	result := make([]CallTimelineEntry, 0, len(order))
	for _, callID := range order {
		entry := byCall[callID]
		entry.Tools = sortedTimelineTools(toolsByCall[callID])
		entry.Operations = sortedTimelineOperations(operationsByCall[callID])
		result = append(result, *entry)
	}
	return result
}

func absorbTaskEvent(entry *CallTimelineEntry, tools map[string]*CallTimelineTool, operations map[string]*CallTimelineOperation, record TaskEventRecord) {
	entry.Events++
	absorbTaskEventIdentity(entry, record)
	absorbTaskEventTimestamp(entry, record.Timestamp)
	absorbTaskEventResult(entry, record)
	for _, block := range record.Blocks {
		absorbTaskBlock(tools, operations, block)
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

func absorbTaskBlock(tools map[string]*CallTimelineTool, operations map[string]*CallTimelineOperation, block TaskBlockSummary) {
	switch block.Type {
	case "tool_use":
		timelineTool(tools, block.Name).Uses++
		timelineOperation(operations, block.OperationCategory).Uses++
	case "tool_result":
		delta := blockResultMeasure(block)
		timelineTool(tools, block.Name).absorb(delta)
		timelineOperation(operations, block.OperationCategory).absorb(delta)
	}
}

func blockResultMeasure(block TaskBlockSummary) timelineMeasure {
	delta := timelineMeasure{Results: 1}
	if block.IsError {
		delta.Errors = 1
	}
	if block.DurationMS > 0 {
		delta.Measured = 1
		delta.MeasuredSumMS = block.DurationMS
		delta.MeasuredMaxMS = block.DurationMS
		return delta
	}
	delta.Unmeasured = 1
	return delta
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

func timelineOperation(operations map[string]*CallTimelineOperation, category string) *CallTimelineOperation {
	if category == "" {
		category = OperationCategoryOther
	}
	operation, ok := operations[category]
	if !ok {
		operation = &CallTimelineOperation{Category: category}
		operations[category] = operation
	}
	return operation
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

func sortedTimelineOperations(operations map[string]*CallTimelineOperation) []CallTimelineOperation {
	categories := make([]string, 0, len(operations))
	for category := range operations {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	result := make([]CallTimelineOperation, 0, len(categories))
	for _, category := range categories {
		result = append(result, *operations[category])
	}
	return result
}

func SumCallTimelineTools(entries []CallTimelineEntry) []CallTimelineTool {
	tools := make(map[string]*CallTimelineTool)
	for _, entry := range entries {
		for _, tool := range entry.Tools {
			timelineTool(tools, tool.Name).absorb(tool.timelineMeasure)
		}
	}
	return sortedTimelineTools(tools)
}

func SumCallTimelineOperations(entries []CallTimelineEntry) []CallTimelineOperation {
	operations := make(map[string]*CallTimelineOperation)
	for _, entry := range entries {
		for _, operation := range entry.Operations {
			timelineOperation(operations, operation.Category).absorb(operation.timelineMeasure)
		}
	}
	return sortedTimelineOperations(operations)
}
