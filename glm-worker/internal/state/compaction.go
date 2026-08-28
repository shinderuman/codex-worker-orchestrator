package state

import (
	"slices"
	"strings"
	"time"
)

type TaskEvents struct {
	TaskID  string
	Records []TaskEventRecord
}

type CompactionBoundaryObservation struct {
	TaskID           string    `json:"task_id"`
	CallID           string    `json:"call_id"`
	SessionID        string    `json:"session_id"`
	Role             string    `json:"role"`
	Phase            string    `json:"phase"`
	PhaseCategory    string    `json:"phase_category"`
	Resumed          bool      `json:"resumed"`
	SessionCallIndex int       `json:"session_call_index"`
	Seq              int       `json:"seq"`
	At               time.Time `json:"at"`
	CallEvents       int       `json:"call_events"`
	CallTurns        int       `json:"call_turns"`
}

type CompactionGroupTotal struct {
	Role          string `json:"role"`
	PhaseCategory string `json:"phase_category"`
	Resumed       bool   `json:"resumed"`
	Calls         int    `json:"calls"`
	Boundaries    int    `json:"boundaries"`
}

type CompactionReport struct {
	BoundarySubtype     string                          `json:"boundary_subtype"`
	Tasks               int                             `json:"tasks"`
	CallsObserved       int                             `json:"calls_observed"`
	CallsWithBoundaries int                             `json:"calls_with_boundaries"`
	Boundaries          int                             `json:"boundaries"`
	Groups              []CompactionGroupTotal          `json:"groups"`
	Observations        []CompactionBoundaryObservation `json:"observations"`
}

type compactionGroupKey struct {
	role          string
	phaseCategory string
	resumed       bool
}

type compactionBuilder struct {
	report          CompactionReport
	groupCalls      map[compactionGroupKey]map[string]bool
	groupBoundaries map[compactionGroupKey]int
}

const CompactionBoundarySubtype = "compact_boundary"

func IsCompactionBoundaryEvent(record TaskEventRecord) bool {
	return record.Kind == "system" && record.Subtype == CompactionBoundarySubtype
}

func BuildCompactionReport(tasks []TaskEvents) CompactionReport {
	builder := newCompactionBuilder()
	for _, task := range tasks {
		builder.absorbTask(task)
	}
	return builder.build()
}

func newCompactionBuilder() *compactionBuilder {
	return &compactionBuilder{
		report: CompactionReport{
			BoundarySubtype: CompactionBoundarySubtype,
			Groups:          []CompactionGroupTotal{},
			Observations:    []CompactionBoundaryObservation{},
		},
		groupCalls:      make(map[compactionGroupKey]map[string]bool),
		groupBoundaries: make(map[compactionGroupKey]int),
	}
}

func (b *compactionBuilder) absorbTask(task TaskEvents) {
	if len(task.Records) == 0 {
		return
	}
	b.report.Tasks++
	entries := CallsFromTaskEvents(task.Records)
	b.report.CallsObserved += len(entries)
	byCall := make(map[string]CallTimelineEntry, len(entries))
	for _, entry := range entries {
		byCall[entry.CallID] = entry
	}
	boundaryCalls := b.absorbBoundaries(task.TaskID, task.Records, byCall)
	b.absorbBoundaryCalls(byCall, boundaryCalls)
}

func (b *compactionBuilder) absorbBoundaries(
	taskID string,
	records []TaskEventRecord,
	byCall map[string]CallTimelineEntry,
) map[string]bool {
	boundaryCalls := make(map[string]bool)
	for _, record := range records {
		if !IsCompactionBoundaryEvent(record) {
			continue
		}
		entry := byCall[record.CallID]
		b.report.Observations = append(b.report.Observations, CompactionBoundaryObservation{
			TaskID:           taskID,
			CallID:           record.CallID,
			SessionID:        entry.SessionID,
			Role:             entry.Role,
			Phase:            entry.Phase,
			PhaseCategory:    WorkerPhaseCategory(entry.Phase),
			Resumed:          entry.Resumed,
			SessionCallIndex: entry.SessionCallIndex,
			Seq:              record.Seq,
			At:               record.Timestamp,
			CallEvents:       entry.Events,
			CallTurns:        entry.NumTurns,
		})
		b.groupBoundaries[compactionGroupKey{
			role:          entry.Role,
			phaseCategory: WorkerPhaseCategory(entry.Phase),
			resumed:       entry.Resumed,
		}]++
		boundaryCalls[record.CallID] = true
	}
	return boundaryCalls
}

func (b *compactionBuilder) absorbBoundaryCalls(byCall map[string]CallTimelineEntry, boundaryCalls map[string]bool) {
	for callID := range boundaryCalls {
		b.report.CallsWithBoundaries++
		entry := byCall[callID]
		key := compactionGroupKey{
			role:          entry.Role,
			phaseCategory: WorkerPhaseCategory(entry.Phase),
			resumed:       entry.Resumed,
		}
		if b.groupCalls[key] == nil {
			b.groupCalls[key] = make(map[string]bool)
		}
		b.groupCalls[key][callID] = true
	}
}

func (b *compactionBuilder) build() CompactionReport {
	b.report.Groups = make([]CompactionGroupTotal, 0, len(b.groupBoundaries))
	for key, boundaries := range b.groupBoundaries {
		b.report.Groups = append(b.report.Groups, CompactionGroupTotal{
			Role: key.role, PhaseCategory: key.phaseCategory, Resumed: key.resumed,
			Calls: len(b.groupCalls[key]), Boundaries: boundaries,
		})
	}
	slices.SortFunc(b.report.Groups, compareCompactionGroup)
	slices.SortFunc(b.report.Observations, compareCompactionObservation)
	b.report.Boundaries = len(b.report.Observations)
	return b.report
}

func compareCompactionObservation(a, b CompactionBoundaryObservation) int {
	if !a.At.Equal(b.At) {
		if a.At.Before(b.At) {
			return -1
		}
		return 1
	}
	if a.TaskID != b.TaskID {
		return strings.Compare(a.TaskID, b.TaskID)
	}
	if a.CallID != b.CallID {
		return strings.Compare(a.CallID, b.CallID)
	}
	if a.Seq != b.Seq {
		if a.Seq < b.Seq {
			return -1
		}
		return 1
	}
	return 0
}

func compareCompactionGroup(a, b CompactionGroupTotal) int {
	if a.Role != b.Role {
		return strings.Compare(a.Role, b.Role)
	}
	if a.PhaseCategory != b.PhaseCategory {
		return strings.Compare(a.PhaseCategory, b.PhaseCategory)
	}
	if a.Resumed == b.Resumed {
		return 0
	}
	if !a.Resumed {
		return -1
	}
	return 1
}
