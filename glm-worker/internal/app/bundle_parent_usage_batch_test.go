package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTelemetryCompactParentUsageScansEachParentOnce(t *testing.T) {
	fixture := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, fixture.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		fixture.start.Add(-3*time.Hour), parentUsagePhaseLines(t, fixture.start, fixture.completeAt))
	otherThread := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	writeAnalysisRollout(t, fixture.codexHome, "sessions/other.jsonl", otherThread,
		fixture.start.Add(-3*time.Hour), parentUsagePhaseLines(t, fixture.start, fixture.completeAt))
	task, err := selectBundleTask(fixture.st, "")
	if err != nil {
		t.Fatal(err)
	}
	stats := []state.TaskStats{task.Stats, task.Stats, task.Stats, task.Stats}
	stats[1].TaskID = "second-task"
	stats[1].StartedAt = fixture.start.Add(time.Minute)
	stats[2].TaskID = "other-parent-task"
	stats[2].ParentCodexThreadID = otherThread
	stats[3].TaskID = "missing-parent-task"
	stats[3].ParentCodexThreadID = ""
	enumerations, scans := 0, 0
	got := buildTelemetryCompactParentUsageWithScans(fixture.cfg, fixture.st, stats,
		func(home string) ([]codexRollout, error) {
			enumerations++
			return scanCodexRollouts(home)
		},
		func(chain []codexRollout, start, end time.Time) (bundleRolloutScan, error) {
			scans++
			return scanCodexRolloutChainWindow(chain, start, end)
		})
	if enumerations != 1 || scans != 2 {
		t.Fatalf("rollout enumeration = %d, parent content scans = %d", enumerations, scans)
	}
	want := telemetryCompactParentUsage{Tasks: len(stats), ByStatus: make(map[string]int)}
	for _, entry := range stats {
		one := buildTelemetryCompactParentUsage(fixture.cfg, fixture.st, []state.TaskStats{entry})
		want.Available += one.Available
		want.Ambiguous += one.Ambiguous
		want.Unknown += one.Unknown
		for status, count := range one.ByStatus {
			want.ByStatus[status] += count
		}
	}
	if !reflect.DeepEqual(got, want) || got.Available != 1 || got.Unknown != 3 {
		t.Fatalf("batched usage = %#v, per-task usage = %#v", got, want)
	}
}

func TestParentUsageBatchPreservesTaskIntervals(t *testing.T) {
	fixture := newAnalysisTerminalTask(t)
	writeAnalysisRollout(t, fixture.codexHome, analysisRolloutRel(), codexTestParentThreadID,
		fixture.start.Add(-3*time.Hour), parentUsagePhaseLines(t, fixture.start, fixture.completeAt))
	task, err := selectBundleTask(fixture.st, "")
	if err != nil {
		t.Fatal(err)
	}
	stats := []state.TaskStats{task.Stats, task.Stats}
	stats[1].StartedAt = fixture.completeAt.Add(-time.Second)
	batch := newParentUsageBatch(fixture.codexHome, stats, scanCodexRollouts, scanCodexRolloutChainWindow)
	for _, entry := range stats {
		sample := bundleTask{ID: entry.TaskID, Status: string(entry.Status), Stats: entry}
		evidence := batch.evidence(sample)
		got := buildParentUsageReportFromScan(fixture.st, sample, evidence.association, evidence.scan, evidence.err)
		want := buildParentUsageReport(fixture.cfg, fixture.st, sample)
		if !reflect.DeepEqual(got.ParentSession, want.ParentSession) || !reflect.DeepEqual(got.Intervals, want.Intervals) {
			t.Fatalf("batched report = %#v, per-task report = %#v", got, want)
		}
	}
}

func TestTelemetryCompactParentUsageRetainsScanFailure(t *testing.T) {
	for _, failure := range []string{"enumeration", "content"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newAnalysisTerminalTask(t)
			writeAnalysisRollout(t, fixture.codexHome, analysisRolloutRel(), codexTestParentThreadID,
				fixture.start.Add(-3*time.Hour), parentUsagePhaseLines(t, fixture.start, fixture.completeAt))
			task, err := selectBundleTask(fixture.st, "")
			if err != nil {
				t.Fatal(err)
			}
			second := task.Stats
			second.TaskID = "second-task"
			enumerations, scans := 0, 0
			got := buildTelemetryCompactParentUsageWithScans(fixture.cfg, fixture.st, []state.TaskStats{task.Stats, second},
				func(home string) ([]codexRollout, error) {
					enumerations++
					if failure == "enumeration" {
						return nil, errors.New("enumeration failed")
					}
					return scanCodexRollouts(home)
				},
				func([]codexRollout, time.Time, time.Time) (bundleRolloutScan, error) {
					scans++
					return bundleRolloutScan{}, errors.New("content unreadable")
				})
			status, wantScans := "unavailable/unavailable", 0
			if failure == "content" {
				status, wantScans = "included/unreadable", 1
			}
			if enumerations != 1 || scans != wantScans || got.Unknown != 2 || got.ByStatus[status] != 2 {
				t.Fatalf("usage = %#v, enumeration = %d, scans = %d", got, enumerations, scans)
			}
		})
	}
}
