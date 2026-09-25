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

type EventValidationObservation struct {
	Form       string `json:"form"`
	GateClass  string `json:"gate_class,omitempty"`
	Suite      string `json:"suite,omitempty"`
	SnapshotID string `json:"snapshot_id,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Attempt    string `json:"attempt,omitempty"`
	Result     string `json:"result,omitempty"`
}

type EventBlockEvidence struct {
	Type              string                       `json:"type"`
	Name              string                       `json:"name,omitempty"`
	OperationCategory string                       `json:"operation_category,omitempty"`
	Validation        []EventValidationObservation `json:"validation,omitempty"`
	Bytes             int                          `json:"bytes"`
	IsError           bool                         `json:"is_error,omitempty"`
	DurationMS        int64                        `json:"duration_ms,omitempty"`
}

type EventValidationEvidence struct {
	Attribution string `json:"attribution"`
	Source      string `json:"source"`
	Form        string `json:"form"`
	GateClass   string `json:"gate_class,omitempty"`
	Suite       string `json:"suite,omitempty"`
	SnapshotID  string `json:"snapshot_id,omitempty"`
	Phase       string `json:"phase,omitempty"`
	Attempt     string `json:"attempt,omitempty"`
	Scope       string `json:"scope,omitempty"`
	Result      string `json:"result"`
	ExitCode    int    `json:"exit_code,omitempty"`
	ExitSource  string `json:"exit_source,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	Evidence    string `json:"evidence,omitempty"`
}

type EventEvidence struct {
	Seq         int                      `json:"seq"`
	Kind        string                   `json:"kind"`
	Subtype     string                   `json:"subtype,omitempty"`
	SearchPaths []string                 `json:"search_paths,omitempty"`
	Blocks      []EventBlockEvidence     `json:"blocks,omitempty"`
	Validation  *EventValidationEvidence `json:"validation,omitempty"`
	IsError     bool                     `json:"is_error,omitempty"`
	DurationMS  int64                    `json:"duration_ms,omitempty"`
}

type InputItem struct {
	CallID              string          `json:"call_id"`
	Role                string          `json:"role"`
	Phase               string          `json:"phase"`
	PacketStatus        string          `json:"packet_status"`
	Outcome             string          `json:"outcome"`
	StartedAt           string          `json:"started_at"`
	DurationMS          int64           `json:"duration_ms"`
	Usage               UsageProjection `json:"usage"`
	SourceEvidenceBytes int             `json:"source_evidence_bytes"`
	ToolUseCounts       map[string]int  `json:"tool_use_counts,omitempty"`
	Events              []EventEvidence `json:"events,omitempty"`
	Summary             string          `json:"summary"`
	Issues              string          `json:"issues"`
	Decision            string          `json:"decision"`
}

type ShadowInput struct {
	Schema              string            `json:"schema"`
	TaskID              string            `json:"bundle_task_id"`
	ItemsSHA256         string            `json:"items_sha256"`
	SourceFiles         map[string]string `json:"source_files"`
	SourceEvidenceBytes int               `json:"source_evidence_bytes"`
	Items               []InputItem       `json:"items"`
}

type packetProjection struct {
	Summary  string `json:"summary"`
	Issues   string `json:"issues"`
	Decision string `json:"decision"`
}

const InputSchema = "system-one-shadow-input/v1"

const packetTextBoundBytes = 768

const packetTextOmissionMarker = "[前方を省略] "

const (
	eventEvidenceMaxItems   = 32
	eventSearchPathMaxItems = 16
	eventBlockMaxItems      = 24
	eventValidationMaxItems = 12
)

func BuildInput(taskID string, logs []state.ModelCallLog, records []state.TaskEventRecord) (ShadowInput, error) {
	toolCounts := toolUseCountsByCall(records)
	events := eventEvidenceByCall(records)
	sourceBytesByCall, totalSourceBytes, err := sourceEvidenceByteCounts(logs, records)
	if err != nil {
		return ShadowInput{}, err
	}
	items := make([]InputItem, 0, len(logs))
	for _, log := range logs {
		if log.CallType != state.CallTypeTask {
			continue
		}
		packet := packetProjectionFromResponse(log.Response)
		items = append(items, InputItem{
			CallID:              log.CallID,
			Role:                string(log.Role),
			Phase:               log.Phase,
			PacketStatus:        log.PacketStatus,
			Outcome:             log.Outcome,
			StartedAt:           log.StartedAt.UTC().Format(time.RFC3339Nano),
			DurationMS:          log.WallDurationMS,
			SourceEvidenceBytes: sourceBytesByCall[log.CallID],
			Usage: UsageProjection{
				InputTokens:              log.TreeUsage.InputTokens,
				CacheCreationInputTokens: log.TreeUsage.CacheCreationInputTokens,
				CacheReadInputTokens:     log.TreeUsage.CacheReadInputTokens,
				OutputTokens:             log.TreeUsage.OutputTokens,
			},
			ToolUseCounts: toolCounts[log.CallID],
			Events:        events[log.CallID],
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
		SourceEvidenceBytes: totalSourceBytes,
		Items:               items,
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

func sourceEvidenceByteCounts(logs []state.ModelCallLog, records []state.TaskEventRecord) (map[string]int, int, error) {
	byCall := make(map[string]int)
	total := 0
	for _, log := range logs {
		data, err := json.Marshal(log)
		if err != nil {
			return nil, 0, fmt.Errorf("shadow telemetry sourceをJSON化できません: %w", err)
		}
		size := len(data) + 1
		total += size
		if log.CallType == state.CallTypeTask && log.CallID != "" {
			byCall[log.CallID] += size
		}
	}
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			return nil, 0, fmt.Errorf("shadow event sourceをJSON化できません: %w", err)
		}
		size := len(data) + 1
		total += size
		if record.CallID != "" {
			byCall[record.CallID] += size
		}
	}
	return byCall, total, nil
}

func eventEvidenceByCall(records []state.TaskEventRecord) map[string][]EventEvidence {
	result := make(map[string][]EventEvidence)
	for _, record := range records {
		if record.CallID == "" || len(result[record.CallID]) >= eventEvidenceMaxItems {
			continue
		}
		result[record.CallID] = append(result[record.CallID], projectEventEvidence(record))
	}
	return result
}

func projectEventEvidence(record state.TaskEventRecord) EventEvidence {
	evidence := EventEvidence{
		Seq:        record.Seq,
		Kind:       boundedPacketText(record.Kind),
		Subtype:    boundedPacketText(record.Subtype),
		IsError:    record.IsError,
		DurationMS: record.DurationMS,
	}
	for _, path := range record.SearchPaths {
		if len(evidence.SearchPaths) >= eventSearchPathMaxItems {
			break
		}
		evidence.SearchPaths = append(evidence.SearchPaths, boundedPacketText(path))
	}
	for _, block := range record.Blocks {
		if len(evidence.Blocks) >= eventBlockMaxItems {
			break
		}
		projected := EventBlockEvidence{
			Type:              boundedPacketText(block.Type),
			Name:              boundedPacketText(block.Name),
			OperationCategory: boundedPacketText(block.OperationCategory),
			Bytes:             block.Bytes,
			IsError:           block.IsError,
			DurationMS:        block.DurationMS,
		}
		for _, observation := range block.Validation {
			if len(projected.Validation) >= eventValidationMaxItems {
				break
			}
			projected.Validation = append(projected.Validation, EventValidationObservation{
				Form:       boundedPacketText(observation.Form),
				GateClass:  boundedPacketText(observation.GateClass),
				Suite:      boundedPacketText(observation.Suite),
				SnapshotID: boundedPacketText(observation.SnapshotID),
				Phase:      boundedPacketText(observation.Phase),
				Attempt:    boundedPacketText(observation.Attempt),
				Result:     boundedPacketText(observation.Result),
			})
		}
		evidence.Blocks = append(evidence.Blocks, projected)
	}
	if record.Validation != nil {
		validation := record.Validation
		evidence.Validation = &EventValidationEvidence{
			Attribution: boundedPacketText(validation.Attribution),
			Source:      boundedPacketText(validation.Source),
			Form:        boundedPacketText(validation.Form),
			GateClass:   boundedPacketText(validation.GateClass),
			Suite:       boundedPacketText(validation.Suite),
			SnapshotID:  boundedPacketText(validation.SnapshotID),
			Phase:       boundedPacketText(validation.Phase),
			Attempt:     boundedPacketText(validation.Attempt),
			Scope:       boundedPacketText(validation.Scope),
			Result:      boundedPacketText(validation.Result),
			ExitCode:    validation.ExitCode,
			ExitSource:  boundedPacketText(validation.ExitSource),
			DurationMS:  validation.DurationMS,
			Evidence:    boundedPacketText(validation.Evidence),
		}
	}
	return evidence
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
