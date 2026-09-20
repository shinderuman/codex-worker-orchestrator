package runner

import (
	"fmt"
	"regexp"
	"time"
)

type ZaiFiveHourLimit struct {
	ResetAtCST     string
	ResetAtRFC3339 string
}

type ZaiRateLimitError struct {
	Phase           string
	Limit           ZaiFiveHourLimit
	TaskID          string
	RepoRoot        string
	RepoShort       string
	ArtifactWarning string
}

const autoResumeGrace = 2 * time.Minute

var (
	zaiBracketedBusinessCodePattern = regexp.MustCompile(`\[([0-9]{4})\]`)
	zaiJSONBusinessCodePattern      = regexp.MustCompile(`(?i)"code"\s*:\s*"?([0-9]{4})"?`)
	zaiResetPattern                 = regexp.MustCompile(`\b([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})\b`)
)

var zaiFiveHourBusinessCodes = map[string]struct{}{
	"1308": {},
	"1316": {},
	"1318": {},
	"1320": {},
}

func DetectZaiBusinessCodeText(output string) (string, bool) {
	for _, pattern := range []*regexp.Regexp{zaiJSONBusinessCodePattern, zaiBracketedBusinessCodePattern} {
		match := pattern.FindStringSubmatch(output)
		if len(match) == 2 {
			return match[1], true
		}
	}
	return "", false
}

func DetectZaiFiveHourLimitText(output string) (ZaiFiveHourLimit, bool) {
	code, ok := DetectZaiBusinessCodeText(output)
	if !ok {
		return ZaiFiveHourLimit{}, false
	}
	if _, fiveHour := zaiFiveHourBusinessCodes[code]; !fiveHour {
		return ZaiFiveHourLimit{}, false
	}

	limit := ZaiFiveHourLimit{}
	match := zaiResetPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return limit, true
	}

	chinaStandardTime := time.FixedZone("CST", 8*60*60)
	resetAt, err := time.ParseInLocation(
		"2006-01-02 15:04:05",
		match[1],
		chinaStandardTime,
	)
	if err == nil {
		limit.ResetAtRFC3339 = resetAt.Format(time.RFC3339)
		limit.ResetAtCST = FormatZaiResetAtCST(limit.ResetAtRFC3339)
	}

	return limit, true
}

func FormatZaiResetAtCST(resetAtRFC3339 string) string {
	if resetAtRFC3339 == "" {
		return ""
	}
	resetAt, err := time.Parse(time.RFC3339, resetAtRFC3339)
	if err != nil {
		return ""
	}
	chinaStandardTime := time.FixedZone("CST", 8*60*60)
	return resetAt.In(chinaStandardTime).Format("2006-01-02 15:04:05")
}

func (e ZaiRateLimitError) Error() string {
	return fmt.Sprintf("Z.ai Coding Plan 5h limit reached at phase %s; task stopped, resumable via glm-worker --resume", e.Phase)
}

func (e ZaiRateLimitError) AutoResumeSchedule() (bool, string) {
	return AutoResumeAtFromReset(e.Limit.ResetAtRFC3339)
}

func (e ZaiRateLimitError) AutoResumeKey() string {
	return AutoResumeKeyFor(e.RepoShort, e.TaskID)
}

func AutoResumeAtFromReset(resetAtRFC3339 string) (bool, string) {
	return autoResumeSchedule(resetAtRFC3339)
}

func AutoResumeKeyFor(repoShort string, taskID string) string {
	return autoResumeKey(repoShort, taskID)
}

func autoResumeSchedule(resetAtRFC3339 string) (bool, string) {
	resetAt, err := time.Parse(time.RFC3339, resetAtRFC3339)
	if err != nil {
		return false, "unknown"
	}
	return true, resetAt.Add(autoResumeGrace).Format(time.RFC3339)
}

func autoResumeKey(repoShort string, taskID string) string {
	if repoShort == "" {
		repoShort = "unknown-repo"
	}
	if taskID == "" {
		taskID = "unknown-task"
	} else if len(taskID) > 8 {
		taskID = taskID[:8]
	}
	return fmt.Sprintf("glm-worker-resume-%s-%s", repoShort, taskID)
}
