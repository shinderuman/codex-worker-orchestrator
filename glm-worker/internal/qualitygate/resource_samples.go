package qualitygate

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type CPUSample struct {
	BusyPercent   float64 `json:"busy_percent"`
	UserPercent   float64 `json:"user_percent"`
	SystemPercent float64 `json:"system_percent"`
	IdlePercent   float64 `json:"idle_percent"`
	WindowMS      int64   `json:"window_ms,omitempty"`
}

type MemorySample struct {
	UsedBytes      uint64            `json:"used_bytes"`
	AvailableBytes uint64            `json:"available_bytes"`
	RawPages       map[string]uint64 `json:"raw_pages,omitempty"`
}

type SwapSample struct {
	TotalBytes uint64 `json:"total_bytes"`
	UsedBytes  uint64 `json:"used_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type HostResourceSample struct {
	At        time.Time     `json:"at"`
	ElapsedMS int64         `json:"elapsed_ms"`
	CPU       *CPUSample    `json:"cpu,omitempty"`
	Memory    *MemorySample `json:"memory,omitempty"`
	Swap      *SwapSample   `json:"swap,omitempty"`
}

type ResourceSampleHost struct {
	GOOS             string `json:"goos"`
	GOARCH           string `json:"goarch"`
	LogicalCPUs      int    `json:"logical_cpus,omitempty"`
	CPUDenominator   string `json:"cpu_denominator,omitempty"`
	TotalMemoryBytes uint64 `json:"total_memory_bytes,omitempty"`
	CPUBasis         string `json:"cpu_basis,omitempty"`
	MemoryDefinition string `json:"memory_definition,omitempty"`
	SwapDefinition   string `json:"swap_definition,omitempty"`
	Units            string `json:"units,omitempty"`
	TimeBasis        string `json:"time_basis,omitempty"`
}

type ResourceSampleCoverage struct {
	Status       string    `json:"status"`
	Source       string    `json:"source,omitempty"`
	IntervalMS   int64     `json:"interval_ms"`
	SampleBound  int       `json:"sample_bound"`
	StartedAt    time.Time `json:"started_at"`
	EndedAt      time.Time `json:"ended_at,omitempty"`
	EndBasis     string    `json:"end_basis,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
	CPUState     string    `json:"cpu_state"`
	MemoryState  string    `json:"memory_state"`
	SwapState    string    `json:"swap_state"`
	ProblemTicks int       `json:"problem_ticks,omitempty"`
	Problems     []string  `json:"problems,omitempty"`
}

type ResourceSamples struct {
	SchemaVersion   int                    `json:"schema_version"`
	ValidationRunID string                 `json:"validation_run_id"`
	Form            string                 `json:"form"`
	Host            ResourceSampleHost     `json:"host"`
	Coverage        ResourceSampleCoverage `json:"coverage"`
	Samples         []HostResourceSample   `json:"samples,omitempty"`
}

type cpuPercentReading struct {
	busy     float64
	user     float64
	system   float64
	idle     float64
	windowMS int64
}

type cpuTicksReading struct {
	user   uint64
	system uint64
	idle   uint64
	total  uint64
}

type hostResourceReading struct {
	cpuPercent *cpuPercentReading
	cpuTicks   *cpuTicksReading
	memory     *MemorySample
	swap       *SwapSample
	problems   []string
}

type hostResourceSource struct {
	label     string
	supported bool
	reason    string
	host      ResourceSampleHost
	read      func() hostResourceReading
}

type ResourceSamplerConfig struct {
	ValidationRunID string
	Form            string
	Interval        time.Duration
	SampleBound     int
	Source          *hostResourceSource
}

type resourceDomainCounters struct {
	measured int
	problems int
}

type ResourceSampler struct {
	mu           sync.Mutex
	runID        string
	form         string
	interval     time.Duration
	bound        int
	source       *hostResourceSource
	started      time.Time
	samples      []HostResourceSample
	prevTicks    *cpuTicksReading
	prevAt       time.Time
	cpu          resourceDomainCounters
	memory       resourceDomainCounters
	swap         resourceDomainCounters
	problemTicks int
	problems     map[string]int
	ordered      []string
	stopped      bool
	document     ResourceSamples
	done         chan struct{}
}

const (
	ResourceSamplesFile          = "resource-samples.json"
	ResourceSamplesSchemaVersion = 1

	ResourceSampleDefaultInterval = 5 * time.Second
	ResourceSampleDefaultBound    = 720
	ResourceSampleProblemsBound   = 8

	ResourceCoverageCollected   = "collected"
	ResourceCoveragePartial     = "partial"
	ResourceCoverageFailed      = "failed"
	ResourceCoverageUnsupported = "unsupported"

	ResourceStateMeasured    = "measured"
	ResourceStatePartial     = "partial"
	ResourceStateMissing     = "missing"
	ResourceStateUnsupported = "unsupported"

	ResourceSampleEndBasis = "sampler-stopped-at-run-completion"
)

func StartResourceSampler(config ResourceSamplerConfig) *ResourceSampler {
	source := config.Source
	if source == nil {
		platform := platformHostResourceSource()
		source = &platform
	}
	interval := config.Interval
	if interval <= 0 {
		interval = ResourceSampleDefaultInterval
	}
	bound := config.SampleBound
	if bound <= 0 {
		bound = ResourceSampleDefaultBound
	}
	source.host.TimeBasis = "sample.at stamps are host wall-clock UTC; elapsed_ms counts from coverage.started_at; the quality gate run window lives in run.json beside this file"
	sampler := &ResourceSampler{
		runID: config.ValidationRunID, form: config.Form,
		interval: interval, bound: bound, source: source,
		problems: map[string]int{}, done: make(chan struct{}),
	}
	if !source.supported {
		sampler.stopped = true
		sampler.document = sampler.unsupportedDocument(source.reason)
		return sampler
	}
	sampler.started = time.Now().UTC()
	sampler.collect()
	go sampler.loop()
	return sampler
}

func (s *ResourceSampler) loop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if !s.collect() {
				return
			}
		}
	}
}

func (s *ResourceSampler) collect() bool {
	s.mu.Lock()
	if s.stopped || len(s.samples) >= s.bound {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()
	at := time.Now().UTC()
	reading := s.source.read()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	sample := HostResourceSample{At: at, ElapsedMS: s.nextElapsed(at)}
	s.recordReading(&sample, reading, at)
	s.samples = append(s.samples, sample)
	return len(s.samples) < s.bound
}

func (s *ResourceSampler) nextElapsed(at time.Time) int64 {
	elapsed := at.Sub(s.started).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	if len(s.samples) > 0 && elapsed <= s.samples[len(s.samples)-1].ElapsedMS {
		return s.samples[len(s.samples)-1].ElapsedMS + 1
	}
	return elapsed
}

func (s *ResourceSampler) recordReading(sample *HostResourceSample, reading hostResourceReading, at time.Time) {
	s.recordSampleCPU(sample, &reading, at)
	if reading.memory != nil {
		sample.Memory = reading.memory
		s.memory.measured++
	}
	if reading.swap != nil {
		sample.Swap = reading.swap
		s.swap.measured++
	}
	if len(reading.problems) > 0 {
		s.recordReadingProblems(reading.problems)
	}
}

func (s *ResourceSampler) recordSampleCPU(sample *HostResourceSample, reading *hostResourceReading, at time.Time) {
	switch {
	case reading.cpuPercent != nil:
		sample.CPU = &CPUSample{
			BusyPercent: reading.cpuPercent.busy, UserPercent: reading.cpuPercent.user,
			SystemPercent: reading.cpuPercent.system, IdlePercent: reading.cpuPercent.idle,
			WindowMS: reading.cpuPercent.windowMS,
		}
		s.cpu.measured++
	case reading.cpuTicks != nil:
		cpu, problem := s.cpuDeltaSample(reading.cpuTicks, at)
		if problem != "" {
			reading.problems = append(reading.problems, problem)
		} else if cpu != nil {
			sample.CPU = cpu
			s.cpu.measured++
		}
		s.prevTicks, s.prevAt = reading.cpuTicks, at
	}
}

func (s *ResourceSampler) recordReadingProblems(problems []string) {
	s.problemTicks++
	for _, problem := range problems {
		s.countProblem(problem)
		if _, seen := s.problems[problem]; !seen {
			s.ordered = append(s.ordered, problem)
		}
		s.problems[problem]++
	}
}

func (s *ResourceSampler) countProblem(problem string) {
	switch resourceProblemDomain(problem) {
	case "cpu":
		s.cpu.problems++
	case "memory":
		s.memory.problems++
	case "swap":
		s.swap.problems++
	}
}

func (s *ResourceSampler) cpuDeltaSample(ticks *cpuTicksReading, at time.Time) (*CPUSample, string) {
	if s.prevTicks == nil {
		return nil, ""
	}
	if !cpuTicksAdvance(s.prevTicks, ticks) {
		return nil, "cpu: cumulative cpu counters did not advance consistently since the previous sample"
	}
	window := at.Sub(s.prevAt).Milliseconds()
	if window <= 0 {
		return nil, ""
	}
	elapsed := ticks.total - s.prevTicks.total
	scale := func(value uint64) float64 {
		return roundPercent(float64(value) / float64(elapsed) * 100)
	}
	user, system, idle := scale(ticks.user-s.prevTicks.user), scale(ticks.system-s.prevTicks.system), scale(ticks.idle-s.prevTicks.idle)
	busy := roundPercent(float64(elapsed-ticks.idle+s.prevTicks.idle) / float64(elapsed) * 100)
	return &CPUSample{
		BusyPercent: busy, UserPercent: user, SystemPercent: system, IdlePercent: idle,
		WindowMS: window,
	}, ""
}

func cpuTicksAdvance(previous, current *cpuTicksReading) bool {
	return current.total > previous.total &&
		current.user >= previous.user &&
		current.system >= previous.system &&
		current.idle >= previous.idle
}

func (s *ResourceSampler) Stop() ResourceSamples {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return s.document
	}
	s.stopped = true
	close(s.done)
	s.document = s.finalize(time.Now().UTC())
	return s.document
}

func (s *ResourceSampler) finalize(ended time.Time) ResourceSamples {
	coverage := ResourceSampleCoverage{
		Status: ResourceCoverageCollected, Source: s.source.label,
		IntervalMS: s.interval.Milliseconds(), SampleBound: s.bound,
		StartedAt: s.started, EndedAt: ended, EndBasis: ResourceSampleEndBasis,
		CPUState: resourceDomainState(s.cpu), MemoryState: resourceDomainState(s.memory), SwapState: resourceDomainState(s.swap),
		ProblemTicks: s.problemTicks,
	}
	coverage.Problems = boundedProblems(s.ordered)
	coverage.Truncated = len(s.samples) >= s.bound
	switch {
	case s.cpu.measured == 0 && s.memory.measured == 0 && s.swap.measured == 0:
		coverage.Status = ResourceCoverageFailed
	case coverage.Truncated || s.problemTicks > 0 || coverage.CPUState != ResourceStateMeasured ||
		coverage.MemoryState != ResourceStateMeasured || coverage.SwapState != ResourceStateMeasured:
		coverage.Status = ResourceCoveragePartial
	}
	return ResourceSamples{
		SchemaVersion: ResourceSamplesSchemaVersion, ValidationRunID: s.runID, Form: s.form,
		Host: s.source.host, Coverage: coverage, Samples: s.samples,
	}
}

func (s *ResourceSampler) unsupportedDocument(reason string) ResourceSamples {
	unsupported := ResourceStateUnsupported
	return ResourceSamples{
		SchemaVersion: ResourceSamplesSchemaVersion, ValidationRunID: s.runID, Form: s.form,
		Host: s.source.host,
		Coverage: ResourceSampleCoverage{
			Status: ResourceCoverageUnsupported, Source: s.source.label,
			IntervalMS:  s.interval.Milliseconds(),
			SampleBound: s.bound,
			CPUState:    unsupported, MemoryState: unsupported, SwapState: unsupported,
			Problems: []string{reason},
		},
	}
}

func resourceDomainState(counters resourceDomainCounters) string {
	switch {
	case counters.measured == 0:
		return ResourceStateMissing
	case counters.problems > 0:
		return ResourceStatePartial
	default:
		return ResourceStateMeasured
	}
}

func boundedProblems(ordered []string) []string {
	if len(ordered) <= ResourceSampleProblemsBound {
		return ordered
	}
	return ordered[:ResourceSampleProblemsBound]
}

func resourceProblemDomain(problem string) string {
	domain, _, _ := strings.Cut(problem, ":")
	return domain
}

func roundPercent(value float64) float64 {
	return float64(int64(value*100+0.5)) / 100
}

func EncodeResourceSamples(samples ResourceSamples) ([]byte, error) {
	if err := validateResourceSamples(samples); err != nil {
		return nil, err
	}
	return json.Marshal(samples)
}

func DecodeResourceSamples(data []byte) (ResourceSamples, error) {
	var samples ResourceSamples
	if err := json.Unmarshal(data, &samples); err != nil {
		return ResourceSamples{}, err
	}
	if err := validateResourceSamples(samples); err != nil {
		return ResourceSamples{}, err
	}
	return samples, nil
}

func validateResourceSamples(samples ResourceSamples) error {
	if samples.SchemaVersion != ResourceSamplesSchemaVersion {
		return fmt.Errorf("resource samples schema version %d is unsupported", samples.SchemaVersion)
	}
	if !ValidRunID(samples.ValidationRunID) {
		return fmt.Errorf("resource samples run identity is invalid")
	}
	if samples.Form != formGoTest && samples.Form != formGoTestRace {
		return fmt.Errorf("resource samples form is missing")
	}
	if err := validateResourceCoverage(samples.Coverage, len(samples.Samples)); err != nil {
		return err
	}
	previous := int64(-1)
	for _, sample := range samples.Samples {
		if err := validateResourceSampleRow(sample, previous); err != nil {
			return err
		}
		previous = sample.ElapsedMS
	}
	return nil
}

func validateResourceCoverage(coverage ResourceSampleCoverage, rows int) error {
	if !validResourceCoverageStatus(coverage.Status) {
		return fmt.Errorf("resource samples coverage status %q is unsupported", coverage.Status)
	}
	for _, state := range []string{coverage.CPUState, coverage.MemoryState, coverage.SwapState} {
		if !validResourceDomainState(state) {
			return fmt.Errorf("resource samples domain state %q is unsupported", state)
		}
	}
	if coverage.IntervalMS <= 0 || coverage.SampleBound <= 0 {
		return fmt.Errorf("resource samples sampling bounds are invalid")
	}
	if coverage.Status != ResourceCoverageUnsupported && coverage.StartedAt.IsZero() {
		return fmt.Errorf("resource samples coverage lacks its start")
	}
	if err := validateResourceCoverageRows(coverage, rows); err != nil {
		return err
	}
	return validateResourceCoverageConsistency(coverage, rows)
}

func validResourceCoverageStatus(status string) bool {
	switch status {
	case ResourceCoverageCollected, ResourceCoveragePartial, ResourceCoverageFailed, ResourceCoverageUnsupported:
		return true
	default:
		return false
	}
}

func validResourceDomainState(state string) bool {
	switch state {
	case ResourceStateMeasured, ResourceStatePartial, ResourceStateMissing, ResourceStateUnsupported:
		return true
	default:
		return false
	}
}

func validateResourceCoverageRows(coverage ResourceSampleCoverage, rows int) error {
	switch {
	case rows > coverage.SampleBound:
		return fmt.Errorf("resource sample rows exceed the declared bound")
	case coverage.Truncated && rows != coverage.SampleBound:
		return fmt.Errorf("resource samples truncation disagrees with the row count")
	}
	return nil
}

func validateResourceCoverageConsistency(coverage ResourceSampleCoverage, rows int) error {
	switch {
	case coverage.Status == ResourceCoverageCollected && (coverage.CPUState != ResourceStateMeasured ||
		coverage.MemoryState != ResourceStateMeasured || coverage.SwapState != ResourceStateMeasured || coverage.ProblemTicks > 0):
		return fmt.Errorf("resource samples coverage status disagrees with its domain states")
	case coverage.Status == ResourceCoverageUnsupported && rows > 0:
		return fmt.Errorf("unsupported resource samples carry rows")
	}
	return nil
}

func validateResourceSampleRow(sample HostResourceSample, previous int64) error {
	switch {
	case sample.ElapsedMS < 0 || sample.ElapsedMS <= previous:
		return fmt.Errorf("resource sample elapsed %d is not increasing", sample.ElapsedMS)
	case sample.CPU != nil && !validCPUSample(*sample.CPU):
		return fmt.Errorf("resource sample cpu percentages are invalid")
	case sample.CPU != nil && sample.CPU.WindowMS < 0:
		return fmt.Errorf("resource sample cpu window is negative")
	case sample.Swap != nil && sample.Swap.UsedBytes > sample.Swap.TotalBytes:
		return fmt.Errorf("resource sample swap usage exceeds its total")
	case sample.Swap != nil && sample.Swap.FreeBytes > sample.Swap.TotalBytes:
		return fmt.Errorf("resource sample swap free exceeds its total")
	}
	return nil
}

func validCPUSample(cpu CPUSample) bool {
	for _, percent := range []float64{cpu.BusyPercent, cpu.UserPercent, cpu.SystemPercent, cpu.IdlePercent} {
		if percent < 0 || percent > 100 {
			return false
		}
	}
	return true
}
