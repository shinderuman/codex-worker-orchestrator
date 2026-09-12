package state

import (
	"strings"
	"testing"
)

func TestPendingUnparkCleanupRejectsIncompatibleRecord(t *testing.T) {
	for _, test := range []struct {
		name   string
		record ParkRecord
		want   string
	}{
		{
			name: "task mismatch",
			record: ParkRecord{
				TaskID:     "other-task",
				FromStatus: TaskStatusWaitingDecision,
				Cleanup:    &ParkCleanup{},
			},
			want: "does not match current task",
		},
		{
			name: "missing cleanup",
			record: ParkRecord{
				TaskID:     "task-1",
				FromStatus: TaskStatusWaitingDecision,
			},
			want: "without an unpark cleanup checkpoint",
		},
		{
			name: "status mismatch",
			record: ParkRecord{
				TaskID:     "task-1",
				FromStatus: TaskStatusWaitingSolReview,
				Cleanup:    &ParkCleanup{},
			},
			want: "does not match current status",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			st := &StateStore{dir: t.TempDir()}
			if err := st.Write("task.id", "task-1"); err != nil {
				t.Fatal(err)
			}
			if err := st.Write("task.status", string(TaskStatusWaitingDecision)); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveParkRecord(test.record); err != nil {
				t.Fatal(err)
			}

			_, pending, err := st.PendingUnparkCleanup()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PendingUnparkCleanup error = %v, want %q", err, test.want)
			}
			if pending {
				t.Fatal("incompatible park record was treated as pending cleanup")
			}
		})
	}
}
