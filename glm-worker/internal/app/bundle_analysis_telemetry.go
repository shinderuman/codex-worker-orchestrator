package app

import (
	"bufio"
	"encoding/json"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type analysisTelemetryRecord struct {
	Version     int    `json:"version"`
	CallID      string `json:"call_id"`
	RetryOf     string `json:"retry_of"`
	RetryReason string `json:"retry_reason"`
	Phase       string `json:"phase"`
	Outcome     string `json:"outcome"`
	Resumed     bool   `json:"resumed"`
}

type analysisTelemetryVariant struct {
	RetryOf     string
	RetryReason string
	Phase       string
	Outcome     string
	Resumed     bool
	Lines       []int
}

type analysisTelemetryCall struct {
	CallID   string
	Variants []analysisTelemetryVariant
	Lines    []int
}

type analysisTelemetryScan struct {
	status string
	calls  []analysisTelemetryCall
}

func (call analysisTelemetryCall) conflicted() bool {
	return len(call.Variants) > 1
}

func scanAnalysisTelemetryCalls(st *state.StateStore, taskID string) analysisTelemetryScan {
	file, err := os.Open(st.ModelCallLogPath(taskID))
	if err != nil {
		return analysisTelemetryScan{status: analysisStatusMissing}
	}
	defer func() { _ = file.Close() }()

	scan := analysisTelemetryScan{status: analysisStatusAvailable}
	groups := map[string]int{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		var record analysisTelemetryRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return analysisTelemetryScan{status: analysisStatusMissing}
		}
		if record.Version != state.ModelCallLogVersion || record.CallID == "" {
			continue
		}
		index, known := groups[record.CallID]
		if !known {
			scan.calls = append(scan.calls, analysisTelemetryCall{
				CallID:   record.CallID,
				Lines:    []int{lineNumber},
				Variants: []analysisTelemetryVariant{analysisTelemetryVariantFrom(record, lineNumber)},
			})
			groups[record.CallID] = len(scan.calls) - 1
			continue
		}
		scan.mergeTelemetryCall(index, record, lineNumber)
	}
	if err := scanner.Err(); err != nil {
		return analysisTelemetryScan{status: analysisStatusMissing}
	}
	return scan
}

func (scan *analysisTelemetryScan) mergeTelemetryCall(index int, record analysisTelemetryRecord, lineNumber int) {
	call := &scan.calls[index]
	call.Lines = append(call.Lines, lineNumber)
	for i := range call.Variants {
		if analysisTelemetryVariantMatches(call.Variants[i], record) {
			call.Variants[i].Lines = append(call.Variants[i].Lines, lineNumber)
			return
		}
	}
	call.Variants = append(call.Variants, analysisTelemetryVariantFrom(record, lineNumber))
}

func analysisTelemetryVariantFrom(record analysisTelemetryRecord, lineNumber int) analysisTelemetryVariant {
	return analysisTelemetryVariant{
		RetryOf:     record.RetryOf,
		RetryReason: record.RetryReason,
		Phase:       record.Phase,
		Outcome:     record.Outcome,
		Resumed:     record.Resumed,
		Lines:       []int{lineNumber},
	}
}

func analysisTelemetryVariantMatches(variant analysisTelemetryVariant, record analysisTelemetryRecord) bool {
	return variant.RetryOf == record.RetryOf && variant.RetryReason == record.RetryReason &&
		variant.Phase == record.Phase && variant.Outcome == record.Outcome && variant.Resumed == record.Resumed
}

func analysisModelCallRelations(scan analysisTelemetryScan, taskID string) bundleAnalysisModelRelations {
	relations := bundleAnalysisModelRelations{Status: scan.status}
	if scan.status != analysisStatusAvailable {
		return relations
	}
	archivePath := bundleTelemetryArchivePrefix + taskID + ".jsonl"
	known, conflicted := analysisRelationCallIDs(scan.calls, &relations)
	for _, call := range scan.calls {
		if call.conflicted() {
			analysisAppendSourceConflictedRelations(&relations, call, archivePath, conflicted)
			continue
		}
		analysisAppendModelCallRelation(&relations, call, archivePath, known, conflicted)
	}
	return relations
}

func analysisRelationCallIDs(calls []analysisTelemetryCall, relations *bundleAnalysisModelRelations) (map[string]struct{}, map[string]struct{}) {
	known := make(map[string]struct{}, len(calls))
	conflicted := map[string]struct{}{}
	for _, call := range calls {
		if !call.conflicted() {
			known[call.CallID] = struct{}{}
			continue
		}
		conflicted[call.CallID] = struct{}{}
		relations.DuplicateCallIDs = append(relations.DuplicateCallIDs, bundleAnalysisDuplicateCalls{
			CallID: call.CallID,
			Lines:  call.Lines,
		})
	}
	return known, conflicted
}

func analysisAppendSourceConflictedRelations(relations *bundleAnalysisModelRelations, call analysisTelemetryCall, archivePath string, conflicted map[string]struct{}) {
	for _, variant := range call.Variants {
		relation := analysisAmbiguousRelationFrom(call.CallID, variant, archivePath, []string{analysisAmbiguitySourceConflicted})
		if variant.RetryOf == "" {
			if !variant.Resumed && variant.RetryReason == "" {
				continue
			}
			relations.Ambiguous = append(relations.Ambiguous, relation)
			continue
		}
		if _, targetConflicted := conflicted[variant.RetryOf]; targetConflicted {
			relation.Ambiguity = append(relation.Ambiguity, analysisAmbiguityTargetConflicted)
		}
		relations.Ambiguous = append(relations.Ambiguous, relation)
	}
}

func analysisAmbiguousRelationFrom(callID string, variant analysisTelemetryVariant, archivePath string, ambiguity []string) bundleAnalysisAmbiguousRelation {
	return bundleAnalysisAmbiguousRelation{
		CallID:      callID,
		RetryOf:     variant.RetryOf,
		RetryReason: variant.RetryReason,
		Phase:       variant.Phase,
		Outcome:     variant.Outcome,
		Resumed:     variant.Resumed,
		Ambiguity:   ambiguity,
		Source:      bundleAnalysisRecordTrace{ArchivePath: archivePath, Lines: variant.Lines},
	}
}

func analysisAppendModelCallRelation(relations *bundleAnalysisModelRelations, call analysisTelemetryCall, archivePath string, known, conflicted map[string]struct{}) {
	variant := call.Variants[0]
	if variant.RetryOf == "" {
		analysisAppendUnlinkedCall(relations, call, variant, archivePath)
		return
	}
	edge := bundleAnalysisRetryEdge{
		CallID:      call.CallID,
		RetryOf:     variant.RetryOf,
		RetryReason: variant.RetryReason,
		Phase:       variant.Phase,
		Outcome:     variant.Outcome,
		Resumed:     variant.Resumed,
		Source:      bundleAnalysisRecordTrace{ArchivePath: archivePath, Lines: variant.Lines},
	}
	if _, excluded := conflicted[variant.RetryOf]; excluded {
		relations.Ambiguous = append(relations.Ambiguous, analysisAmbiguousRelationFrom(call.CallID, variant, archivePath,
			[]string{analysisAmbiguityTargetConflicted}))
		return
	}
	if _, resolved := known[variant.RetryOf]; resolved {
		relations.Resolved = append(relations.Resolved, edge)
		return
	}
	relations.Dangling = append(relations.Dangling, edge)
}

func analysisAppendUnlinkedCall(relations *bundleAnalysisModelRelations, call analysisTelemetryCall, variant analysisTelemetryVariant, archivePath string) {
	if !variant.Resumed && variant.RetryReason == "" {
		return
	}
	relations.Unlinked = append(relations.Unlinked, bundleAnalysisUnlinkedCall{
		CallID:      call.CallID,
		Phase:       variant.Phase,
		Outcome:     variant.Outcome,
		Resumed:     variant.Resumed,
		RetryReason: variant.RetryReason,
		Source:      bundleAnalysisRecordTrace{ArchivePath: archivePath, Lines: variant.Lines},
	})
}
