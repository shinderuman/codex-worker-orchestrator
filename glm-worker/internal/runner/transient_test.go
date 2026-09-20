package runner

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClassifyTransientFailureHTTPErrors(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{"502 gateway", "API Error: 502 Bad Gateway", "http-502"},
		{"503 service", "Request rejected (503)", "http-503"},
		{"504 timeout", "HTTP 504 Gateway Timeout", "http-504"},
		{"529 overloaded", "status code 529", "http-529"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, transient := ClassifyTransientFailure(tt.text)
			if !transient || class != tt.want {
				t.Fatalf("ClassifyTransientFailure(%q) = (%q, %v), want (%q, true)", tt.text, class, transient, tt.want)
			}
		})
	}
}

func TestClassifyTransientFailureDoesNotMatchPortLikeSubstrings(t *testing.T) {
	class, transient := ClassifyTransientFailure("connecting to localhost:5034 failed")
	if transient {
		t.Fatalf("port-like 5034 must not match: (%q, %v)", class, transient)
	}
}

func TestClassifyTransientFailureNetworkSignals(t *testing.T) {
	for _, text := range []string{
		"dial tcp: lookup api.z.ai: no such host",
		"connection refused",
		"context deadline exceeded",
		"read tcp: unexpected EOF",
	} {
		class, transient := ClassifyTransientFailure(text)
		if !transient || !strings.HasPrefix(class, "network:") {
			t.Fatalf("network信号をtransient扱いすべき: %q -> (%q, %v)", text, class, transient)
		}
	}
}

func TestClassifyTransientFailureRejectsNonTransient(t *testing.T) {
	for _, text := range []string{
		"401 Unauthorized",
		"403 Forbidden",
		"400 Bad Request: invalid_request_error",
		"invalid api key",
		"429 Too Many Requests",
		"API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-07-22 14:06:34]",
		"session corrupted",
		"",
	} {
		class, transient := ClassifyTransientFailure(text)
		if transient {
			t.Fatalf("非transientを誤検出: %q -> (%q, %v)", text, class, transient)
		}
	}
}

func TestReadTransientSignalMissingFile(t *testing.T) {
	if got := ReadTransientSignal("/nonexistent/transient-signal-xyz"); got != "" {
		t.Fatalf("欠損fileは空文字を期待: %q", got)
	}
}

func TestClassifyProviderFailureTextByZaiBusinessCode(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantKind   string
		wantCode   string
		wantDetail string
	}{
		{"request rate limit", `[1302][wording can change]`, ProviderFailureTransient, "1302", "zai-code:1302"},
		{"temporary overload", `{"error":{"code":1305,"message":"different wording"}}`, ProviderFailureTransient, "1305", "zai-code:1305"},
		{"observed five hour", `[1308][different wording][2026-07-22 14:06:34]`, ProviderFailureZaiFiveHour, "1308", "zai-code:1308"},
		{"five hour no balance", `[1316][different wording][2026-07-22 14:06:34]`, ProviderFailureZaiFiveHour, "1316", "zai-code:1316"},
		{"five hour no spend", `[1318][different wording][2026-07-22 14:06:34]`, ProviderFailureZaiFiveHour, "1318", "zai-code:1318"},
		{"five hour spend cap", `[1320][different wording][2026-07-22 14:06:34]`, ProviderFailureZaiFiveHour, "1320", "zai-code:1320"},
		{"weekly monthly quota", `[1310][different wording]`, ProviderFailureZaiLongQuota, "1310", "zai-code:1310"},
		{"seven day no balance", `[1317][different wording]`, ProviderFailureZaiLongQuota, "1317", "zai-code:1317"},
		{"seven day no spend", `[1319][different wording]`, ProviderFailureZaiLongQuota, "1319", "zai-code:1319"},
		{"seven day spend cap", `[1321][different wording]`, ProviderFailureZaiLongQuota, "1321", "zai-code:1321"},
		{"insufficient balance", `[1113][different wording]`, ProviderFailureZaiActionRequired, "1113", "zai-code:1113"},
		{"plan expired", `[1309][different wording]`, ProviderFailureZaiActionRequired, "1309", "zai-code:1309"},
		{"model excluded", `[1311][different wording]`, ProviderFailureZaiActionRequired, "1311", "zai-code:1311"},
		{"fair use", `[1313][different wording]`, ProviderFailureZaiActionRequired, "1313", "zai-code:1313"},
		{"enterprise expired", `[1314][different wording]`, ProviderFailureZaiActionRequired, "1314", "zai-code:1314"},
		{"enterprise key", `[1315][different wording]`, ProviderFailureZaiActionRequired, "1315", "zai-code:1315"},
		{"unknown business code", `[1999][unknown]`, ProviderFailureZaiUnknownSafeStop, "1999", "zai-code:1999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyProviderFailureText(tt.text)
			if got.Kind != tt.wantKind || got.BusinessCode != tt.wantCode || got.Detail != tt.wantDetail {
				t.Fatalf("ClassifyProviderFailureText(%q) = %#v, want kind=%q code=%q detail=%q", tt.text, got, tt.wantKind, tt.wantCode, tt.wantDetail)
			}
		})
	}
}

func TestClassifyProviderFailureTextFallbackSignals(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantKind   string
		wantDetail string
	}{
		{"http 502", "API Error: 502 Bad Gateway", ProviderFailureTransient, "http-502"},
		{"network dial", "dial tcp: lookup api.z.ai: no such host", ProviderFailureTransient, "network:dial tcp"},
		{"auth 401", "401 Unauthorized", ProviderFailureFatal, ""},
		{"unknown", "boom fatal", ProviderFailureFatal, ""},
		{"empty", "", ProviderFailureFatal, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClassifyProviderFailureText(tt.text)
			if got.Kind != tt.wantKind {
				t.Fatalf("ClassifyProviderFailureText(%q) kind = %q want %q", tt.text, got.Kind, tt.wantKind)
			}
			if got.Detail != tt.wantDetail {
				t.Fatalf("ClassifyProviderFailureText(%q) detail = %q want %q", tt.text, got.Detail, tt.wantDetail)
			}
			if got.BusinessCode != "" {
				t.Fatalf("non-Z.ai fallback must not invent business code: %#v", got)
			}
		})
	}
}

func TestProviderUnavailableErrorMessage(t *testing.T) {
	err := &ProviderUnavailableError{
		Phase:          "worker-new",
		Classification: "http-503",
		Probes:         4,
		Elapsed:        51 * time.Minute,
	}
	msg := err.Error()
	for _, want := range []string{"worker-new", "http-503", "4"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("ProviderUnavailableErrorのmessageに%qがありません:\n%s", want, msg)
		}
	}
	for _, forbidden := range []string{"STATUS:", "RATE_LIMITED", "AUTO_RESUME", "\n"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("provider-unavailableのmessageは単行machine前の旧形式 %qを含まない: %s", forbidden, msg)
		}
	}
}

func TestDetectProbeFatalSignal(t *testing.T) {
	fatal := []string{
		"401 Unauthorized",
		"403 Forbidden",
		"401 Unauthorized: invalid api key",
		"HTTP/1.1 403 Forbidden",
		"400 Bad Request: invalid_request_error",
		"API Error: 401 · invalid_api_key",
		"status code 403 returned",
		"invalid api key",
		"invalid x-api-key provided",
		"API key not valid. Please renew.",
		"authentication failed for this request",
		"authentication required before continuing",
		"permission denied for this credential",
		"invalid model: glm-unknown",
		"model not found: glm-unknown",
	}
	for _, text := range fatal {
		if !DetectProbeFatalSignal(text) {
			t.Fatalf("明示的auth/config信号を検出すべき: %q", text)
		}
	}
	notFatal := []string{
		"",
		"GLM_WORKER_PROBE_OK",
		"Scheduled maintenance is in progress. Please retry later.",
		"probe不正応答(opus): 応答がsentinel \"GLM_WORKER_PROBE_OK\"と一致しません: \"\"",
		"API Error: 503 Service Unavailable",
		"retry failed after waiting 400 ms",
		"queued 403 jobs behind the maintenance window",
		"authentication service unavailable",
		"connecting to localhost:4001 failed",
		"metrics: 4003 requests",
		"unauthorized",
		"Unauthorized",
		"forbidden",
		"Forbidden",
		"This request is unauthorized for the current account",
		"Access to this model is forbidden during maintenance",
		"the caller was unauthorized and forbidden from reading the repository",
	}
	for _, text := range notFatal {
		if DetectProbeFatalSignal(text) {
			t.Fatalf("semantic invalid・裸の数字・一般語をfatalへ誤分類: %q", text)
		}
	}
}

func TestProbeFatalSignalsEnumerationMatchesContract(t *testing.T) {
	bareWords := map[string]bool{
		"unauthorized":   true,
		"forbidden":      true,
		"authentication": true,
		"permission":     true,
	}
	for _, signal := range probeFatalSignals {
		if bareWords[signal] {
			t.Fatalf("probeFatalSignalsに文脈なし一般語%qを登録できない", signal)
		}
		if _, err := strconv.Atoi(strings.TrimSpace(signal)); err == nil {
			t.Fatalf("probeFatalSignalsに数字だけのsignal %qを登録できない", signal)
		}
		if !DetectProbeFatalSignal(signal) {
			t.Fatalf("列挙signal %qが実際にはfatal検出されない", signal)
		}
	}
}
