package peakpause

import (
	"testing"
	"time"
)

func TestFromEnvironmentIsOptIn(t *testing.T) {
	t.Setenv("GLM_WORKER_PEAK_PAUSE_ENABLED", "")
	t.Setenv("GLM_WORKER_PEAK_PAUSE_TIMEZONE", "bad/timezone")
	t.Setenv("GLM_WORKER_PEAK_PAUSE_WINDOWS", "invalid")
	policy, err := FromEnvironment()
	if err != nil || policy.Enabled() {
		t.Fatalf("unset feature must preserve previous behavior: enabled=%v err=%v", policy.Enabled(), err)
	}
	t.Setenv("GLM_WORKER_PEAK_PAUSE_ENABLED", "false")
	policy, err = FromEnvironment()
	if err != nil || policy.Enabled() {
		t.Fatalf("disabled feature must ignore schedule: enabled=%v err=%v", policy.Enabled(), err)
	}
	t.Setenv("GLM_WORKER_PEAK_PAUSE_ENABLED", "true")
	if _, err := FromEnvironment(); err == nil {
		t.Fatal("enabled feature must reject invalid configuration")
	}
}

func TestWindowBoundariesAndWeekends(t *testing.T) {
	policy, err := Parse("Asia/Tokyo", "mon-fri@14:00-18:00")
	if err != nil {
		t.Fatal(err)
	}
	location, _ := time.LoadLocation("Asia/Tokyo")
	for _, tc := range []struct {
		name   string
		at     time.Time
		paused bool
		next   time.Time
	}{
		{"before", time.Date(2026, 11, 6, 13, 59, 0, 0, location), false, time.Date(2026, 11, 6, 14, 0, 0, 0, location)},
		{"start", time.Date(2026, 11, 6, 14, 0, 0, 0, location), true, time.Date(2026, 11, 6, 18, 0, 0, 0, location)},
		{"end", time.Date(2026, 11, 6, 18, 0, 0, 0, location), false, time.Date(2026, 11, 9, 14, 0, 0, 0, location)},
		{"weekend", time.Date(2026, 11, 7, 12, 0, 0, 0, location), false, time.Date(2026, 11, 9, 14, 0, 0, 0, location)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active, next := policy.Evaluate(tc.at)
			if active != tc.paused || !next.Equal(tc.next) {
				t.Fatalf("Evaluate(%v) = %v/%v, want %v/%v", tc.at, active, next, tc.paused, tc.next)
			}
		})
	}
}

func TestOvernightAndOverlappingWindows(t *testing.T) {
	policy, err := Parse("UTC", "fri@23:00-02:00;sat@01:00-03:00")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 7, 1, 30, 0, 0, time.UTC)
	active, next := policy.Evaluate(now)
	want := time.Date(2026, 11, 7, 3, 0, 0, 0, time.UTC)
	if !active || !next.Equal(want) {
		t.Fatalf("overlapping pause = %v/%v, want true/%v", active, next, want)
	}
}

func TestInvalidWindowsFailClosed(t *testing.T) {
	for _, tc := range []struct {
		timezone string
		windows  string
	}{
		{"Invalid/Zone", "mon@14:00-18:00"},
		{"UTC", ""},
		{"UTC", "xyz@14:00-18:00"},
		{"UTC", "mon@14:00-14:00"},
		{"UTC", "mon@25:00-18:00"},
		{"UTC", "mon@14:00-xx:00"},
		{"UTC", "mon@14:00-18:00;"},
	} {
		if _, err := Parse(tc.timezone, tc.windows); err == nil {
			t.Fatalf("invalid pause config accepted: %q/%q", tc.timezone, tc.windows)
		}
	}
}
