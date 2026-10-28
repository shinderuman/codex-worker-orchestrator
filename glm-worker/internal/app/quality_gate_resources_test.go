package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

type fixtureQualityGateResourceSampler struct {
	document qualitygate.ResourceSamples
}

func (s *fixtureQualityGateResourceSampler) Stop() qualitygate.ResourceSamples {
	return s.document
}

func newDeterministicQualityGateResourceSampler(record qualitygate.RunRecord, _ func(qualitygate.ResourceSamples)) qualityGateResourceSampler {
	started := time.Now().UTC()
	host := qualitygate.ResourceSampleHost{
		GOOS: "fixture", GOARCH: "fixture", LogicalCPUs: 1, TotalMemoryBytes: 1 << 30,
		CPUDenominator: "cpu percentages are 0-100 where 100 means every logical cpu is busy",
		Units:          "memory and swap are bytes",
		CPUBasis:       "fixture source", MemoryDefinition: "fixture source",
		SwapDefinition: "fixture source", TimeBasis: "fixture source",
	}
	coverage := qualitygate.ResourceSampleCoverage{
		Status: qualitygate.ResourceCoverageCollected, Source: "fixture",
		IntervalMS: 5000, SampleBound: 720,
		StartedAt: started, EndedAt: started.Add(5 * time.Second), EndBasis: qualitygate.ResourceSampleEndBasis,
		CPUState: qualitygate.ResourceStateMeasured, MemoryState: qualitygate.ResourceStateMeasured,
		SwapState: qualitygate.ResourceStateMeasured,
	}
	sample := func(elapsed int64) qualitygate.HostResourceSample {
		return qualitygate.HostResourceSample{
			At: started.Add(time.Duration(elapsed) * time.Millisecond), ElapsedMS: elapsed,
			CPU:    &qualitygate.CPUSample{BusyPercent: 1.5, UserPercent: 0.75, SystemPercent: 0.75, IdlePercent: 98.5, WindowMS: 5000},
			Memory: &qualitygate.MemorySample{UsedBytes: 1 << 20, AvailableBytes: 2 << 20},
			Swap:   &qualitygate.SwapSample{TotalBytes: 1 << 20, UsedBytes: 1 << 10, FreeBytes: 1<<20 - 1<<10},
		}
	}
	return &fixtureQualityGateResourceSampler{document: qualitygate.ResourceSamples{
		SchemaVersion:   qualitygate.ResourceSamplesSchemaVersion,
		ValidationRunID: record.ValidationRunID,
		Form:            record.Form,
		Host:            host,
		Coverage:        coverage,
		Samples:         []qualitygate.HostResourceSample{sample(0), sample(5000)},
	}}
}

func TestQualityGateRunPersistsResourceSamples(t *testing.T) {
	useInlineQualityGateRunner(t)
	enterQualityGateGitWorkingDir(t)
	t.Setenv("PATH", writeTestTimingsGoShim(t, 0)+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, st := newQualityGateEnv(t)

	record := runQualityGateForRecord(t, "go-test", st)
	if record.ResourceSamples == "" {
		t.Fatalf("run record lacks the resource samples path: %#v", record)
	}
	data, err := os.ReadFile(record.ResourceSamples)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := qualitygate.DecodeResourceSamples(data)
	if err != nil {
		t.Fatalf("persisted resource samples do not decode: %v", err)
	}
	if resources.ValidationRunID != record.ValidationRunID || resources.Form != record.Form {
		t.Fatalf("resource samples identity = %#v", resources)
	}
	switch resources.Coverage.Status {
	case qualitygate.ResourceCoverageCollected, qualitygate.ResourceCoveragePartial, qualitygate.ResourceCoverageFailed:
	default:
		t.Fatalf("resource samples coverage status = %q", resources.Coverage.Status)
	}
	if len(resources.Samples) == 0 {
		t.Fatalf("resource samples captured no ticks: %#v", resources)
	}
	previous := int64(-1)
	for _, sample := range resources.Samples {
		if sample.ElapsedMS <= previous {
			t.Fatalf("resource samples elapsed is not increasing: %#v", resources.Samples)
		}
		previous = sample.ElapsedMS
	}
	if resources.Coverage.IntervalMS <= 0 || resources.Coverage.SampleBound <= 0 {
		t.Fatalf("resource samples bounds = %#v", resources.Coverage)
	}
	if resources.Host.GOOS == "" || resources.Host.GOARCH == "" {
		t.Fatalf("resource samples host context = %#v", resources.Host)
	}
}

func TestQualityGateResourceSamplesWriteFailureKeepsRunOutcome(t *testing.T) {
	_, st := newQualityGateEnv(t)
	runID := strings.Repeat("b", 32)
	if err := os.MkdirAll(filepath.Join(st.Path(qualitygate.RunDirectory), runID, qualitygate.ResourceSamplesFile), 0o700); err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID: runID,
		Form:            "go-test",
		Repository:      "/repo",
		StartedAt:       time.Now().Add(-time.Second).UTC(),
		Status:          qualitygate.StatusRunning,
	}
	resources := qualitygate.ResourceSamples{
		SchemaVersion:   qualitygate.ResourceSamplesSchemaVersion,
		ValidationRunID: runID,
		Form:            "go-test",
		Coverage: qualitygate.ResourceSampleCoverage{
			Status: qualitygate.ResourceCoverageFailed, IntervalMS: 5000, SampleBound: 720,
			StartedAt: time.Now().UTC(),
			CPUState:  qualitygate.ResourceStateMissing, MemoryState: qualitygate.ResourceStateMissing, SwapState: qualitygate.ResourceStateMissing,
		},
	}
	final, err := completeQualityGateRun(st, record, []byte("gate output\n"), nil, &resources)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != qualitygate.StatusPass || final.ExitCode != 0 {
		t.Fatalf("resource write failure changed the run outcome: %#v", final)
	}
	if final.ResourceSamples != "" {
		t.Fatalf("failed write reported a path: %q", final.ResourceSamples)
	}
	if final.ResourceSamplesError == "" {
		t.Fatal("resource write failure lost its diagnostic")
	}
}
