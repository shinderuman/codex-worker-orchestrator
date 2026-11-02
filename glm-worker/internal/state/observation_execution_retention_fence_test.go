package state

import (
	"fmt"
	"testing"
	"time"
)

// A completed execution may leave the 16-entry display history, but its
// admission identity must remain fenced for the original decision round.
func TestObservationReplayFenceSurvivesTerminalRetention(t *testing.T) {
	for _, status := range []string{
		ObservationExecutionStatusPass,
		ObservationExecutionStatusFail,
		ObservationExecutionStatusIndeterminate,
	} {
		t.Run(status, func(t *testing.T) {
			st := newObservationExecutionStore(t, observationExecutionDeclaration)
			admitObservationDecisionBoundary(t, st)
			admission, err := st.ObservationExecuteAdmission()
			if err != nil {
				t.Fatal(err)
			}
			taskID := admission.TaskID
			startedAt := time.Now().UTC()
			if status == ObservationExecutionStatusIndeterminate {
				startedAt = startedAt.Add(-time.Minute)
			}
			first := ObservationExecutionRecord{
				ExecutionID:      "replay-original",
				TaskID:           taskID,
				Operation:        "shadow-eval",
				ParamsDigest:     "original-digest",
				Status:           ObservationExecutionStatusInFlight,
				DecisionRound:    admission.Round,
				DeadlineMS:       30_000,
				StartedAtRFC3339: startedAt.Format(time.RFC3339Nano),
			}
			if err := st.BeginObservationExecution(first); err != nil {
				t.Fatal(err)
			}
			if status == ObservationExecutionStatusIndeterminate {
				if err := st.RecoverStaleObservationExecutions(time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
			} else {
				completed := first
				completed.Status = status
				completed.CompletedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
				if err := st.CompleteObservationExecution(completed); err != nil {
					t.Fatal(err)
				}
			}

			// Exercise the real Begin/Complete path, not only Append's display
			// history shortcut, beyond the terminal retention limit.
			for index := 0; index < observationExecutionsRetention+1; index++ {
				next := first
				next.ExecutionID = fmt.Sprintf("replay-later-%d", index)
				next.ParamsDigest = fmt.Sprintf("later-digest-%d", index)
				next.StartedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
				if err := st.BeginObservationExecution(next); err != nil {
					t.Fatal(err)
				}
				next.Status = ObservationExecutionStatusPass
				next.CompletedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
				if err := st.CompleteObservationExecution(next); err != nil {
					t.Fatal(err)
				}
			}
			records, err := st.ObservationExecutions()
			if err != nil {
				t.Fatal(err)
			}
			if len(records) != observationExecutionsRetention {
				t.Fatalf("display history = %d, want %d", len(records), observationExecutionsRetention)
			}
			for _, record := range records {
				if record.ExecutionID == first.ExecutionID {
					t.Fatal("first record still in display history; fixture did not exercise eviction")
				}
			}
			exists, err := st.HasObservationExecution(first.Operation, first.ParamsDigest, admission.Round)
			if err != nil || !exists {
				t.Fatalf("evicted terminal %s lost its replay fence: exists=%v err=%v", status, exists, err)
			}
			replay := first
			replay.ExecutionID = "replay-new-execution-id"
			replay.StartedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
			if err := st.BeginObservationExecution(replay); err == nil {
				t.Fatalf("evicted terminal %s was readmitted with a new execution ID", status)
			}

			// A different parameter identity is not a replay.
			other := first
			other.ExecutionID = "replay-other-params"
			other.ParamsDigest = "different-digest"
			other.StartedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
			if err := st.BeginObservationExecution(other); err != nil {
				t.Fatalf("different params must be admitted: %v", err)
			}
			other.Status = ObservationExecutionStatusPass
			other.CompletedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
			if err := st.CompleteObservationExecution(other); err != nil {
				t.Fatal(err)
			}

			// The same params are valid again at the next canonical round.
			if _, err := st.BeginParentDecision(); err != nil {
				t.Fatal(err)
			}
			if err := st.CommitParentActionBegin(); err != nil {
				t.Fatal(err)
			}
			admitObservationDecisionBoundary(t, st)
			laterAdmission, err := st.ObservationExecuteAdmission()
			if err != nil {
				t.Fatal(err)
			}
			if laterAdmission.Round <= admission.Round {
				t.Fatalf("decision round failed to advance: %d -> %d", admission.Round, laterAdmission.Round)
			}
			exists, err = st.HasObservationExecution(first.Operation, first.ParamsDigest, laterAdmission.Round)
			if err != nil || exists {
				t.Fatalf("previous round incorrectly fences next round: exists=%v err=%v", exists, err)
			}
			later := first
			later.ExecutionID = "replay-next-round"
			later.DecisionRound = laterAdmission.Round
			later.StartedAtRFC3339 = time.Now().UTC().Format(time.RFC3339Nano)
			if err := st.BeginObservationExecution(later); err != nil {
				t.Fatalf("next-round admission rejected: %v", err)
			}
		})
	}
}
