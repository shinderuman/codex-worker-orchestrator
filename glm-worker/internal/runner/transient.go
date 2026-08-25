package runner

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
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

type ProviderFailureClass struct {
	Kind          string
	Detail        string
	FiveHourLimit ZaiFiveHourLimit
}

const (
	ProviderFailureZaiFiveHour = "zai-5h"
	ProviderFailureTransient   = "transient"
	ProviderFailureFatal       = "fatal"

	ProbeContractFailure = "probe-contract"
)

func ClassifyProviderFailureText(text string) ProviderFailureClass {
	if limit, ok := DetectZaiFiveHourLimitText(text); ok {
		return ProviderFailureClass{Kind: ProviderFailureZaiFiveHour, FiveHourLimit: limit}
	}
	if classification, transient := ClassifyTransientFailure(text); transient {
		return ProviderFailureClass{Kind: ProviderFailureTransient, Detail: classification}
	}
	return ProviderFailureClass{Kind: ProviderFailureFatal}
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

func (e *ProviderUnavailableError) Error() string {
	return fmt.Sprintf(
		"provider stayed unavailable after %d probes (classification %s) at phase %s; task stopped, resumable via glm-worker --resume",
		e.Probes,
		e.Classification,
		e.Phase,
	)
}
