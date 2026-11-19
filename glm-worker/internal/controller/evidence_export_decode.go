package controller

import (
    "encoding/json"
    "strings"

    "github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// Each source belongs to a single export snapshot. A malformed, changing or
// cross-task stream is not usable by any projection of that export.
type evidenceExportDecodedSource[T any] struct {
    records []T
    status string
}

type evidenceExportDecodedRecords struct {
    telemetry evidenceExportDecodedSource[state.ModelCallLog]
    lifecycle evidenceExportDecodedSource[state.TaskLifecycleRecord]
    rounds evidenceExportDecodedSource[state.RoundRecord]
    events evidenceExportDecodedSource[state.TaskEventRecord]
}

func (b *evidenceExportBuilder) exportDecodedEvidence() *evidenceExportDecodedRecords {
    if b.decodedEvidence != nil {
        return b.decodedEvidence
    }
    taskID := b.workflowRuntimeTaskID()
    b.decodedEvidence = &evidenceExportDecodedRecords{
        telemetry: decodeExportRecordSource(b, "telemetry.jsonl", taskID, parseWorkflowTelemetry,
            func(record state.ModelCallLog, taskID string) bool { return record.TaskID == taskID }),
        lifecycle: decodeExportRecordSource(b, "lifecycle.jsonl", taskID, parseWorkflowLifecycle,
            func(record state.TaskLifecycleRecord, taskID string) bool { return record.TaskID == taskID && !record.Timestamp.IsZero() && !record.Timestamp.After(b.observedAt) }),
        rounds: decodeExportRecordSource(b, "rounds.jsonl", taskID, parseWorkflowRounds,
            func(record state.RoundRecord, taskID string) bool { return record.TaskID == taskID && record.Seq > 0 && !record.CapturedAt.IsZero() && !record.CapturedAt.After(b.observedAt) }),
        events: decodeExportRecordSource(b, "events.jsonl", taskID, parseExportTaskEvents,
            func(record state.TaskEventRecord, taskID string) bool {
                // Validation evidence is emitted without a per-call event sequence.
                // Preserve that canonical zero-sequence record, but reject an
                // unsequenced stream event or negative sequence.
                return record.TaskID == taskID &&
                    (record.Seq > 0 || (record.Seq == 0 && record.Kind == taskEventKindValidation)) &&
                    !record.Timestamp.IsZero() && !record.Timestamp.After(b.observedAt)
            }),
    }
    return b.decodedEvidence
}

func (b *evidenceExportBuilder) exportRecordBytes(name string) (string, []byte, bool) {
    for _, path := range []string{"live/task/" + name, "bound/task/" + name, "attempt/" + name} {
        if data, ok := b.fileBytes(path); ok {
            return path, data, true
        }
    }
    return "", nil, false
}

func (b *evidenceExportBuilder) exportRecordSourcePartial(path string) bool {
    for _, entry := range b.entries {
        if entry.Path != path {
            continue
        }
        return entry.Changing || entry.TrailingFragment || entry.Missing ||
            entry.Unreadable != "" || entry.Unattributed != ""
    }
    return false
}

func decodeExportRecordSource[T any](b *evidenceExportBuilder, name, taskID string,
    parse func([]byte, bool) []T, matches func(T, string) bool,
) evidenceExportDecodedSource[T] {
    path, data, ok := b.exportRecordBytes(name)
    if !ok {
        return evidenceExportDecodedSource[T]{status: evidenceAnalysisStatusAbsent}
    }
    if b.exportRecordSourcePartial(path) {
        return evidenceExportDecodedSource[T]{status: evidenceAnalysisStatusPartial}
    }
    // An empty rounds file is an observed zero-round stream for workflow.
    // Other empty streams have no records and remain absent in the index.
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
    // InProgress denotes a live, appendable file, not a failed snapshot
    // read. analysisStates separately exposes its open-source provenance.
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
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var record state.RoundRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil
		}
		if record.Seq <= 0 || record.CapturedAt.IsZero() {
			return nil
		}
		records = append(records, record)
	}
	return records
}

