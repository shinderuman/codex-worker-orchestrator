package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectZaiFiveHourLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.log")
	content := "API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34][202607221342470f952f313a624fd3]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	limit, ok := DetectZaiFiveHourLimit(path)
	if !ok {
		t.Fatal("expected Z.ai 5h limit")
	}
	if limit.ResetAtCST != "2026-07-22 14:06:34" {
		t.Fatalf("ResetAtCST = %q", limit.ResetAtCST)
	}
	if limit.ResetAtRFC3339 != "2026-07-22T14:06:34+08:00" {
		t.Fatalf("ResetAtRFC3339 = %q", limit.ResetAtRFC3339)
	}
}

func TestAutoResumeScheduleSecondPrecision(t *testing.T) {
	cases := []struct {
		name        string
		resetAt     string
		wantResumed string
	}{
		{"second precision reset", "2026-07-22T14:06:34+08:00", "2026-07-22T14:08:34+08:00"},
		{"sub-second reset", "2026-07-22T14:06:34.325+08:00", "2026-07-22T14:08:34+08:00"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit := ZaiRateLimitError{Limit: ZaiFiveHourLimit{ResetAtRFC3339: c.resetAt}}
			available, at := limit.AutoResumeSchedule()
			if !available {
				t.Fatal("expected auto resume schedule")
			}
			if at != c.wantResumed {
				t.Fatalf("auto resume at = %q want %q", at, c.wantResumed)
			}
			if strings.Contains(at, ".") {
				t.Fatalf("auto resume at must stay second precision: %q", at)
			}
			parsed, err := time.Parse(time.RFC3339, at)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.UnixMilli()%1000 != 0 {
				t.Fatalf("auto resume at must align with a whole-second next_run_at: %q", at)
			}
		})
	}
}

func TestDetectZaiFiveHourLimitRejectsGeneric429(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.log")
	if err := os.WriteFile(path, []byte("API Error: Request rejected (429)\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok := DetectZaiFiveHourLimit(path); ok {
		t.Fatal("generic 429 must not be treated as Z.ai 5h limit")
	}
}

func TestDetectZaiFiveHourLimitRejectsDifferentCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claude.log")
	content := "API Error: Request rejected (429) · [9999][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34]\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, ok := DetectZaiFiveHourLimit(path); ok {
		t.Fatal("different Z.ai error code must not be treated as 5h limit")
	}
}
