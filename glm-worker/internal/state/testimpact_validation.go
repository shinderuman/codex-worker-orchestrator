package state

import (
	"fmt"
	"slices"
	"strings"
)

type TestImpactValidationMeasure struct {
	GateClass      string   `json:"gate_class"`
	Suite          string   `json:"suite"`
	Runs           int      `json:"runs"`
	Initial        int      `json:"initial"`
	Retries        int      `json:"retries,omitempty"`
	Pass           int      `json:"pass,omitempty"`
	Fail           int      `json:"fail,omitempty"`
	Unknown        int      `json:"unknown,omitempty"`
	Measured       int      `json:"measured,omitempty"`
	MeasuredSumMS  int64    `json:"measured_sum_ms,omitempty"`
	MeasuredMaxMS  int64    `json:"measured_max_ms,omitempty"`
	SnapshotIDs    []string `json:"snapshot_ids,omitempty"`
	Phases         []string `json:"phases,omitempty"`
	SourceLocators []string `json:"source_locators,omitempty"`
}

type testImpactValidationObservation struct {
	Validation TaskValidationObservation
	DurationMS int64
	Locator    string
}

const testImpactValidationLocatorLimit = 8

const (
	testImpactBlockToolUse    = "tool_use"
	testImpactBlockToolResult = "tool_result"
)

func testImpactValidationMeasures(task TaskEvents) []TestImpactValidationMeasure {
	observations := testImpactValidationObservations(task)
	measures := make(map[string]*TestImpactValidationMeasure)
	attempts := make(map[string]int)
	for _, observation := range observations {
		absorbTestImpactValidationObservation(measures, attempts, observation)
	}
	return sortedTestImpactValidationMeasures(measures)
}

func absorbTestImpactValidationObservation(
	measures map[string]*TestImpactValidationMeasure,
	attempts map[string]int,
	observation testImpactValidationObservation,
) {
	value, gateClass, suite := normalizeTestImpactValidation(observation.Validation)
	key := gateClass + "\x00" + suite
	measure := measures[key]
	if measure == nil {
		measure = &TestImpactValidationMeasure{GateClass: gateClass, Suite: suite}
		measures[key] = measure
	}
	measure.Runs++
	absorbTestImpactValidationAttempt(measure, attempts, key, value)
	absorbTestImpactValidationResult(measure, value.Result)
	absorbTestImpactValidationDuration(measure, observation.DurationMS)
	measure.SnapshotIDs = appendBoundedUnique(measure.SnapshotIDs, value.SnapshotID, testImpactValidationLocatorLimit)
	measure.Phases = appendBoundedUnique(measure.Phases, value.Phase, testImpactValidationLocatorLimit)
	measure.SourceLocators = appendBoundedUnique(measure.SourceLocators, observation.Locator, testImpactValidationLocatorLimit)
}

func normalizeTestImpactValidation(value TaskValidationObservation) (TaskValidationObservation, string, string) {
	suite := value.Suite
	if suite == "" {
		suite = value.Form
	}
	if suite == "" {
		suite = ValidationGateClassUnknown
	}
	gateClass := value.GateClass
	if gateClass == "" {
		gateClass = ValidationGateClass(suite)
	}
	if gateClass == "" {
		gateClass = ValidationGateClassUnknown
	}
	value.Suite = suite
	value.GateClass = gateClass
	return value, gateClass, suite
}

func absorbTestImpactValidationAttempt(
	measure *TestImpactValidationMeasure,
	attempts map[string]int,
	key string,
	value TaskValidationObservation,
) {
	attemptKey := key + "\x00" + value.SnapshotID
	attempt := value.Attempt
	if attempt == "" {
		if attempts[attemptKey] == 0 {
			attempt = ValidationAttemptInitial
		} else {
			attempt = ValidationAttemptRetry
		}
	}
	attempts[attemptKey]++
	if attempt == ValidationAttemptRetry {
		measure.Retries++
		return
	}
	measure.Initial++
}

func absorbTestImpactValidationResult(measure *TestImpactValidationMeasure, result string) {
	switch result {
	case ValidationResultPass:
		measure.Pass++
	case ValidationResultFail:
		measure.Fail++
	default:
		measure.Unknown++
	}
}

func absorbTestImpactValidationDuration(measure *TestImpactValidationMeasure, durationMS int64) {
	if durationMS <= 0 {
		return
	}
	measure.Measured++
	measure.MeasuredSumMS += durationMS
	if durationMS > measure.MeasuredMaxMS {
		measure.MeasuredMaxMS = durationMS
	}
}

func testImpactValidationObservations(task TaskEvents) []testImpactValidationObservation {
	resultIDs := testImpactValidationResultIDs(task)
	observations := make([]testImpactValidationObservation, 0)
	for _, record := range task.Records {
		observations = appendTestImpactEventValidation(observations, task.TaskID, record)
		observations = appendTestImpactBlockValidations(observations, task.TaskID, record, resultIDs)
	}
	return observations
}

func testImpactValidationResultIDs(task TaskEvents) map[string]struct{} {
	resultIDs := make(map[string]struct{})
	for _, record := range task.Records {
		for _, block := range record.Blocks {
			if block.Type == testImpactBlockToolResult && block.ToolID != "" && len(block.Validation) > 0 {
				resultIDs[record.CallID+"\x00"+block.ToolID] = struct{}{}
			}
		}
	}
	return resultIDs
}

func appendTestImpactEventValidation(
	observations []testImpactValidationObservation,
	taskID string,
	record TaskEventRecord,
) []testImpactValidationObservation {
	if record.Validation == nil {
		return observations
	}
	return append(observations, testImpactValidationObservation{
		Validation: validationObservationFromEvent(*record.Validation, record.Phase),
		DurationMS: record.Validation.DurationMS,
		Locator:    fmt.Sprintf("events/%s.jsonl:seq=%d:validation", taskID, record.Seq),
	})
}

func appendTestImpactBlockValidations(
	observations []testImpactValidationObservation,
	taskID string,
	record TaskEventRecord,
	resultIDs map[string]struct{},
) []testImpactValidationObservation {
	for blockIndex, block := range record.Blocks {
		if !testImpactValidationBlockEligible(record.CallID, block, resultIDs) {
			continue
		}
		for validationIndex, validation := range block.Validation {
			if validation.Phase == "" {
				validation.Phase = record.Phase
			}
			observations = append(observations, testImpactValidationObservation{
				Validation: validation,
				DurationMS: block.DurationMS,
				Locator: fmt.Sprintf(
					"events/%s.jsonl:seq=%d:block=%d:validation=%d",
					taskID, record.Seq, blockIndex, validationIndex,
				),
			})
		}
	}
	return observations
}

func testImpactValidationBlockEligible(callID string, block TaskBlockSummary, resultIDs map[string]struct{}) bool {
	if len(block.Validation) == 0 || block.Type != testImpactBlockToolResult && block.Type != testImpactBlockToolUse {
		return false
	}
	if block.Type != testImpactBlockToolUse {
		return true
	}
	_, hasResult := resultIDs[callID+"\x00"+block.ToolID]
	return !hasResult
}

func validationObservationFromEvent(event TaskValidationEvent, fallbackPhase string) TaskValidationObservation {
	phase := event.Phase
	if phase == "" {
		phase = fallbackPhase
	}
	return TaskValidationObservation{
		Form:       event.Form,
		GateClass:  event.GateClass,
		Suite:      event.Suite,
		SnapshotID: event.SnapshotID,
		Phase:      phase,
		Attempt:    event.Attempt,
		Result:     event.Result,
	}
}

func absorbTestImpactValidationMeasure(totals map[string]*TestImpactValidationMeasure, measure TestImpactValidationMeasure) {
	key := measure.GateClass + "\x00" + measure.Suite
	total := totals[key]
	if total == nil {
		total = &TestImpactValidationMeasure{GateClass: measure.GateClass, Suite: measure.Suite}
		totals[key] = total
	}
	total.Runs += measure.Runs
	total.Initial += measure.Initial
	total.Retries += measure.Retries
	total.Pass += measure.Pass
	total.Fail += measure.Fail
	total.Unknown += measure.Unknown
	total.Measured += measure.Measured
	total.MeasuredSumMS += measure.MeasuredSumMS
	if measure.MeasuredMaxMS > total.MeasuredMaxMS {
		total.MeasuredMaxMS = measure.MeasuredMaxMS
	}
	for _, value := range measure.SnapshotIDs {
		total.SnapshotIDs = appendBoundedUnique(total.SnapshotIDs, value, testImpactValidationLocatorLimit)
	}
	for _, value := range measure.Phases {
		total.Phases = appendBoundedUnique(total.Phases, value, testImpactValidationLocatorLimit)
	}
	for _, value := range measure.SourceLocators {
		total.SourceLocators = appendBoundedUnique(total.SourceLocators, value, testImpactValidationLocatorLimit)
	}
}

func sortedTestImpactValidationMeasures(totals map[string]*TestImpactValidationMeasure) []TestImpactValidationMeasure {
	values := make([]TestImpactValidationMeasure, 0, len(totals))
	for _, value := range totals {
		values = append(values, *value)
	}
	slices.SortFunc(values, func(a, b TestImpactValidationMeasure) int {
		if byClass := strings.Compare(a.GateClass, b.GateClass); byClass != 0 {
			return byClass
		}
		return strings.Compare(a.Suite, b.Suite)
	})
	return values
}

func appendBoundedUnique(values []string, value string, limit int) []string {
	if value == "" || slices.Contains(values, value) || len(values) >= limit {
		return values
	}
	return append(values, value)
}
