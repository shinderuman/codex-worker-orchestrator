package shadoweval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type UsageProjection struct {
	InputTokens              int64 `json:"input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
}

type InputItem struct {
	CallID        string          `json:"call_id"`
	Role          string          `json:"role"`
	Phase         string          `json:"phase"`
	PacketStatus  string          `json:"packet_status"`
	Outcome       string          `json:"outcome"`
	StartedAt     string          `json:"started_at"`
	DurationMS    int64           `json:"duration_ms"`
	Usage         UsageProjection `json:"usage"`
	ToolUseCounts map[string]int  `json:"tool_use_counts,omitempty"`
	Summary       string          `json:"summary"`
	Issues        string          `json:"issues"`
	Decision      string          `json:"decision"`
}

type ShadowInput struct {
	Schema      string            `json:"schema"`
	TaskID      string            `json:"bundle_task_id"`
	ItemsSHA256 string            `json:"items_sha256"`
	SourceFiles map[string]string `json:"source_files"`
	Items       []InputItem       `json:"items"`
}

type packetProjection struct {
	Summary  string `json:"summary"`
	Issues   string `json:"issues"`
	Decision string `json:"decision"`
}

const InputSchema = "system-one-shadow-input/v1"

const packetTextBoundBytes = 768

const packetTextOmissionMarker = "[前方を省略] "

func BuildInput(taskID string, logs []state.ModelCallLog, records []state.TaskEventRecord) (ShadowInput, error) {
	toolCounts := toolUseCountsByCall(records)
	items := make([]InputItem, 0, len(logs))
	for _, log := range logs {
		if log.CallType != state.CallTypeTask {
			continue
		}
		packet := packetProjectionFromResponse(log.Response)
		items = append(items, InputItem{
			CallID:       log.CallID,
			Role:         string(log.Role),
			Phase:        log.Phase,
			PacketStatus: log.PacketStatus,
			Outcome:      log.Outcome,
			StartedAt:    log.StartedAt.UTC().Format(time.RFC3339Nano),
			DurationMS:   log.WallDurationMS,
			Usage: UsageProjection{
				InputTokens:              log.TreeUsage.InputTokens,
				CacheCreationInputTokens: log.TreeUsage.CacheCreationInputTokens,
				CacheReadInputTokens:     log.TreeUsage.CacheReadInputTokens,
				OutputTokens:             log.TreeUsage.OutputTokens,
			},
			ToolUseCounts: toolCounts[log.CallID],
			Summary:       boundedPacketText(packet.Summary),
			Issues:        boundedPacketText(packet.Issues),
			Decision:      boundedPacketText(packet.Decision),
		})
	}
	if len(items) == 0 {
		return ShadowInput{}, fmt.Errorf("task %sのshadow評価対象となるtask呼出telemetryがありません", taskID)
	}
	input := ShadowInput{
		Schema: InputSchema,
		TaskID: taskID,
		SourceFiles: map[string]string{
			"telemetry": "telemetry/" + taskID + ".jsonl",
			"events":    "events/" + taskID + ".jsonl",
		},
		Items: items,
	}
	digest, err := ItemsSHA256(items)
	if err != nil {
		return ShadowInput{}, err
	}
	input.ItemsSHA256 = digest
	return input, nil
}

func MarshalInput(input ShadowInput) ([]byte, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("shadow入力をJSON化できません: %w", err)
	}
	return data, nil
}

func ItemsSHA256(items []InputItem) (string, error) {
	data, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("shadow入力itemsをJSON化できません: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func packetProjectionFromResponse(response string) packetProjection {
	if response == "" {
		return packetProjection{}
	}
	var packet packetProjection
	if err := json.Unmarshal([]byte(response), &packet); err != nil {
		return packetProjection{}
	}
	return packet
}

func boundedPacketText(text string) string {
	if len(text) <= packetTextBoundBytes {
		return text
	}
	tail := []byte(text)
	start := len(tail) - (packetTextBoundBytes - len(packetTextOmissionMarker))
	for start < len(tail) && tail[start]&0xC0 == 0x80 {
		start++
	}
	return packetTextOmissionMarker + string(tail[start:])
}

func toolUseCountsByCall(records []state.TaskEventRecord) map[string]map[string]int {
	counts := make(map[string]map[string]int)
	for _, call := range state.CallsFromTaskEvents(records) {
		if len(call.Tools) == 0 {
			continue
		}
		perCall := make(map[string]int, len(call.Tools))
		for _, tool := range call.Tools {
			if tool.Uses > 0 {
				perCall[tool.Name] = tool.Uses
			}
		}
		if len(perCall) > 0 {
			counts[call.CallID] = perCall
		}
	}
	return counts
}
