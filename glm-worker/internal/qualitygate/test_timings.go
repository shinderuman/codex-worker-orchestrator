package qualitygate

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

type TestTiming struct {
	Package   string `json:"package"`
	Test      string `json:"test,omitempty"`
	Action    string `json:"action"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type TestTimingCoverage struct {
	Source                string `json:"source"`
	Captured              bool   `json:"captured"`
	TerminalEvents        int    `json:"terminal_events"`
	UntimedTerminalEvents int    `json:"untimed_terminal_events"`
}

type TestTimings struct {
	SchemaVersion   int                `json:"schema_version"`
	ValidationRunID string             `json:"validation_run_id"`
	Form            string             `json:"form"`
	Coverage        TestTimingCoverage `json:"coverage"`
	Tests           []TestTiming       `json:"tests,omitempty"`
}

type goTestJSONEvent struct {
	Action  string   `json:"Action"`
	Package string   `json:"Package"`
	Test    string   `json:"Test"`
	Elapsed *float64 `json:"Elapsed"`
	Output  string   `json:"Output"`
}

type testTimingKey struct {
	pkg  string
	test string
}

const (
	TestTimingsFile = "test-timings.json"

	TestTimingsSchemaVersion = 2

	TestTimingActionPass = "pass"
	TestTimingActionFail = "fail"
	TestTimingActionSkip = "skip"

	TestTimingSourceGoTestJSON = "go-test-json"
	TestTimingSourcePlainLog   = "plain-log"
)

func DistillTestTimings(validationRunID, form string, gateLog []byte) TestTimings {
	timings := TestTimings{
		SchemaVersion:   TestTimingsSchemaVersion,
		ValidationRunID: validationRunID,
		Form:            form,
		Coverage:        TestTimingCoverage{Source: TestTimingSourcePlainLog},
	}
	collected := map[testTimingKey]TestTiming{}
	for _, line := range strings.Split(string(gateLog), "\n") {
		timing, coverage, ok := distillTestTimingLine(line, &timings.Coverage)
		if coverage {
			delete(collected, testTimingKey{pkg: timing.Package, test: timing.Test})
			continue
		}
		if !ok {
			continue
		}
		collected[testTimingKey{pkg: timing.Package, test: timing.Test}] = timing
	}
	if timings.Coverage.TerminalEvents > 0 {
		timings.Coverage.Source = TestTimingSourceGoTestJSON
		timings.Coverage.Captured = true
	}
	timings.Tests = sortedTestTimings(collected)
	return timings
}

func distillTestTimingLine(line string, coverage *TestTimingCoverage) (TestTiming, bool, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return TestTiming{}, false, false
	}
	var event goTestJSONEvent
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		return TestTiming{}, false, false
	}
	if event.Action == "" || event.Package == "" {
		return TestTiming{}, false, false
	}
	if !testTimingTerminalAction(event.Action) {
		return TestTiming{}, false, false
	}
	coverage.TerminalEvents++
	if event.Elapsed == nil || *event.Elapsed < 0 || *event.Elapsed >= float64(math.MaxInt64)/1000 {
		coverage.UntimedTerminalEvents++
		return TestTiming{Package: event.Package, Test: event.Test}, true, false
	}
	return TestTiming{
		Package:   event.Package,
		Test:      event.Test,
		Action:    event.Action,
		ElapsedMS: int64(*event.Elapsed * 1000),
	}, false, true
}

func sortedTestTimings(collected map[testTimingKey]TestTiming) []TestTiming {
	keys := make([]testTimingKey, 0, len(collected))
	for key := range collected {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].test < keys[j].test
	})
	tests := make([]TestTiming, 0, len(keys))
	for _, key := range keys {
		tests = append(tests, collected[key])
	}
	return tests
}

func testTimingTerminalAction(action string) bool {
	switch action {
	case TestTimingActionPass, TestTimingActionFail, TestTimingActionSkip:
		return true
	default:
		return false
	}
}

func EncodeTestTimings(timings TestTimings) ([]byte, error) {
	if err := validateTestTimings(timings); err != nil {
		return nil, err
	}
	return json.Marshal(timings)
}

func DecodeTestTimings(data []byte) (TestTimings, error) {
	var timings TestTimings
	if err := json.Unmarshal(data, &timings); err != nil {
		return TestTimings{}, err
	}
	var wire struct {
		Tests []struct {
			ElapsedMS *int64 `json:"elapsed_ms"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return TestTimings{}, err
	}
	for _, row := range wire.Tests {
		if row.ElapsedMS == nil {
			return TestTimings{}, fmt.Errorf("test timing elapsed is missing")
		}
	}
	if err := validateTestTimings(timings); err != nil {
		return TestTimings{}, err
	}
	return timings, nil
}

func validateTestTimings(timings TestTimings) error {
	if timings.SchemaVersion != TestTimingsSchemaVersion {
		return fmt.Errorf("test timings schema version %d is unsupported", timings.SchemaVersion)
	}
	if !ValidRunID(timings.ValidationRunID) {
		return fmt.Errorf("test timings run identity is invalid")
	}
	if timings.Form != formGoTest && timings.Form != formGoTestRace {
		return fmt.Errorf("test timings form is missing")
	}
	if err := validateTestTimingCoverage(timings.Coverage, len(timings.Tests)); err != nil {
		return err
	}
	seen := map[testTimingKey]bool{}
	for _, timing := range timings.Tests {
		if err := validateTestTimingRow(timing); err != nil {
			return err
		}
		key := testTimingKey{pkg: timing.Package, test: timing.Test}
		if seen[key] {
			return fmt.Errorf("test timings repeat row %s in %s", timing.Test, timing.Package)
		}
		seen[key] = true
	}
	return nil
}

func (t TestTiming) IsPackageTiming() bool {
	return t.Test == ""
}

func (t TestTimings) CountTests() int {
	count := 0
	for _, timing := range t.Tests {
		if !timing.IsPackageTiming() {
			count++
		}
	}
	return count
}

func (t TestTimings) SlowestTestMS() int64 {
	slowest := int64(0)
	for _, timing := range t.Tests {
		if timing.IsPackageTiming() {
			continue
		}
		if timing.ElapsedMS > slowest {
			slowest = timing.ElapsedMS
		}
	}
	return slowest
}

func validateTestTimingCoverage(coverage TestTimingCoverage, rows int) error {
	switch coverage.Source {
	case TestTimingSourceGoTestJSON, TestTimingSourcePlainLog:
	default:
		return fmt.Errorf("test timings coverage source %q is unsupported", coverage.Source)
	}
	if coverage.Captured != (coverage.Source == TestTimingSourceGoTestJSON) {
		return fmt.Errorf("test timings coverage capture flag disagrees with its source")
	}
	if coverage.TerminalEvents < 0 || coverage.UntimedTerminalEvents < 0 ||
		coverage.UntimedTerminalEvents > coverage.TerminalEvents {
		return fmt.Errorf("test timings coverage counters are invalid")
	}
	if rows > coverage.TerminalEvents-coverage.UntimedTerminalEvents || (!coverage.Captured && rows > 0) || (coverage.Captured && coverage.TerminalEvents == 0) {
		return fmt.Errorf("test timing rows disagree with coverage")
	}

	return nil
}

func validateTestTimingRow(timing TestTiming) error {
	switch {
	case strings.TrimSpace(timing.Package) == "":
		return fmt.Errorf("test timings row is missing its package")
	case !testTimingTerminalAction(timing.Action):
		return fmt.Errorf("test timings row action %q is unsupported", timing.Action)
	case timing.ElapsedMS < 0:
		return fmt.Errorf("test timings row %s has a negative elapsed", timing.Test)
	}
	return nil
}
