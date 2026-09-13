package parentidentity

import (
	"errors"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
)

func TestReadSessionLimitForIdentityBind(t *testing.T) {
	used := int64(37)
	resetsAt := int64(1787685137)
	capturedAt := time.Date(2026, 9, 14, 8, 30, 0, 0, time.FixedZone("JST", 9*60*60))
	complete := codexlimit.Snapshot{
		LimitID: "codex",
		FiveHour: codexlimit.Window{
			UsedPercent: &used,
			ResetsAt:    &resetsAt,
		},
	}

	tests := []struct {
		name string
		read func(string) (codexlimit.Snapshot, error)
		want bool
	}{
		{
			name: "read failure",
			read: func(string) (codexlimit.Snapshot, error) {
				return codexlimit.Snapshot{}, errors.New("read failed")
			},
		},
		{
			name: "missing limit id",
			read: func(string) (codexlimit.Snapshot, error) {
				snapshot := complete
				snapshot.LimitID = ""
				return snapshot, nil
			},
		},
		{
			name: "missing used percent",
			read: func(string) (codexlimit.Snapshot, error) {
				snapshot := complete
				snapshot.FiveHour.UsedPercent = nil
				return snapshot, nil
			},
		},
		{
			name: "missing reset time",
			read: func(string) (codexlimit.Snapshot, error) {
				snapshot := complete
				snapshot.FiveHour.ResetsAt = nil
				return snapshot, nil
			},
		},
		{
			name: "complete reading",
			read: func(string) (codexlimit.Snapshot, error) {
				return complete, nil
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readSessionLimitForIdentityBind("codex-test", func() time.Time { return capturedAt }, tt.read)
			if !tt.want {
				if got != nil {
					t.Fatalf("reading = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("reading = nil, want complete reading")
			}
			if got.LimitID != complete.LimitID || got.UsedPercent != used || got.ResetsAt != resetsAt {
				t.Fatalf("reading = %#v", got)
			}
			if want := capturedAt.UTC(); !got.CapturedAt.Equal(want) || got.CapturedAt.Location() != time.UTC {
				t.Fatalf("captured_at = %v, want UTC %v", got.CapturedAt, want)
			}
		})
	}
}
