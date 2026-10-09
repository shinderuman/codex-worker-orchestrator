package controller

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceParentEvidenceGroup struct {
	state.ParentEvidenceRecord
	Count    int       `json:"count"`
	LastTime time.Time `json:"last_time"`
}

type evidenceParentEvidenceAggregate struct {
	SchemaVersion      int                           `json:"schema_version"`
	Projection         string                        `json:"projection"`
	Source             string                        `json:"source"`
	SourceSHA256       string                        `json:"source_sha256"`
	SourceBytes        int64                         `json:"source_bytes"`
	Records            int                           `json:"records"`
	GroupCount         int                           `json:"group_count"`
	RepetitionsOmitted int                           `json:"repetitions_omitted"`
	FirstTime          time.Time                     `json:"first_time,omitempty"`
	LastTime           time.Time                     `json:"last_time,omitempty"`
	Outcomes           map[string]int                `json:"outcomes"`
	Surfaces           map[string]int                `json:"surfaces"`
	Groups             []evidenceParentEvidenceGroup `json:"groups"`
}

const (
	evidenceExportParentEvidenceFile      = "parent-evidence.jsonl"
	evidenceExportParentEvidenceAggregate = "parent-evidence.aggregate.json"

	evidenceExportParentEvidenceOmission = "duplicate parent-evidence record repetitions aggregated; the raw ledger remains with the canonical runtime state evidence"

	evidenceParentEvidenceAggregateSchema = 1
	evidenceParentEvidenceProjection      = "parent-evidence-repetition-aggregate"
)

func aggregateParentEvidence(data []byte, sourceEntry string) ([]byte, error) {
	aggregate := evidenceParentEvidenceAggregate{
		SchemaVersion: evidenceParentEvidenceAggregateSchema,
		Projection:    evidenceParentEvidenceProjection,
		Source:        sourceEntry,
		SourceSHA256:  digestBytes(data),
		SourceBytes:   int64(len(data)),
		Outcomes:      map[string]int{},
		Surfaces:      map[string]int{},
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var record state.ParentEvidenceRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, fmt.Errorf("parent evidence ledger record is invalid: %w", err)
		}
		aggregate.appendRecord(record)
	}
	aggregate.GroupCount = len(aggregate.Groups)
	aggregate.RepetitionsOmitted = aggregate.Records - aggregate.GroupCount
	data, err := json.MarshalIndent(aggregate, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (aggregate *evidenceParentEvidenceAggregate) appendRecord(record state.ParentEvidenceRecord) {
	aggregate.Records++
	aggregate.Outcomes[record.Outcome]++
	aggregate.Surfaces[record.Surface]++
	if aggregate.FirstTime.IsZero() {
		aggregate.FirstTime = record.Time
	}
	aggregate.LastTime = record.Time
	if last := len(aggregate.Groups) - 1; last >= 0 && sameParentEvidenceRepetition(aggregate.Groups[last].ParentEvidenceRecord, record) {
		aggregate.Groups[last].Count++
		aggregate.Groups[last].LastTime = record.Time
		return
	}
	aggregate.Groups = append(aggregate.Groups, evidenceParentEvidenceGroup{ParentEvidenceRecord: record, Count: 1, LastTime: record.Time})
}

func sameParentEvidenceRepetition(left, right state.ParentEvidenceRecord) bool {
	left.Time = time.Time{}
	right.Time = time.Time{}
	return left == right
}

func evidenceExportEphemeralStateFile(name string) bool {
	return name == "lock" || strings.HasSuffix(name, ".lock") || strings.HasSuffix(name, ".ready")
}
