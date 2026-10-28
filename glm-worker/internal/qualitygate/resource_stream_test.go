package qualitygate

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestResourceSamplerPublishesBeforeStop(t *testing.T) {
	var documents []ResourceSamples
	s := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("1", 32), Form: "go-test", Interval: time.Hour,
		Source:  samplerTestSource(func() hostResourceReading { return fullHostResourceReading(nil) }),
		Publish: func(d ResourceSamples) { documents = append(documents, d) },
	})
	defer s.Stop()
	s.collect()
	if len(documents) != 2 || len(documents[0].Samples) != 1 || len(documents[1].Samples) != 2 {
		t.Fatalf("stream snapshots: %#v", documents)
	}
	if documents[1].Coverage.ExecutionState != "running" || !documents[1].Coverage.EndedAt.IsZero() {
		t.Fatalf("running coverage: %#v", documents[1].Coverage)
	}
	for _, d := range documents {
		if _, err := EncodeResourceSamples(d); err != nil {
			t.Fatal(err)
		}
	}
	final := s.StopWithState("interrupted")
	if final.Coverage.ExecutionState != "interrupted" || documents[len(documents)-1].Coverage.EndedAt.IsZero() {
		t.Fatal("terminal snapshot missing")
	}
}

func TestResourceSamplerAsyncStartAndCancellation(t *testing.T) {
	entered, cancelled, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	source := samplerTestSource(func() hostResourceReading {
		close(entered)
		<-cancelled
		close(finished)
		return fullHostResourceReading(nil)
	})
	source.cancel = func() { once.Do(func() { close(cancelled) }) }
	var mu sync.Mutex
	var snapshots []ResourceSamples
	s := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("1", 32), Form: "go-test", AsyncFirst: true, Source: source,
		Publish: func(d ResourceSamples) { mu.Lock(); defer mu.Unlock(); snapshots = append(snapshots, d) },
	})
	defer s.Stop()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("sampling not started")
	}
	final := s.Stop()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("read not cancelled")
	}
	if final.Coverage.SynchronousSamples != 0 || final.Coverage.SynchronousWaitMS != 0 {
		t.Fatalf("async costs: %#v", final.Coverage)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(snapshots) < 2 || snapshots[len(snapshots)-1].Coverage.ExecutionState != "completed" {
		t.Fatalf("snapshots: %#v", snapshots)
	}
}

func TestResourceSamplerBoundRetainsInterruption(t *testing.T) {
	s := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("1", 32), Form: "go-test", Interval: time.Hour, SampleBound: 1,
		Source: samplerTestSource(func() hostResourceReading { return fullHostResourceReading(nil) }),
	})
	defer s.Stop()
	running := s.Snapshot()
	if !running.Coverage.Truncated || running.Coverage.EndedAt.IsZero() || running.Coverage.EndBasis != "sample-bound-reached" {
		t.Fatalf("bound coverage: %#v", running.Coverage)
	}
	final := s.StopWithState("interrupted")
	if !final.Coverage.Truncated || final.Coverage.ExecutionState != "interrupted" || !final.Coverage.EndedAt.Equal(running.Coverage.EndedAt) {
		t.Fatalf("terminal bound: %#v", final.Coverage)
	}
}

func TestResourceSamplerStopPreventsLatePublication(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	reads := 0
	var mu sync.Mutex
	publications := 0
	s := StartResourceSampler(ResourceSamplerConfig{
		ValidationRunID: strings.Repeat("1", 32), Form: "go-test", Interval: time.Hour,
		Source: samplerTestSource(func() hostResourceReading {
			reads++
			if reads == 2 {
				close(entered)
				<-release
			}
			return fullHostResourceReading(nil)
		}),
		Publish: func(ResourceSamples) { mu.Lock(); publications++; mu.Unlock() },
	})
	defer s.Stop()
	go func() { s.collect(); close(finished) }()
	<-entered
	s.Stop()
	mu.Lock()
	stoppedCount := publications
	mu.Unlock()
	close(release)
	<-finished
	s.Stop()
	mu.Lock()
	defer mu.Unlock()
	if publications != stoppedCount {
		t.Fatalf("publish after stop: stopped=%d final=%d", stoppedCount, publications)
	}
}

func TestResourceSamplerInvalidDomainKeepsValidHistory(t *testing.T) {
	reads := 0
	s := manualResourceSampler(t, func() hostResourceReading {
		reading := fullHostResourceReading(nil)
		reading.cpuPercent = &cpuPercentReading{busy: 37, user: 20, system: 17, idle: 63}
		if reads == 1 {
			reading.cpuPercent = &cpuPercentReading{busy: 100.01, user: 70, system: 30.01, idle: 0}
			reading.swap = &SwapSample{TotalBytes: 4, UsedBytes: 5}
		}
		reads++
		return reading
	})
	s.collect()
	s.collect()
	document := s.Stop()
	if _, err := EncodeResourceSamples(document); err != nil {
		t.Fatal(err)
	}
	if len(document.Samples) != 3 || document.Samples[0].CPU == nil || document.Samples[2].CPU == nil {
		t.Fatalf("valid history lost: %#v", document.Samples)
	}
	bad := document.Samples[1]
	if bad.CPU != nil || bad.Swap != nil || bad.Memory == nil {
		t.Fatalf("invalid domain was retained or valid domain lost: %#v", bad)
	}
	if document.Coverage.CPUState != ResourceStatePartial || document.Coverage.SwapState != ResourceStatePartial || document.Coverage.Status != ResourceCoveragePartial {
		t.Fatalf("invalid domain coverage: %#v", document.Coverage)
	}
}
