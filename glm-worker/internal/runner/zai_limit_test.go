package runner

import (
	"strings"
	"testing"
	"time"
)

func TestDetectZaiBusinessCodeText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"bracketed", "API Error · [1308][quota exhausted]", "1308"},
		{"json string", `429 {"error":{"code":"1316","message":"quota exhausted"}}`, "1316"},
		{"json number", `429 {"error":{"code":1305,"message":"busy"}}`, "1305"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DetectZaiBusinessCodeText(tt.text)
			if !ok || got != tt.want {
				t.Fatalf("DetectZaiBusinessCodeText(%q) = (%q, %v), want (%q, true)", tt.text, got, ok, tt.want)
			}
		})
	}
}

func TestDetectZaiFiveHourLimitText(t *testing.T) {
	content := "API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34][202607221342470f952f313a624fd3]\n"
	limit, ok := DetectZaiFiveHourLimitText(content)
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

func TestDetectZaiFiveHourLimitDoesNotDependOnEnglishMessage(t *testing.T) {
	for _, content := range []string{
		"[1308][任意の文言][2026-07-22 14:06:34]",
		`429 {"error":{"code":"1316","message":"wording changed completely; 2026-07-22 14:06:34"}}`,
		`429 {"error":{"code":"1318","message":"different producer text; 2026-07-22 14:06:34"}}`,
		`429 {"error":{"code":"1320","message":"another wording; 2026-07-22 14:06:34"}}`,
	} {
		limit, ok := DetectZaiFiveHourLimitText(content)
		if !ok {
			t.Fatalf("business-code 5h limit not detected: %q", content)
		}
		if limit.ResetAtRFC3339 != "2026-07-22T14:06:34+08:00" {
			t.Fatalf("reset = %q for %q", limit.ResetAtRFC3339, content)
		}
	}
}

func TestDetectZaiFiveHourLimitKeepsInvalidResetUnschedulable(t *testing.T) {
	content := "API Error: Request rejected (429) · [1308][quota][2026-99-99 14:06:34]\n"
	limit, ok := DetectZaiFiveHourLimitText(content)
	if !ok {
		t.Fatal("expected Z.ai 5h limit even when reset timestamp is invalid")
	}
	if limit.ResetAtRFC3339 != "" || limit.ResetAtCST != "" {
		t.Fatalf("invalid reset must not become schedulable/display authority: %#v", limit)
	}
	available, at := (ZaiRateLimitError{Limit: limit}).AutoResumeSchedule()
	if available || at != "unknown" {
		t.Fatalf("auto resume from invalid reset = available:%v at:%q", available, at)
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

func TestDetectZaiFiveHourLimitTextRejectsGeneric429(t *testing.T) {
	if _, ok := DetectZaiFiveHourLimitText("API Error: Request rejected (429)\n"); ok {
		t.Fatal("generic 429 must not be treated as Z.ai 5h limit")
	}
}

func TestDetectZaiFiveHourLimitTextRejectsLongQuotaCodes(t *testing.T) {
	for _, code := range []string{"1310", "1317", "1319", "1321"} {
		content := "API Error · [" + code + "][Usage limit reached for 5 hour.][2026-07-22 14:06:34]"
		if _, ok := DetectZaiFiveHourLimitText(content); ok {
			t.Fatalf("long quota code %s must not be treated as 5h limit", code)
		}
	}
}

func TestDetectZaiFiveHourLimitTextRejectsDifferentCode(t *testing.T) {
	content := "API Error: Request rejected (429) · [9999][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34]\n"
	if _, ok := DetectZaiFiveHourLimitText(content); ok {
		t.Fatal("different Z.ai error code must not be treated as 5h limit")
	}
}
