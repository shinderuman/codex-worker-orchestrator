package codexrollout

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

type Activity struct {
	ModelTurns      int
	ToolOutputBytes int64
	Compactions     int
}

type activityTurn struct {
	StartedAt   time.Time
	CompletedAt time.Time
	HasStart    bool
	HasComplete bool
}

type rolloutItemPayload struct {
	Type   string          `json:"type"`
	Output json.RawMessage `json:"output"`
}

const (
	rolloutTaskStartedType          = "task_started"
	rolloutTaskCompleteType         = "task_complete"
	rolloutFunctionCallOutputType   = "function_call_output"
	rolloutCustomToolCallOutputType = "custom_tool_call_output"
	rolloutCompactedType            = "compacted"
)

func ScanActivity(chain []Rollout, start, end time.Time) (Activity, error) {
	turns := make(map[string]*activityTurn)
	activity := Activity{}
	for _, member := range chain {
		if err := scanActivityMember(member, start, end, turns, &activity); err != nil {
			return Activity{}, err
		}
	}
	for _, turn := range turns {
		if !turn.HasStart || turn.StartedAt.After(end) {
			continue
		}
		if turn.HasComplete && turn.CompletedAt.Before(start) {
			continue
		}
		activity.ModelTurns++
	}
	return activity, nil
}

func scanActivityMember(member Rollout, start, end time.Time, turns map[string]*activityTurn, activity *Activity) error {
	file, err := os.Open(member.AbsolutePath)
	if err != nil {
		return fmt.Errorf("parent rolloutを開けません: %w", err)
	}
	defer func() { _ = file.Close() }()
	reader := bufio.NewReaderSize(file, 64*1024)
	lineNumber := 0
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineNumber++
			if err := observeActivityLine(line, lineNumber, start, end, turns, activity); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return fmt.Errorf("parent rolloutを読めません: %w", readErr)
		}
	}
}

func observeActivityLine(line []byte, lineNumber int, start, end time.Time, turns map[string]*activityTurn, activity *Activity) error {
	trimmed := line
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == '\n' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	if len(trimmed) == 0 {
		return nil
	}
	var record rolloutScanLine
	if err := json.Unmarshal(trimmed, &record); err != nil {
		return fmt.Errorf("parent rollout %d行目のJSONを解析できません: %w", lineNumber, err)
	}
	at, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	if err != nil {
		return fmt.Errorf("parent rollout %d行目のtimestampを解析できません: %w", lineNumber, err)
	}
	if record.Type == "event_msg" {
		observeActivityEvent(record.Payload, at, turns)
	}
	if at.Before(start) || at.After(end) {
		return nil
	}
	observeWindowedActivity(record, activity)
	return nil
}

func observeWindowedActivity(record rolloutScanLine, activity *Activity) {
	if record.Type == "response_item" {
		var item rolloutItemPayload
		if err := json.Unmarshal(record.Payload, &item); err == nil {
			switch item.Type {
			case rolloutFunctionCallOutputType, rolloutCustomToolCallOutputType:
				activity.ToolOutputBytes += toolOutputBytes(item.Output)
			}
		}
	}
	if record.Type == rolloutCompactedType {
		activity.Compactions++
	}
}

func observeActivityEvent(raw json.RawMessage, at time.Time, turns map[string]*activityTurn) {
	var payload rolloutEventPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.TurnID == "" {
		return
	}
	turn := turns[payload.TurnID]
	if turn == nil {
		turn = &activityTurn{}
		turns[payload.TurnID] = turn
	}
	if payload.Type == rolloutTaskStartedType && !turn.HasStart {
		turn.StartedAt = at
		turn.HasStart = true
	}
	if payload.Type == rolloutTaskCompleteType && !turn.HasComplete {
		turn.CompletedAt = at
		turn.HasComplete = true
	}
}

func toolOutputBytes(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return int64(len(text))
	}
	var items []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return 0
	}
	var total int64
	for _, item := range items {
		total += int64(len(item.Text))
	}
	return total
}
