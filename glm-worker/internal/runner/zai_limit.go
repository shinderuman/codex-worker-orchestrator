package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"
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

const (
	zaiFiveHourLimitCode = "1308"
	zaiFiveHourMessage   = "Usage limit reached for 5 hour."
	autoResumeGrace      = 2 * time.Minute
)

var zaiResetPattern = regexp.MustCompile(`Your limit will reset at ([0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2})`)

func DetectZaiFiveHourLimit(path string) (ZaiFiveHourLimit, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ZaiFiveHourLimit{}, false
	}
	return DetectZaiFiveHourLimitText(string(data))
}

func DetectZaiFiveHourLimitText(output string) (ZaiFiveHourLimit, bool) {
	if !strings.Contains(output, "Request rejected (429)") {
		return ZaiFiveHourLimit{}, false
	}
	if !strings.Contains(output, "["+zaiFiveHourLimitCode+"]") {
		return ZaiFiveHourLimit{}, false
	}
	if !strings.Contains(output, zaiFiveHourMessage) {
		return ZaiFiveHourLimit{}, false
	}

	limit := ZaiFiveHourLimit{}
	match := zaiResetPattern.FindStringSubmatch(output)
	if len(match) != 2 {
		return limit, true
	}

	limit.ResetAtCST = match[1]

	chinaStandardTime := time.FixedZone("CST", 8*60*60)
	resetAt, err := time.ParseInLocation(
		"2006-01-02 15:04:05",
		limit.ResetAtCST,
		chinaStandardTime,
	)
	if err == nil {
		limit.ResetAtRFC3339 = resetAt.Format(time.RFC3339)
	}

	return limit, true
}

func (e ZaiRateLimitError) Error() string {
	return fmt.Sprintf("Z.ai Coding Plan 5h limit reached at phase %s; task stopped, resumable via glm-worker --resume", e.Phase)
}

func (e ZaiRateLimitError) AutoResumeSchedule() (bool, string) {
	return autoResumeSchedule(e.Limit.ResetAtRFC3339)
}

func (e ZaiRateLimitError) AutoResumeKey() string {
	return autoResumeKey(e.RepoShort, e.TaskID)
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
