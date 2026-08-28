package runner

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type streamEventIngester struct {
	state                *state.StateStore
	base                 state.TaskEventRecord
	seq                  int
	pending              []byte
	closed               bool
	resultLine           []byte
	plain                []byte
	tools                map[string]toolUseObservation
	instructionReads     map[string]struct{}
	workerInstructionDir string
	now                  func() time.Time

	liveLastEventAt         time.Time
	liveLastModelActivityAt time.Time
	liveLastWrite           time.Time
	liveBroken              bool
}

type toolUseObservation struct {
	toolID          string
	timestamp       time.Time
	name            string
	command         string
	category        string
	purpose         string
	background      bool
	waitTaskID      string
	instructionRead string
}

type liveToolDetail struct {
	command    string
	purpose    string
	background bool
	waitTaskID string
}

type streamEvent struct {
	Type            string                 `json:"type"`
	Subtype         string                 `json:"subtype"`
	Model           string                 `json:"model"`
	Message         json.RawMessage        `json:"message"`
	IsError         bool                   `json:"is_error"`
	DurationMS      int64                  `json:"duration_ms"`
	DurationAPIMS   int64                  `json:"duration_api_ms"`
	NumTurns        int                    `json:"num_turns"`
	TotalCostUSD    float64                `json:"total_cost_usd"`
	Usage           *state.TaskEventUsage  `json:"usage"`
	CompactMetadata *streamCompactMetadata `json:"compactMetadata"`
}

type streamMessage struct {
	Model   string                `json:"model"`
	Usage   *state.TaskEventUsage `json:"usage"`
	Content []json.RawMessage     `json:"content"`
}

type streamBlock struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	ID        string `json:"id"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

const streamResultType = "result"

const plainSignalMaxBytes = 64 * 1024

const maxStreamEventRecordsPerCall = 50000

const liveStatusWriteInterval = time.Second

const (
	liveCommandMaxBytes = 2048
	livePurposeMaxBytes = 512
)

func newStreamEventIngester(
	st *state.StateStore,
	taskID string,
	callID string,
	role state.SessionRole,
	phase string,
	model string,
	sessionID string,
	resumed bool,
) *streamEventIngester {
	return &streamEventIngester{
		state: st,
		base: state.TaskEventRecord{
			TaskID:     taskID,
			CallID:     callID,
			SessionID:  sessionID,
			Role:       string(role),
			Phase:      phase,
			ModelAlias: model,
			Resumed:    resumed,
		},
		tools:            make(map[string]toolUseObservation),
		instructionReads: make(map[string]struct{}),
		now:              time.Now,
	}
}

func (g *streamEventIngester) result() ([]byte, bool) {
	if len(g.resultLine) == 0 {
		return nil, false
	}
	return g.resultLine, true
}

func (g *streamEventIngester) Write(p []byte) (int, error) {
	g.pending = append(g.pending, p...)
	for {
		index := bytes.IndexByte(g.pending, '\n')
		if index < 0 {
			break
		}
		line := g.pending[:index]
		g.pending = g.pending[index+1:]
		g.ingestLine(line)
	}
	return len(p), nil
}

func (g *streamEventIngester) flush() {
	if len(bytes.TrimSpace(g.pending)) > 0 {
		g.ingestLine(g.pending)
	}
	g.pending = nil
}

func (g *streamEventIngester) ingestLine(line []byte) {
	if len(bytes.TrimSpace(line)) == 0 {
		return
	}
	if streamResultEvent(line) {
		g.resultLine = append(g.resultLine[:0], line...)
	}
	g.capturePlainSignal(line)
	if g.closed {
		return
	}
	if g.seq >= maxStreamEventRecordsPerCall {
		state.WarnTaskEventCap(maxStreamEventRecordsPerCall)
		g.closed = true
		return
	}
	record := reduceStreamEvent(line, g.base, g.seq+1, g.now().UTC())
	modelActivity := state.IsModelActivityEvent(record)
	if progressOnlyStreamEvent(record) {

		g.noteLiveActivity(record.Timestamp, modelActivity, false)
		return
	}
	toolsChanged := g.observeToolBlocks(&record, toolUseInputs(line))
	if err := g.state.AppendTaskEvent(record); err != nil {
		state.WarnTaskEventSkip("追記", err)
		g.closed = true
		return
	}
	g.seq++
	g.noteLiveActivity(record.Timestamp, modelActivity, toolsChanged)
}

func progressOnlyStreamEvent(record state.TaskEventRecord) bool {
	return record.Kind == "system" && record.Subtype == "thinking_tokens"
}

func (g *streamEventIngester) observeToolBlocks(record *state.TaskEventRecord, inputs map[string]json.RawMessage) bool {
	changed := false
	for i := range record.Blocks {
		if g.observeToolBlock(&record.Blocks[i], record.Timestamp, inputs) {
			changed = true
		}
	}
	if record.Kind == streamResultType && len(g.tools) > 0 {
		g.tools = make(map[string]toolUseObservation)
		changed = true
	}
	return changed
}

func (g *streamEventIngester) observeToolBlock(block *state.TaskBlockSummary, at time.Time, inputs map[string]json.RawMessage) bool {
	switch block.Type {
	case "tool_use":
		return g.observeToolUse(block, at, inputs)
	case "tool_result":
		return g.observeToolResult(block, at)
	default:
		return false
	}
}

func (g *streamEventIngester) observeToolUse(block *state.TaskBlockSummary, at time.Time, inputs map[string]json.RawMessage) bool {
	if block.ToolID == "" {
		return false
	}
	observation := toolUseObservation{toolID: block.ToolID, timestamp: at, name: block.Name}
	if input, ok := inputs[block.ToolID]; ok {
		detail := extractLiveToolDetail(input)
		observation.command = detail.command
		observation.purpose = detail.purpose
		observation.background = detail.background
		observation.waitTaskID = detail.waitTaskID
		if name, matched := workerInstructionReadName(observation.name, input, g.workerInstructionDir); matched {
			observation.instructionRead = name
		}
	}
	observation.category = operationCategoryForTool(block.Name, observation.command)
	block.OperationCategory = observation.category
	g.tools[block.ToolID] = observation
	return true
}

func workerInstructionReadName(toolName string, input json.RawMessage, instructionDir string) (string, bool) {
	if toolName != "Read" || len(input) == 0 || instructionDir == "" {
		return "", false
	}
	var parsed struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return "", false
	}
	name, ok := workerInstructionReadPathName(parsed.FilePath, instructionDir)
	if !ok {
		return "", false
	}
	return knownWorkerInstructionFile(name)
}

func workerInstructionReadPathName(filePath, instructionDir string) (string, bool) {
	readPath, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		return "", false
	}
	root, err := filepath.Abs(filepath.Clean(instructionDir))
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, readPath)
	if err != nil || relative == "." || filepath.IsAbs(relative) || relative == ".." {
		return "", false
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) || strings.ContainsRune(relative, filepath.Separator) {
		return "", false
	}
	return filepath.Base(readPath), true
}

func knownWorkerInstructionFile(name string) (string, bool) {
	switch name {
	case "common-code.md", "testing.md", "state-transitions.md", "cli.md",
		"go.md", "javascript.md", "php.md", "eslint.md":
		return name, true
	default:
		return "", false
	}
}

func (g *streamEventIngester) instructionReadNames() []string {
	if len(g.instructionReads) == 0 {
		return nil
	}
	result := make([]string, 0, len(g.instructionReads))
	for name := range g.instructionReads {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func (g *streamEventIngester) observeToolResult(block *state.TaskBlockSummary, at time.Time) bool {
	if block.ToolID == "" {
		return false
	}
	observed, ok := g.tools[block.ToolID]
	if !ok {
		return false
	}
	block.DurationMS = at.Sub(observed.timestamp).Milliseconds()
	if block.Name == "" {
		block.Name = observed.name
	}
	block.OperationCategory = observed.category
	if !block.IsError && observed.instructionRead != "" {
		g.instructionReads[observed.instructionRead] = struct{}{}
	}
	delete(g.tools, block.ToolID)
	return true
}

func (g *streamEventIngester) noteLiveActivity(at time.Time, modelActivity bool, toolsChanged bool) {
	if at.After(g.liveLastEventAt) {
		g.liveLastEventAt = at
	}
	if modelActivity && at.After(g.liveLastModelActivityAt) {
		g.liveLastModelActivityAt = at
	}
	if g.liveBroken || g.closed {
		return
	}
	if !toolsChanged && at.Sub(g.liveLastWrite) < liveStatusWriteInterval {
		return
	}
	status := state.TaskLiveStatus{
		UpdatedAt:           at.UTC(),
		LastEventAt:         g.liveLastEventAt.UTC(),
		LastModelActivityAt: g.liveLastModelActivityAt.UTC(),
		Tools:               liveToolsSnapshot(g.tools),
	}
	if err := g.state.WriteTaskLiveStatus(g.base.TaskID, status); err != nil {
		state.WarnTaskLiveSkip(err)
		g.liveBroken = true
		return
	}
	g.liveLastWrite = at
}

func liveToolsSnapshot(tools map[string]toolUseObservation) []state.TaskLiveTool {
	if len(tools) == 0 {
		return nil
	}
	observations := make([]toolUseObservation, 0, len(tools))
	for _, observation := range tools {
		observations = append(observations, observation)
	}
	sort.Slice(observations, func(i, j int) bool { return observations[i].timestamp.Before(observations[j].timestamp) })
	result := make([]state.TaskLiveTool, 0, len(observations))
	for _, observation := range observations {
		result = append(result, state.TaskLiveTool{
			ToolID:     observation.toolID,
			Command:    observation.command,
			Purpose:    observation.purpose,
			Background: observation.background,
			WaitTaskID: observation.waitTaskID,
		})
	}
	return result
}

func extractLiveToolDetail(input json.RawMessage) liveToolDetail {
	if len(input) == 0 {
		return liveToolDetail{}
	}
	var parsed struct {
		Command         string `json:"command"`
		Description     string `json:"description"`
		RunInBackground bool   `json:"run_in_background"`
		TaskID          string `json:"task_id"`
		BashID          string `json:"bash_id"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil {
		return liveToolDetail{}
	}
	detail := liveToolDetail{
		command:    boundLiveText(parsed.Command, liveCommandMaxBytes),
		purpose:    boundLiveText(parsed.Description, livePurposeMaxBytes),
		background: parsed.RunInBackground,
		waitTaskID: parsed.TaskID,
	}
	if detail.waitTaskID == "" {
		detail.waitTaskID = parsed.BashID
	}
	return detail
}

func boundLiveText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func toolUseInputs(line []byte) map[string]json.RawMessage {
	var event struct {
		Message struct {
			Content []struct {
				Type  string          `json:"type"`
				ID    string          `json:"id"`
				Input json.RawMessage `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return nil
	}
	var inputs map[string]json.RawMessage
	for _, block := range event.Message.Content {
		if block.Type != "tool_use" || block.ID == "" {
			continue
		}
		if inputs == nil {
			inputs = make(map[string]json.RawMessage)
		}
		inputs[block.ID] = block.Input
	}
	return inputs
}

func (g *streamEventIngester) capturePlainSignal(line []byte) {
	var event streamEvent
	if json.Unmarshal(line, &event) == nil {
		return
	}
	g.plain = append(g.plain, line...)
	g.plain = append(g.plain, '\n')
	if excess := len(g.plain) - plainSignalMaxBytes; excess > 0 {
		g.plain = g.plain[excess:]
	}
}

func (g *streamEventIngester) plainSignal() string {
	return string(g.plain)
}

func streamResultEvent(line []byte) bool {
	var head struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(line, &head) == nil && head.Type == streamResultType
}

func reduceStreamEvent(line []byte, base state.TaskEventRecord, seq int, observedAt time.Time) state.TaskEventRecord {
	record := base
	record.Version = 0
	record.Seq = seq
	record.Timestamp = observedAt
	var event streamEvent
	if err := json.Unmarshal(line, &event); err != nil {
		record.Kind = "unknown"
		return record
	}
	record.Kind = event.Type
	record.Subtype = event.Subtype
	switch event.Type {
	case "system":
		record.MessageModel = event.Model
		if event.Subtype == "compact_boundary" {
			record.Compaction = reduceCompactionMetadata(event.CompactMetadata)
		}
	case "assistant", "user":
		var message streamMessage
		if err := json.Unmarshal(event.Message, &message); err == nil {
			record.MessageModel = message.Model
			record.Usage = message.Usage
			record.Blocks = reduceStreamBlocks(message.Content)
		}
	case streamResultType:
		record.IsError = event.IsError
		record.DurationMS = event.DurationMS
		record.DurationAPIMS = event.DurationAPIMS
		record.NumTurns = event.NumTurns
		record.TotalCostUSD = event.TotalCostUSD
		record.Usage = event.Usage
	}
	return record
}

func reduceStreamBlocks(content []json.RawMessage) []state.TaskBlockSummary {
	blocks := make([]state.TaskBlockSummary, 0, len(content))
	for _, raw := range content {
		var block streamBlock
		if err := json.Unmarshal(raw, &block); err != nil {
			blocks = append(blocks, state.TaskBlockSummary{Type: "unknown", Bytes: len(raw)})
			continue
		}
		blocks = append(blocks, state.TaskBlockSummary{
			Type:    block.Type,
			Name:    block.Name,
			ToolID:  blockToolID(block),
			Bytes:   len(raw),
			IsError: block.IsError,
		})
	}
	if len(blocks) == 0 {
		return nil
	}
	return blocks
}

func blockToolID(block streamBlock) string {
	if block.ToolUseID != "" {
		return block.ToolUseID
	}
	return block.ID
}
