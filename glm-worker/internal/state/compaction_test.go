package state

import (
	"testing"
	"time"
)

func TestBuildCompactionReportObservesBoundariesPerCall(t *testing.T) {
	base := timelineBaseTime()
	firstTask := []TaskEventRecord{
		{
			TaskID: "task-1", CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new-rule-activation-1",
			Resumed: true, Seq: 1, Timestamp: base, Kind: "system", Subtype: "init",
		},
		{
			TaskID: "task-1", CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new-rule-activation-1",
			Resumed: true, Seq: 2, Timestamp: base.Add(10 * time.Second), Kind: "system", Subtype: "compact_boundary",
		},
		{
			TaskID: "task-1", CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new-rule-activation-1",
			Resumed: true, Seq: 3, Timestamp: base.Add(11 * time.Second), Kind: "assistant",
		},
		{
			TaskID: "task-1", CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new-rule-activation-1",
			Resumed: true, Seq: 5, Timestamp: base.Add(30 * time.Second), Kind: "system", Subtype: "compact_boundary",
		},
		{
			TaskID: "task-1", CallID: "call-1", SessionID: "sess-a", Role: "worker", Phase: "worker-new-rule-activation-1",
			Resumed: true, Seq: 6, Timestamp: base.Add(40 * time.Second), Kind: "result", Subtype: "success", NumTurns: 44,
		},
		{
			TaskID: "task-1", CallID: "call-2", SessionID: "sess-a", Role: "worker", Phase: "worker-explicit-fix",
			Resumed: true, Seq: 1, Timestamp: base.Add(60 * time.Second), Kind: "assistant",
		},
	}
	secondTask := []TaskEventRecord{
		{
			TaskID: "task-2", CallID: "call-3", SessionID: "sess-b", Role: "reviewer", Phase: "reviewer-1",
			Seq: 4, Timestamp: base.Add(20 * time.Second), Kind: "system", Subtype: "compact_boundary",
		},
	}

	report := BuildCompactionReport([]TaskEvents{
		{TaskID: "task-1", Records: firstTask},
		{TaskID: "task-2", Records: secondTask},
	})

	if report.BoundarySubtype != CompactionBoundarySubtype {
		t.Fatalf("boundary subtype = %q", report.BoundarySubtype)
	}
	if report.Tasks != 2 || report.CallsObserved != 3 || report.CallsWithBoundaries != 2 || report.Boundaries != 3 {
		t.Fatalf("計数 = %+v", report)
	}
	if len(report.Observations) != 3 {
		t.Fatalf("observation数 = %d: %+v", len(report.Observations), report.Observations)
	}
	first := report.Observations[0]
	if first.TaskID != "task-1" || first.CallID != "call-1" || first.SessionID != "sess-a" ||
		first.Role != "worker" || first.Phase != "worker-new-rule-activation-1" ||
		first.PhaseCategory != WorkerPhaseCategoryNew || !first.Resumed ||
		first.SessionCallIndex != 1 || first.Seq != 2 || first.CallEvents != 5 || first.CallTurns != 44 {
		t.Fatalf("call-1最初のboundary = %+v", first)
	}
	if report.Observations[1].TaskID != "task-2" || report.Observations[1].Seq != 4 {
		t.Fatalf("時刻順の2番目 = %+v", report.Observations[1])
	}
	if report.Observations[2].CallID != "call-1" || report.Observations[2].Seq != 5 {
		t.Fatalf("call-1二番目のboundary = %+v", report.Observations[2])
	}

	wantGroups := []CompactionGroupTotal{
		{Role: "reviewer", PhaseCategory: "reviewer-1", Resumed: false, Calls: 1, Boundaries: 1},
		{Role: "worker", PhaseCategory: WorkerPhaseCategoryNew, Resumed: true, Calls: 1, Boundaries: 2},
	}
	if len(report.Groups) != len(wantGroups) {
		t.Fatalf("group数 = %d: %+v", len(report.Groups), report.Groups)
	}
	for index, want := range wantGroups {
		got := report.Groups[index]
		if got != want {
			t.Fatalf("group[%d] = %+v want %+v", index, got, want)
		}
	}
}

func TestBuildCompactionReportCountsOnlyBoundaryEvents(t *testing.T) {
	base := timelineBaseTime()
	records := []TaskEventRecord{
		{TaskID: "task-1", CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 1, Timestamp: base, Kind: "system", Subtype: "status"},
		{TaskID: "task-1", CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 2, Timestamp: base.Add(time.Second), Kind: "system", Subtype: "init"},
		{TaskID: "task-1", CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 3, Timestamp: base.Add(2 * time.Second), Kind: "assistant"},
		{TaskID: "task-1", CallID: "call-1", Role: "worker", Phase: "worker-new", Seq: 4, Timestamp: base.Add(3 * time.Second), Kind: "user", Subtype: "compact_boundary"},
	}

	report := BuildCompactionReport([]TaskEvents{
		{TaskID: "task-1", Records: records},
		{TaskID: "task-empty", Records: nil},
	})

	if report.Tasks != 1 || report.CallsObserved != 1 || report.CallsWithBoundaries != 0 || report.Boundaries != 0 {
		t.Fatalf("計数 = %+v", report)
	}
	if len(report.Observations) != 0 {
		t.Fatalf("observations = %+v", report.Observations)
	}
	if len(report.Groups) != 0 {
		t.Fatalf("groups = %+v", report.Groups)
	}
}
