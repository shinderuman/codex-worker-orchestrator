package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

type ProviderFailureClass struct {
	Kind          string
	Detail        string
	BusinessCode  string
	FiveHourLimit ZaiFiveHourLimit
}

type ProviderUnavailableError struct {
	Phase          string
	Classification string
	Probes         int
	Elapsed        time.Duration
	TaskID         string
	RepoRoot       string
	RepoShort      string
}

const (
	ProviderFailureZaiFiveHour      = "zai-5h"
	ProviderFailureZaiLongQuota     = "zai-long-quota"
	ProviderFailureZaiActionRequired = "zai-action-required"
	ProviderFailureTransient        = "transient"
	ProviderFailureFatal            = "fatal"

	ProbeContractFailure = "probe-contract"
)

var transientHTTPPattern = regexp.MustCompile(`\b(502|503|504|529)\b`)

var transientNetworkSignals = []string{
	"connection refused",
	"connection reset",
	"i/o timeout",
	"context deadline exceeded",
	"dial tcp",
	"no such host",
	"network is unreachable",
	"transport is closing",
	"unexpected eof",
	"temporary failure",
	"server closed idle connection",
	"proxyconnect",
}

var zaiTransientBusinessCodes = map[string]struct{}{
	"1302": {}, // request rate limit
	"1305": {}, // temporary overload
}

var zaiLongQuotaBusinessCodes = map[string]struct{}{
	"1310": {}, // weekly/monthly quota exhausted
	"1317": {}, // seven-day limit, no balance for extra usage
	"1319": {}, // seven-day limit, no monthly spend available
	"1321": {}, // seven-day limit, monthly spend cap reached
}

var zaiActionRequiredBusinessCodes = map[string]struct{}{
	"1113": {}, // insufficient balance or no resource package
	"1309": {}, // Coding Plan expired
	"1311": {}, // plan does not include requested model
	"1313": {}, // fair-use restriction
	"1314": {}, // enterprise package expired
	"1315": {}, // key restricted to enterprise scenarios
}

var probeFatalHTTPPattern = regexp.MustCompile(`(?i)\b(?:http|status|error|api)[^\n]{0,24}\b(?:400|401|403)\b|\b(?:400|401|403)\b[^\n]{0,24}\b(?:bad request|unauthorized|forbidden)\b`)

var probeFatalSignals = []string{
	"invalid api key",
	"invalid_api_key",
	"invalid x-api-key",
	"api key not valid",
	"authentication failed",
	"authentication required",
	"permission denied",
	"invalid model",
	"model not found",
}

func DetectProbeFatalSignal(text string) bool {
	if probeFatalHTTPPattern.MatchString(text) {
		return true
	}
	lower := strings.ToLower(text)
	for _, signal := range probeFatalSignals {
		if strings.Contains(lower, signal) {
			return true
		}
	}
	return false
}

func ClassifyTransientFailure(text string) (classification string, transient bool) {
	if match := transientHTTPPattern.FindString(text); match != "" {
		return "http-" + match, true
	}
	for _, signal := range transientNetworkSignals {
		if strings.Contains(strings.ToLower(text), signal) {
			return "network:" + signal, true
		}
	}
	return "", false
}

func ReadTransientSignal(outputPath string) string {
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return ""
	}
	return string(data)
}

func ClassifyProviderFailureText(text string) ProviderFailureClass {
	if code, ok := DetectZaiBusinessCodeText(text); ok {
		if _, transient := zaiTransientBusinessCodes[code]; transient {
			return ProviderFailureClass{
				Kind:         ProviderFailureTransient,
				Detail:       "zai-code:" + code,
				BusinessCode: code,
			}
		}
		if _, fiveHour := zaiFiveHourBusinessCodes[code]; fiveHour {
			limit, _ := DetectZaiFiveHourLimitText(text)
			return ProviderFailureClass{
				Kind:          ProviderFailureZaiFiveHour,
				Detail:        "zai-code:" + code,
				BusinessCode:  code,
				FiveHourLimit: limit,
			}
		}
		if _, longQuota := zaiLongQuotaBusinessCodes[code]; longQuota {
			return ProviderFailureClass{
				Kind:         ProviderFailureZaiLongQuota,
				Detail:       "zai-code:" + code,
				BusinessCode: code,
			}
		}
		if _, actionRequired := zaiActionRequiredBusinessCodes[code]; actionRequired {
			return ProviderFailureClass{
				Kind:         ProviderFailureZaiActionRequired,
				Detail:       "zai-code:" + code,
				BusinessCode: code,
			}
		}
		return ProviderFailureClass{
			Kind:         ProviderFailureFatal,
			Detail:       "zai-code:" + code,
			BusinessCode: code,
		}
	}
	if classification, transient := ClassifyTransientFailure(text); transient {
		return ProviderFailureClass{Kind: ProviderFailureTransient, Detail: classification}
	}
	return ProviderFailureClass{Kind: ProviderFailureFatal}
}

func (e *ProviderUnavailableError) Error() string {
	return fmt.Sprintf(
		"provider stayed unavailable after %d probes (classification %s) at phase %s; task stopped, resumable via glm-worker --resume",
		e.Probes,
		e.Classification,
		e.Phase,
	)
}
