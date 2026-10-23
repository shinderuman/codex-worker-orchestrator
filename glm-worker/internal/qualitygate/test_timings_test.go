package qualitygate

import (
	"reflect"
	"strings"
	"testing"
)

func TestDistillTestTimingsCollectsTerminalEventsOnly(t *testing.T) {
	t.Parallel()
	gateLog := strings.Join([]string{
		`{"Time":"2026-10-22T00:00:00Z","Action":"start","Package":"example.com/m"}`,
		`{"Time":"2026-10-22T00:00:00Z","Action":"run","Package":"example.com/m","Test":"TestFast"}`,
		`{"Time":"2026-10-22T00:00:00Z","Action":"run","Package":"example.com/m","Test":"TestVanished"}`,
		`{"Time":"2026-10-22T00:00:01Z","Action":"output","Package":"example.com/m","Test":"TestFast","Output":"ok\n"}`,
		`{"Time":"2026-10-22T00:00:01Z","Action":"pass","Package":"example.com/m","Test":"TestFast","Elapsed":0.123}`,
		`{"Time":"2026-10-22T00:00:02Z","Action":"pass","Package":"example.com/m","Test":"TestSlow/sub_case","Elapsed":1.5}`,
		`{"Time":"2026-10-22T00:00:03Z","Action":"fail","Package":"example.com/m","Test":"TestBroken","Elapsed":0.05}`,
		`{"Time":"2026-10-22T00:00:03Z","Action":"skip","Package":"example.com/m","Test":"TestSkipped","Elapsed":0}`,
		`{"Time":"2026-10-22T00:00:04Z","Action":"pass","Package":"example.com/m","Elapsed":2}`,
		`go shim: ok`,
		``,
	}, "\n")
	timings := DistillTestTimings("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "go-test", []byte(gateLog))
	expected := []TestTiming{
		{Package: "example.com/m", Action: TestTimingActionPass, ElapsedMS: 2000},
		{Package: "example.com/m", Test: "TestBroken", Action: TestTimingActionFail, ElapsedMS: 50},
		{Package: "example.com/m", Test: "TestFast", Action: TestTimingActionPass, ElapsedMS: 123},
		{Package: "example.com/m", Test: "TestSkipped", Action: TestTimingActionSkip, ElapsedMS: 0},
		{Package: "example.com/m", Test: "TestSlow/sub_case", Action: TestTimingActionPass, ElapsedMS: 1500},
	}
	if !reflect.DeepEqual(timings.Tests, expected) {
		t.Fatalf("distilled timings = %#v", timings.Tests)
	}
	if timings.CountTests() != 4 || timings.SlowestTestMS() != 1500 {
		t.Fatalf("test counts = %d slowest = %d", timings.CountTests(), timings.SlowestTestMS())
	}
	if timings.Coverage != (TestTimingCoverage{Source: TestTimingSourceGoTestJSON, Captured: true, TerminalEvents: 5}) {
		t.Fatalf("distilled coverage = %#v", timings.Coverage)
	}
}

func TestDistillTestTimingsKeepsLastTerminalAndRejectsNegativeElapsed(t *testing.T) {
	t.Parallel()
	gateLog := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/m","Test":"TestRetried","Elapsed":0.1}`,
		`{"Action":"pass","Package":"example.com/m","Test":"TestRetried","Elapsed":0.3}`,
		`{"Action":"pass","Package":"example.com/m","Test":"TestNegative","Elapsed":-1}`,
	}, "\n")
	timings := DistillTestTimings("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "go-test-race", []byte(gateLog))
	if len(timings.Tests) != 1 || timings.Tests[0].ElapsedMS != 300 {
		t.Fatalf("distilled timings = %#v", timings.Tests)
	}
	if timings.Coverage.TerminalEvents != 3 || timings.Coverage.UntimedTerminalEvents != 1 {
		t.Fatalf("distilled coverage = %#v", timings.Coverage)
	}
}

func TestDistillTestTimingsMissingElapsedIsUntimedNotZero(t *testing.T) {
	t.Parallel()
	gateLog := strings.Join([]string{
		`{"Action":"pass","Package":"example.com/m","Test":"TestObservedZero","Elapsed":0}`,
		`{"Action":"pass","Package":"example.com/m","Test":"TestNoElapsed"}`,
	}, "\n")
	timings := DistillTestTimings("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "go-test", []byte(gateLog))
	if len(timings.Tests) != 1 || timings.Tests[0].Test != "TestObservedZero" || timings.Tests[0].ElapsedMS != 0 {
		t.Fatalf("distilled timings = %#v", timings.Tests)
	}
	if timings.Coverage.UntimedTerminalEvents != 1 {
		t.Fatalf("untimed terminal events = %d", timings.Coverage.UntimedTerminalEvents)
	}
}

func TestDistillTestTimingsWithoutEventsStaysEmpty(t *testing.T) {
	t.Parallel()
	timings := DistillTestTimings("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "go-test", []byte("plain text log\nwith no events\n"))
	if len(timings.Tests) != 0 {
		t.Fatalf("plain text log distilled entries = %#v", timings.Tests)
	}
	if timings.CountTests() != 0 || timings.SlowestTestMS() != 0 {
		t.Fatalf("plain text log distilled counts = %d/%d", timings.CountTests(), timings.SlowestTestMS())
	}
	if timings.Coverage.Captured || timings.Coverage.Source != TestTimingSourcePlainLog || timings.Coverage.TerminalEvents != 0 {
		t.Fatalf("plain text log coverage = %#v", timings.Coverage)
	}
}

func TestEncodeDecodeTestTimingsRoundTrip(t *testing.T) {
	t.Parallel()
	timings := TestTimings{
		SchemaVersion:   TestTimingsSchemaVersion,
		ValidationRunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Form:            "go-test",
		Coverage:        TestTimingCoverage{Source: TestTimingSourceGoTestJSON, Captured: true, TerminalEvents: 1},
		Tests:           []TestTiming{{Package: "example.com/m", Test: "TestFast", Action: TestTimingActionPass, ElapsedMS: 123}},
	}
	data, err := EncodeTestTimings(timings)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeTestTimings(data)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, timings) {
		t.Fatalf("round trip = %#v", decoded)
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":1,"form":"go-test"}`)); err == nil {
		t.Fatal("unsupported schema version was accepted")
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":2,"validation_run_id":"short","form":"go-test"}`)); err == nil {
		t.Fatal("invalid run identity was accepted")
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":2,"form":"go-test"}`)); err == nil {
		t.Fatal("missing form was accepted")
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":2,"form":"go-test","coverage":{"source":"go-test-json"}}`)); err == nil {
		t.Fatal("captured flag disagreeing with source was accepted")
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":2,"form":"go-test","coverage":{"source":"plain-log"},"tests":[{"package":"example.com/m","test":"TestDup","action":"pass","elapsed_ms":1},{"package":"example.com/m","test":"TestDup","action":"pass","elapsed_ms":2}]}`)); err == nil {
		t.Fatal("duplicate timing row was accepted")
	}
	if _, err := DecodeTestTimings([]byte(`{"schema_version":2,"form":"go-test","coverage":{"source":"plain-log"},"tests":[{"package":"example.com/m","test":"TestNeg","action":"pass","elapsed_ms":-1}]}`)); err == nil {
		t.Fatal("negative elapsed row was accepted")
	}
}
