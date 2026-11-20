package controller

import (
	"encoding/json"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceExportDecodedSource[T any] struct {
	records []T
	status  string
}

type evidenceExportDecodedRecords struct {
	telemetry evidenceExportDecodedSource[state.ModelCallLog]
	lifecycle evidenceExportDecodedSource[state.TaskLifecycleRecord]
	rounds    evidenceExportDecodedSource[state.RoundRecord]
	events    evidenceExportDecodedSource[state.TaskEventRecord]
}

func (records *evidenceExportDecodedRecords) isPartial() bool {
	return records.telemetry.status == evidenceAnalysisStatusPartial ||
		records.lifecycle.status == evidenceAnalysisStatusPartial ||
		records.rounds.status == evidenceAnalysisStatusPartial ||
		records.events.status == evidenceAnalysisStatusPartial
}

func (b *evidenceExportBuilder) exportDecodedEvidence() *evidenceExportDecodedRecords {
	if b.decodedEvidence != nil {
		return b.decodedEvidence
	}
	taskID := b.workflowRuntimeTaskID()
	b.decodedEvidence = &evidenceExportDecodedRecords{
		telemetry: b.decodeExportTelemetry(taskID),
		lifecycle: decodeExportRecordSource(b, "lifecycle.jsonl", taskID, parseWorkflowLifecycle,
			func(record state.TaskLifecycleRecord, taskID string) bool {
				return record.TaskID == taskID && !record.Timestamp.IsZero() && !record.Timestamp.After(b.observedAt)
			}),
		rounds: decodeExportRecordSource(b, "rounds.jsonl", taskID, parseWorkflowRounds,
			func(record state.RoundRecord, taskID string) bool {
				return record.TaskID == taskID && record.Seq > 0 && !record.CapturedAt.IsZero() && !record.CapturedAt.After(b.observedAt)
			}),
		events: decodeExportRecordSource(b, "events.jsonl", taskID, parseExportTaskEvents,
			func(record state.TaskEventRecord, taskID string) bool {
				return record.TaskID == taskID &&
					(record.Seq > 0 || (record.Seq == 0 && record.Kind == taskEventKindValidation)) &&
					!record.Timestamp.IsZero() && !record.Timestamp.After(b.observedAt)
			}),
	}
	return b.decodedEvidence
}

func (b *evidenceExportBuilder) decodeExportTelemetry(taskID string) evidenceExportDecodedSource[state.ModelCallLog] {
	source := decodeExportRecordSource(b, "telemetry.jsonl", taskID, parseWorkflowTelemetry,
		func(record state.ModelCallLog, taskID string) bool {
			return record.TaskID == taskID && !record.StartedAt.IsZero() && !record.CompletedAt.Before(record.StartedAt)
		})
	if source.status != evidenceAnalysisStatusCollected {
		return source
	}
	observed := source.records[:0]
	for _, record := range source.records {
		if !record.CompletedAt.After(b.observedAt) {
			observed = append(observed, record)
		}
	}
	if len(observed) == 0 {
		return evidenceExportDecodedSource[state.ModelCallLog]{status: evidenceAnalysisStatusAbsent}
	}
	source.records = observed
	return source
}

func (b *evidenceExportBuilder) exportRecordBytes(name string) ([]byte, string) {
	for _, path := range []string{"live/task/" + name, "bound/task/" + name, "attempt/" + name} {
		data, present := b.fileBytes(path)
		if b.exportRecordSourcePartial(path, present) {
			return nil, evidenceAnalysisStatusPartial
		}
		if present {
			return data, evidenceAnalysisStatusCollected
		}
	}
	return nil, evidenceAnalysisStatusAbsent
}

func (b *evidenceExportBuilder) exportRecordSourcePartial(path string, bytesPresent bool) bool {
	for _, entry := range b.entries {
		if entry.Path != path {
			continue
		}
		return entry.Changing || entry.TrailingFragment || (bytesPresent && entry.Missing) ||
			entry.Unreadable != "" || entry.Unattributed != ""
	}
	return false
}

func decodeExportRecordSource[T any](b *evidenceExportBuilder, name, taskID string,
	parse func([]byte, bool) []T, matches func(T, string) bool,
) evidenceExportDecodedSource[T] {
	data, status := b.exportRecordBytes(name)
	if status != evidenceAnalysisStatusCollected {
		return evidenceExportDecodedSource[T]{status: status}
	}
	if len(data) == 0 {
		return evidenceExportDecodedSource[T]{records: parse(data, true), status: evidenceAnalysisStatusAbsent}
	}
	records := parse(data, true)
	if records == nil || taskID == "" {
		return evidenceExportDecodedSource[T]{status: evidenceAnalysisStatusPartial}
	}
	for _, record := range records {
		if !matches(record, taskID) {
			return evidenceExportDecodedSource[T]{status: evidenceAnalysisStatusPartial}
		}
	}
	return evidenceExportDecodedSource[T]{records: records, status: evidenceAnalysisStatusCollected}
}

func parseExportTaskEvents(data []byte, ok bool) []state.TaskEventRecord {
	if !ok || len(data) == 0 {
		return nil
	}
	var records []state.TaskEventRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseTaskEventLine([]byte(line))
		if err != nil {
			return nil
		}
		records = append(records, record)
	}
	return records
}

func parseWorkflowTelemetry(data []byte, ok bool) []state.ModelCallLog {
	if !ok || len(data) == 0 {
		return nil
	}
	var logs []state.ModelCallLog
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var log state.ModelCallLog
		if err := json.Unmarshal([]byte(line), &log); err != nil {
			return nil
		}
		if log.CallID == "" || log.Version != state.ModelCallLogVersion {
			return nil
		}
		logs = append(logs, log)
	}
	return logs
}

func parseWorkflowLifecycle(data []byte, ok bool) []state.TaskLifecycleRecord {
	if !ok || len(data) == 0 {
		return nil
	}
	var records []state.TaskLifecycleRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseTaskLifecycleLine([]byte(line))
		if err != nil {
			return nil
		}
		records = append(records, record)
	}
	return records
}

func parseWorkflowRounds(data []byte, ok bool) []state.RoundRecord {
	if !ok {
		return nil
	}
	records := make([]state.RoundRecord, 0)
	previousSeq := 0
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseRoundLine([]byte(line))
		if err != nil || record.Seq <= previousSeq || record.CapturedAt.IsZero() {
			return nil
		}
		records = append(records, record)
		previousSeq = record.Seq
	}
	return records
}
