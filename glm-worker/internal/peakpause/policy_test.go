package peakpause

import (
	"testing"
	"time"
)

type pauseCase struct {
	name   string
	at     string
	paused bool
	next   string
}

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

func TestDSTSpringForwardMissingCivilMinutes(t *testing.T) {
	policy, err := Parse("America/New_York", "sun@02:30-03:30")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, policy, []pauseCase{
		{"before missing start", "2027-03-14T06:30:00Z", false, "2027-03-14T07:00:00Z"},
		{"still before jump", "2027-03-14T06:59:00Z", false, "2027-03-14T07:00:00Z"},
		{"first existing minute", "2027-03-14T07:00:00Z", true, "2027-03-14T07:30:00Z"},
		{"last included minute", "2027-03-14T07:29:00Z", true, "2027-03-14T07:30:00Z"},
		{"exclusive end", "2027-03-14T07:30:00Z", false, "2027-03-21T06:30:00Z"},
	})
}

func TestDSTSpringForwardEntireWindowDisappears(t *testing.T) {
	policy, err := Parse("America/New_York", "sun@02:10-02:50")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, policy, []pauseCase{
		{"before missing Sunday", "2027-03-12T12:00:00Z", false, "2027-03-21T06:10:00Z"},
		{"missing interval", "2027-03-14T06:30:00Z", false, "2027-03-21T06:10:00Z"},
		{"after spring forward", "2027-03-14T07:30:00Z", false, "2027-03-21T06:10:00Z"},
	})
}

func TestDSTFallBackRepeatedMinutes(t *testing.T) {
	policy, err := Parse("America/New_York", "sun@01:30-01:45")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, policy, []pauseCase{
		{"before first occurrence", "2027-11-07T05:29:00Z", false, "2027-11-07T05:30:00Z"},
		{"first occurrence", "2027-11-07T05:30:00Z", true, "2027-11-07T05:45:00Z"},
		{"gap before fallback", "2027-11-07T05:45:00Z", false, "2027-11-07T06:30:00Z"},
		{"repeated hour before start", "2027-11-07T06:15:00Z", false, "2027-11-07T06:30:00Z"},
		{"second occurrence", "2027-11-07T06:30:00Z", true, "2027-11-07T06:45:00Z"},
		{"after second occurrence", "2027-11-07T06:45:00Z", false, "2027-11-14T06:30:00Z"},
	})
}

func TestDSTOvernightWeekdayAndAdjacentMerge(t *testing.T) {
	overnight, err := Parse("America/New_York", "sat@23:30-01:30")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, overnight, []pauseCase{
		{"Saturday start", "2027-11-07T03:30:00Z", true, "2027-11-07T05:30:00Z"},
		{"Sunday gap", "2027-11-07T05:30:00Z", false, "2027-11-07T06:00:00Z"},
		{"Sunday second hour", "2027-11-07T06:00:00Z", true, "2027-11-07T06:30:00Z"},
	})
	adjacent, err := Parse("America/New_York", "sun@01:30-01:45;sun@01:45-02:00")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, adjacent, []pauseCase{
		{"first merged period", "2027-11-07T05:40:00Z", true, "2027-11-07T06:00:00Z"},
		{"gap between repeated periods", "2027-11-07T06:15:00Z", false, "2027-11-07T06:30:00Z"},
		{"second merged period", "2027-11-07T06:50:00Z", true, "2027-11-07T07:00:00Z"},
	})
}

func TestSkippedCivilDateDoesNotInheritPreviousWeekday(t *testing.T) {
	policy, err := Parse("Pacific/Apia", "fri@12:00-13:00")
	if err != nil {
		t.Fatal(err)
	}
	assertPauseCases(t, policy, []pauseCase{
		{"skipped Friday", "2011-12-29T15:00:00Z", false, "2012-01-05T22:00:00Z"},
	})
}

func assertPauseCases(t *testing.T, policy Policy, cases []pauseCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at, err := time.Parse(time.RFC3339, tc.at)
			if err != nil {
				t.Fatal(err)
			}
			want, err := time.Parse(time.RFC3339, tc.next)
			if err != nil {
				t.Fatal(err)
			}
			gotPaused, gotNext := policy.Evaluate(at)
			if gotPaused != tc.paused || !gotNext.Equal(want) {
				t.Fatalf("Evaluate(%s) = %v/%v, want %v/%v", tc.at, gotPaused, gotNext, tc.paused, want)
			}
		})
	}
}
