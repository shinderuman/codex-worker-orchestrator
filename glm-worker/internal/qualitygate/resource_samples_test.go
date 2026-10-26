package qualitygate

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func samplerTestSource(read func() hostResourceReading) *hostResourceSource {
	return &hostResourceSource{
		label: "test-source", supported: true,
		host: ResourceSampleHost{GOOS: "linux", GOARCH: "arm64", LogicalCPUs: 8, TotalMemoryBytes: 16 << 30},
		read: read,
	}
}

func fullHostResourceReading(ticks *cpuTicksReading) hostResourceReading {
	return hostResourceReading{
		cpuTicks: ticks,
		memory:   &MemorySample{UsedBytes: 1 << 30, AvailableBytes: 2 << 30},
		swap:     &SwapSample{TotalBytes: 4 << 30, UsedBytes: 1 << 20, FreeBytes: 4<<30 - 1<<20},
	}
}

func manualResourceSampler(t *testing.T, read func() hostResourceReading) *ResourceSampler {
	t.Helper()
	sampler := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("1", 32),
		Form:            "go-test",
		Interval:        time.Hour,
		SampleBound:     16,
		Source:          samplerTestSource(read),
	})
	t.Cleanup(func() { _ = sampler.Stop() })
	return sampler
}

func TestResourceSamplerCollectedDocumentRoundTrips(t *testing.T) {
	ticks := []*cpuTicksReading{
		{user: 100, system: 50, idle: 350, total: 500},
		{user: 300, system: 150, idle: 550, total: 1000},
	}
	index := 0
	sampler := manualResourceSampler(t, func() hostResourceReading {
		reading := fullHostResourceReading(ticks[index])
		index++
		return reading
	})
	time.Sleep(5 * time.Millisecond)
	if !sampler.collect() {
		t.Fatal("second collect was refused before the bound")
	}
	document := sampler.Stop()
	if document.Coverage.Status != ResourceCoverageCollected {
		t.Fatalf("coverage = %#v", document.Coverage)
	}
	if document.Coverage.CPUState != ResourceStateMeasured ||
		document.Coverage.MemoryState != ResourceStateMeasured ||
		document.Coverage.SwapState != ResourceStateMeasured {
		t.Fatalf("domain states = %#v", document.Coverage)
	}
	if document.Host.GOOS != "linux" || document.Host.LogicalCPUs != 8 || document.Host.TotalMemoryBytes != 16<<30 {
		t.Fatalf("host context = %#v", document.Host)
	}
	if len(document.Samples) != 2 {
		t.Fatalf("samples = %#v", document.Samples)
	}
	if document.Samples[0].CPU != nil {
		t.Fatalf("first cumulative sample fabricated cpu usage: %#v", document.Samples[0])
	}
	cpu := document.Samples[1].CPU
	if cpu == nil || cpu.WindowMS <= 0 || cpu.BusyPercent <= 0 || cpu.BusyPercent != cpu.UserPercent+cpu.SystemPercent {
		t.Fatalf("second sample cpu = %#v", cpu)
	}
	if cpu.UserPercent != 40 || cpu.SystemPercent != 20 || cpu.IdlePercent != 40 {
		t.Fatalf("cpu percentages = %#v", cpu)
	}
	data, err := EncodeResourceSamples(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeResourceSamples(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Coverage.Status != ResourceCoverageCollected || len(decoded.Samples) != 2 ||
		decoded.Samples[1].Memory.UsedBytes != 1<<30 || decoded.Samples[1].Swap.UsedBytes != 1<<20 {
		t.Fatalf("decoded document = %#v", decoded)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if wire["schema_version"].(float64) != ResourceSamplesSchemaVersion {
		t.Fatalf("wire schema = %v", wire["schema_version"])
	}
	if _, ok := wire["cpu_state"]; ok {
		t.Fatalf("coverage states leaked out of the coverage object: %v", wire)
	}
}

func TestResourceSamplerMarksPartialAndMissingDomains(t *testing.T) {
	cases := []struct {
		name      string
		readings  []hostResourceReading
		status    string
		cpuState  string
		memState  string
		swapState string
		problems  int
	}{
		{
			name: "all-domains-fail",
			readings: []hostResourceReading{
				{problems: []string{"cpu: denied", "memory: denied", "swap: denied"}},
				{problems: []string{"cpu: denied", "memory: denied", "swap: denied"}},
			},
			status: ResourceCoverageFailed, cpuState: ResourceStateMissing,
			memState: ResourceStateMissing, swapState: ResourceStateMissing, problems: 3,
		},
		{
			name: "swap-fails-once",
			readings: []hostResourceReading{
				{cpuTicks: &cpuTicksReading{user: 10, system: 5, idle: 35, total: 50}, memory: &MemorySample{}, swap: &SwapSample{}},
				{cpuTicks: &cpuTicksReading{user: 20, system: 10, idle: 70, total: 100}, memory: &MemorySample{}, problems: []string{"swap: sysctl vm.swapusage: operation not permitted"}},
			},
			status: ResourceCoveragePartial, cpuState: ResourceStateMeasured,
			memState: ResourceStateMeasured, swapState: ResourceStatePartial, problems: 1,
		},
		{
			name: "cpu-always-denied",
			readings: []hostResourceReading{
				{memory: &MemorySample{}, swap: &SwapSample{}, problems: []string{"cpu: top: operation not permitted"}},
				{memory: &MemorySample{}, swap: &SwapSample{}, problems: []string{"cpu: top: operation not permitted"}},
			},
			status: ResourceCoveragePartial, cpuState: ResourceStateMissing,
			memState: ResourceStateMeasured, swapState: ResourceStateMeasured, problems: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index := -1
			sampler := manualResourceSampler(t, func() hostResourceReading {
				index++
				return tc.readings[index]
			})
			time.Sleep(5 * time.Millisecond)
			sampler.collect()
			document := sampler.Stop()
			if document.Coverage.Status != tc.status ||
				document.Coverage.CPUState != tc.cpuState ||
				document.Coverage.MemoryState != tc.memState ||
				document.Coverage.SwapState != tc.swapState {
				t.Fatalf("coverage = %#v", document.Coverage)
			}
			if len(document.Coverage.Problems) != tc.problems {
				t.Fatalf("problems = %#v", document.Coverage.Problems)
			}
			if _, err := EncodeResourceSamples(document); err != nil {
				t.Fatalf("document does not encode: %v", err)
			}
		})
	}
}

func TestResourceSamplerBoundsSampleCount(t *testing.T) {
	sampler := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("2", 32),
		Form:            "go-test-race",
		Interval:        time.Hour,
		SampleBound:     2,
		Source: samplerTestSource(func() hostResourceReading {
			return fullHostResourceReading(&cpuTicksReading{user: 1, system: 1, idle: 2, total: 4})
		}),
	})
	for range 3 {
		sampler.collect()
	}
	document := sampler.Stop()
	if len(document.Samples) != 2 || !document.Coverage.Truncated {
		t.Fatalf("bounded document = %#v", document)
	}
	if document.Coverage.Status != ResourceCoveragePartial {
		t.Fatalf("truncation did not degrade coverage: %#v", document.Coverage)
	}
	if document.Coverage.SampleBound != 2 || document.Coverage.IntervalMS != time.Hour.Milliseconds() {
		t.Fatalf("coverage bounds = %#v", document.Coverage)
	}
	if document.ValidationRunID != strings.Repeat("2", 32) || document.Form != "go-test-race" {
		t.Fatalf("identity = %#v", document)
	}
}

func TestResourceSamplerStopIsIdempotent(t *testing.T) {
	sampler := manualResourceSampler(t, func() hostResourceReading { return fullHostResourceReading(nil) })
	first := sampler.Stop()
	second := sampler.Stop()
	if first.ValidationRunID != second.ValidationRunID || len(first.Samples) != len(second.Samples) {
		t.Fatalf("stop returned diverging documents: %#v vs %#v", first, second)
	}
}

func TestResourceSamplerRecordsPercentReadingWindow(t *testing.T) {
	reading := hostResourceReading{
		cpuPercent: &cpuPercentReading{busy: 41.54, user: 22.41, system: 19.13, idle: 58.46, windowMS: 2000},
		memory:     &MemorySample{UsedBytes: 1 << 30, AvailableBytes: 2 << 30},
		swap:       &SwapSample{TotalBytes: 4 << 30, FreeBytes: 4 << 30},
	}
	sampler := manualResourceSampler(t, func() hostResourceReading { return reading })
	document := sampler.Stop()
	if len(document.Samples) != 1 || document.Samples[0].CPU == nil {
		t.Fatalf("percent reading produced no cpu sample: %#v", document.Samples)
	}
	cpu := document.Samples[0].CPU
	if cpu.BusyPercent != 41.54 || cpu.WindowMS != 2000 {
		t.Fatalf("percent cpu sample = %#v", cpu)
	}
	if document.Coverage.CPUState != ResourceStateMeasured {
		t.Fatalf("cpu state = %s", document.Coverage.CPUState)
	}
}

func TestResourceSamplerRecordsCounterRegressionAsPartialAndRecovers(t *testing.T) {
	readings := []*cpuTicksReading{
		{user: 100, system: 50, idle: 350, total: 500},
		{user: 150, system: 40, idle: 300, total: 520},
		{user: 220, system: 90, idle: 500, total: 810},
	}
	index := -1
	sampler := manualResourceSampler(t, func() hostResourceReading {
		index++
		return hostResourceReading{cpuTicks: readings[index], memory: &MemorySample{}, swap: &SwapSample{}}
	})
	time.Sleep(5 * time.Millisecond)
	sampler.collect()
	time.Sleep(5 * time.Millisecond)
	sampler.collect()
	document := sampler.Stop()
	if len(document.Samples) != 3 {
		t.Fatalf("samples = %#v", document.Samples)
	}
	if document.Samples[0].CPU != nil || document.Samples[1].CPU != nil {
		t.Fatalf("first window or regressed interval fabricated cpu usage: %#v", document.Samples)
	}
	recovered := document.Samples[2].CPU
	if recovered == nil || recovered.BusyPercent != 31.03 || recovered.IdlePercent != 68.97 {
		t.Fatalf("recovered interval cpu = %#v", recovered)
	}
	if document.Coverage.CPUState != ResourceStatePartial || document.Coverage.ProblemTicks != 1 || len(document.Coverage.Problems) != 1 {
		t.Fatalf("coverage after regression = %#v", document.Coverage)
	}
	if document.Coverage.MemoryState != ResourceStateMeasured || document.Coverage.SwapState != ResourceStateMeasured {
		t.Fatalf("regressed cpu interval lost memory or swap: %#v", document.Coverage)
	}
	if document.Coverage.Status != ResourceCoveragePartial {
		t.Fatalf("coverage status = %s", document.Coverage.Status)
	}
	if _, err := EncodeResourceSamples(document); err != nil {
		t.Fatalf("regression document does not encode: %v", err)
	}
}

func TestResourceSamplerUnsupportedSourceProducesTypedDocument(t *testing.T) {
	source := &hostResourceSource{label: "unsupported", supported: false, reason: "no source", host: ResourceSampleHost{GOOS: "plan9"}}
	sampler := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("3", 32),
		Form:            "go-test",
		Source:          source,
	})
	document := sampler.Stop()
	if document.Coverage.Status != ResourceCoverageUnsupported ||
		document.Coverage.CPUState != ResourceStateUnsupported ||
		document.Coverage.MemoryState != ResourceStateUnsupported ||
		document.Coverage.SwapState != ResourceStateUnsupported {
		t.Fatalf("unsupported document = %#v", document.Coverage)
	}
	if len(document.Samples) != 0 || len(document.Coverage.Problems) != 1 || document.Coverage.Problems[0] != "no source" {
		t.Fatalf("unsupported disclosure = %#v", document)
	}
	if _, err := EncodeResourceSamples(document); err != nil {
		t.Fatalf("unsupported document does not encode: %v", err)
	}
}

func TestDecodeResourceSamplesRejectsInvalidDocuments(t *testing.T) {
	base := func(mutate func(*ResourceSamples)) []byte {
		document := ResourceSamples{
			SchemaVersion: ResourceSamplesSchemaVersion, ValidationRunID: strings.Repeat("4", 32), Form: "go-test",
			Coverage: ResourceSampleCoverage{
				Status: ResourceCoverageCollected, IntervalMS: 5000, SampleBound: 720, StartedAt: time.Unix(1000, 0).UTC(),
				CPUState: ResourceStateMeasured, MemoryState: ResourceStateMeasured, SwapState: ResourceStateMeasured,
			},
			Samples: []HostResourceSample{
				{At: time.Unix(1000, 0).UTC(), ElapsedMS: 0, CPU: &CPUSample{BusyPercent: 10, WindowMS: 5000}, Memory: &MemorySample{UsedBytes: 1}, Swap: &SwapSample{TotalBytes: 10, FreeBytes: 10}},
				{At: time.Unix(1005, 0).UTC(), ElapsedMS: 5000, CPU: &CPUSample{BusyPercent: 20, WindowMS: 5000}, Memory: &MemorySample{UsedBytes: 2}, Swap: &SwapSample{TotalBytes: 10, FreeBytes: 10}},
			},
		}
		mutate(&document)
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cases := []struct {
		name   string
		mutate func(*ResourceSamples)
	}{
		{name: "schema-version", mutate: func(d *ResourceSamples) { d.SchemaVersion = 2 }},
		{name: "run-id", mutate: func(d *ResourceSamples) { d.ValidationRunID = "short" }},
		{name: "form", mutate: func(d *ResourceSamples) { d.Form = "go-vet" }},
		{name: "coverage-status", mutate: func(d *ResourceSamples) { d.Coverage.Status = "unknown" }},
		{name: "domain-state", mutate: func(d *ResourceSamples) { d.Coverage.SwapState = "zero" }},
		{name: "interval", mutate: func(d *ResourceSamples) { d.Coverage.IntervalMS = 0 }},
		{name: "bound", mutate: func(d *ResourceSamples) { d.Coverage.SampleBound = 0 }},
		{name: "missing-start", mutate: func(d *ResourceSamples) { d.Coverage.StartedAt = time.Time{} }},
		{name: "rows-over-bound", mutate: func(d *ResourceSamples) { d.Coverage.SampleBound = 1 }},
		{name: "truncation-mismatch", mutate: func(d *ResourceSamples) { d.Coverage.Truncated = true; d.Coverage.SampleBound = 720 }},
		{name: "collected-with-partial-state", mutate: func(d *ResourceSamples) { d.Coverage.MemoryState = ResourceStateMissing }},
		{name: "collected-with-problems", mutate: func(d *ResourceSamples) { d.Coverage.ProblemTicks = 1; d.Coverage.Problems = []string{"swap: denied"} }},
		{name: "elapsed-not-increasing", mutate: func(d *ResourceSamples) { d.Samples[1].ElapsedMS = 0 }},
		{name: "cpu-percent-range", mutate: func(d *ResourceSamples) { d.Samples[1].CPU.BusyPercent = 120 }},
		{name: "cpu-window-negative", mutate: func(d *ResourceSamples) { d.Samples[1].CPU.WindowMS = -1 }},
		{name: "swap-used-over-total", mutate: func(d *ResourceSamples) { d.Samples[1].Swap.UsedBytes = 11 }},
		{name: "swap-free-over-total", mutate: func(d *ResourceSamples) { d.Samples[1].Swap.FreeBytes = 11 }},
		{name: "unsupported-with-rows", mutate: func(d *ResourceSamples) {
			d.Coverage.Status = ResourceCoverageUnsupported
			d.Coverage.CPUState = ResourceStateUnsupported
			d.Coverage.MemoryState = ResourceStateUnsupported
			d.Coverage.SwapState = ResourceStateUnsupported
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeResourceSamples(base(tc.mutate)); err == nil {
				t.Fatal("invalid document decoded")
			}
		})
	}
}

func TestDecodeResourceSamplesAcceptsZeroSwapAsMeasured(t *testing.T) {
	document := ResourceSamples{
		SchemaVersion: ResourceSamplesSchemaVersion, ValidationRunID: strings.Repeat("5", 32), Form: "go-test",
		Coverage: ResourceSampleCoverage{
			Status: ResourceCoveragePartial, IntervalMS: 5000, SampleBound: 720, StartedAt: time.Unix(1000, 0).UTC(),
			CPUState: ResourceStateMissing, MemoryState: ResourceStateMeasured, SwapState: ResourceStateMeasured,
		},
		Samples: []HostResourceSample{
			{At: time.Unix(1000, 0).UTC(), Memory: &MemorySample{UsedBytes: 0, AvailableBytes: 8 << 30}, Swap: &SwapSample{}},
		},
	}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeResourceSamples(data)
	if err != nil {
		t.Fatalf("zero swap document rejected: %v", err)
	}
	if decoded.Samples[0].Swap == nil || decoded.Samples[0].Swap.TotalBytes != 0 {
		t.Fatalf("measured zero swap was dropped: %#v", decoded.Samples[0])
	}
}
