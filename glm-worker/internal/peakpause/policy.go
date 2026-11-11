package peakpause

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Window struct {
	days        [7]bool
	startMinute int
	endMinute   int
}

type Policy struct {
	location *time.Location
	windows  []Window
}

type interval struct {
	start time.Time
	end   time.Time
}

const (
	enabledEnv  = "GLM_WORKER_PEAK_PAUSE_ENABLED"
	timezoneEnv = "GLM_WORKER_PEAK_PAUSE_TIMEZONE"
	windowsEnv  = "GLM_WORKER_PEAK_PAUSE_WINDOWS"
)

var weekdays = map[string]time.Weekday{
	"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday,
}

func FromEnvironment() (Policy, error) {
	raw := strings.TrimSpace(os.Getenv(enabledEnv))
	if raw == "" {
		return Policy{}, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return Policy{}, fmt.Errorf("%s must be a boolean: %w", enabledEnv, err)
	}
	if !enabled {
		return Policy{}, nil
	}
	return Parse(os.Getenv(timezoneEnv), os.Getenv(windowsEnv))
}

func Parse(timezone, windows string) (Policy, error) {
	if strings.TrimSpace(timezone) == "" {
		return Policy{}, fmt.Errorf("%s is required when peak pause is enabled", timezoneEnv)
	}
	location, err := time.LoadLocation(strings.TrimSpace(timezone))
	if err != nil {
		return Policy{}, fmt.Errorf("%s: %w", timezoneEnv, err)
	}
	if strings.TrimSpace(windows) == "" {
		return Policy{}, fmt.Errorf("%s is required when peak pause is enabled", windowsEnv)
	}
	parts := strings.Split(windows, ";")
	parsed := make([]Window, 0, len(parts))
	for _, part := range parts {
		window, err := parseWindow(strings.TrimSpace(part))
		if err != nil {
			return Policy{}, fmt.Errorf("%s: %w", windowsEnv, err)
		}
		parsed = append(parsed, window)
	}
	return Policy{location: location, windows: parsed}, nil
}

func (p Policy) Enabled() bool {
	return p.location != nil && len(p.windows) != 0
}

func (p Policy) Evaluate(now time.Time) (bool, time.Time) {
	if !p.Enabled() {
		return false, time.Time{}
	}
	for _, current := range mergeIntervals(p.intervalsAround(now)) {
		if now.Before(current.start) {
			return false, current.start
		}
		if now.Before(current.end) {
			return true, current.end
		}
	}
	return false, time.Time{}
}

func (p Policy) intervalsAround(now time.Time) []interval {
	local := now.In(p.location)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	ranges := make([]interval, 0, 16*len(p.windows))
	for offset := -1; offset <= 15; offset++ {
		day := midnight.AddDate(0, 0, offset)
		for _, window := range p.windows {
			if window.days[day.Weekday()] {
				ranges = append(ranges, civilWindowIntervals(day, window, p.location)...)
			}
		}
	}
	return ranges
}

func civilWindowIntervals(day time.Time, window Window, location *time.Location) []interval {
	civilStart := day.Add(time.Duration(window.startMinute) * time.Minute)
	civilEnd := day.Add(time.Duration(window.endMinute) * time.Minute)
	if window.endMinute <= window.startMinute {
		civilEnd = civilEnd.Add(24 * time.Hour)
	}

	const margin = 48 * time.Hour
	limit := civilEnd.Add(margin)
	var ranges []interval
	for cursor := civilStart.Add(-margin); cursor.Before(limit); {
		local := cursor.In(location)
		_, offset := local.Zone()
		_, zoneEnd := local.ZoneBounds()
		segmentEnd := zoneEnd
		if segmentEnd.IsZero() || segmentEnd.After(limit) {
			segmentEnd = limit
		}
		shift := time.Duration(offset) * time.Second
		start := civilStart.Add(-shift)
		end := civilEnd.Add(-shift)
		if start.Before(cursor) {
			start = cursor
		}
		if end.After(segmentEnd) {
			end = segmentEnd
		}
		if start.Before(end) {
			ranges = append(ranges, interval{start: start, end: end})
		}
		cursor = segmentEnd
	}
	return ranges
}

func mergeIntervals(ranges []interval) []interval {
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start.Before(ranges[j].start) })
	merged := make([]interval, 0, len(ranges))
	for _, current := range ranges {
		n := len(merged)
		if n > 0 && !current.start.After(merged[n-1].end) {
			if current.end.After(merged[n-1].end) {
				merged[n-1].end = current.end
			}
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func parseWindow(raw string) (Window, error) {
	parts := strings.Split(raw, "@")
	if len(parts) != 2 {
		return Window{}, fmt.Errorf("invalid window %q (expected days@HH:MM-HH:MM)", raw)
	}
	days, err := parseDays(parts[0])
	if err != nil {
		return Window{}, err
	}
	hours := strings.Split(parts[1], "-")
	if len(hours) != 2 {
		return Window{}, fmt.Errorf("invalid time interval %q", parts[1])
	}
	start, err := parseClock(hours[0])
	if err != nil {
		return Window{}, err
	}
	end, err := parseClock(hours[1])
	if err != nil {
		return Window{}, err
	}
	if start == end {
		return Window{}, fmt.Errorf("zero-length or full-day window %q is not supported", raw)
	}
	return Window{days: days, startMinute: start, endMinute: end}, nil
}

func parseDays(raw string) ([7]bool, error) {
	var days [7]bool
	if strings.TrimSpace(raw) == "" {
		return days, fmt.Errorf("days cannot be empty")
	}
	for _, spec := range strings.Split(strings.ToLower(raw), ",") {
		if err := markDayRange(&days, strings.TrimSpace(spec)); err != nil {
			return days, err
		}
	}
	return days, nil
}

func markDayRange(days *[7]bool, spec string) error {
	ends := strings.Split(spec, "-")
	if len(ends) < 1 || len(ends) > 2 {
		return fmt.Errorf("invalid day specification %q", spec)
	}
	from, ok := weekdays[ends[0]]
	if !ok {
		return fmt.Errorf("invalid weekday %q", ends[0])
	}
	to := from
	if len(ends) == 2 {
		var valid bool
		to, valid = weekdays[ends[1]]
		if !valid {
			return fmt.Errorf("invalid weekday %q", ends[1])
		}
	}
	for day := from; ; day = (day + 1) % 7 {
		days[day] = true
		if day == to {
			break
		}
	}
	return nil
}

func parseClock(raw string) (int, error) {
	if len(raw) != 5 || raw[2] != ':' {
		return 0, fmt.Errorf("invalid time %q (expected HH:MM)", raw)
	}
	hour, errHour := strconv.Atoi(raw[:2])
	minute, errMinute := strconv.Atoi(raw[3:])
	if errHour != nil || errMinute != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("invalid time %q", raw)
	}
	return hour*60 + minute, nil
}
