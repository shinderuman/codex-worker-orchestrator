package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
)

const codexLimitFiveHourPrimaryScript = "#!/bin/sh\n" +
	"printf '%s\\n' '{\"id\":1,\"result\":{\"userAgent\":\"glm-worker/0.149.1\"}}'\n" +
	"printf '%s\\n' '{\"id\":2,\"result\":{\"rateLimits\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":100,\"windowDurationMins\":300,\"resetsAt\":1787685137},\"secondary\":{\"usedPercent\":16,\"windowDurationMins\":10080,\"resetsAt\":1788271937},\"planType\":\"plus\",\"rateLimitReachedType\":\"rate_limit_reached\"}}}'\n"

const codexLimitFiveHourSecondaryScript = "#!/bin/sh\n" +
	"printf '%s\\n' '{\"id\":1,\"result\":{\"userAgent\":\"glm-worker/0.149.1\"}}'\n" +
	"printf '%s\\n' '{\"id\":2,\"result\":{\"rateLimits\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":25,\"windowDurationMins\":10080,\"resetsAt\":1788271937},\"secondary\":{\"usedPercent\":60,\"windowDurationMins\":300,\"resetsAt\":1787703163},\"planType\":\"plus\",\"rateLimitReachedType\":null}}}'\n"

const codexLimitNoFiveHourScript = "#!/bin/sh\n" +
	"printf '%s\\n' '{\"id\":1,\"result\":{\"userAgent\":\"glm-worker/0.149.1\"}}'\n" +
	"printf '%s\\n' '{\"id\":2,\"result\":{\"rateLimits\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":10,\"windowDurationMins\":10080,\"resetsAt\":1788271937},\"planType\":\"plus\"}}}'\n"

func writeCodexLimitFake(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-fake")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func executeCodexLimit(t *testing.T, bin string) (string, error) {
	t.Helper()
	cfg := newAppConfig(t)
	cfg.CodexBin = bin
	var out bytes.Buffer
	err := Execute(Command{Mode: ModeCodexLimit}, cfg, nil, &out, io.Discard)
	return out.String(), err
}

func TestExecuteCodexLimitMachineJSONContract(t *testing.T) {
	t.Run("primary位置の5h window", func(t *testing.T) {
		rendered, err := executeCodexLimit(t, writeCodexLimitFake(t, codexLimitFiveHourPrimaryScript))
		if err != nil {
			t.Fatal(err)
		}

		decoded := decodeSingleLineJSON(t, rendered)
		fiveHour, ok := decoded["five_hour"].(map[string]any)
		if !ok {
			t.Fatalf("five_hourがJSON objectではありません: %#v", decoded["five_hour"])
		}
		if fiveHour["window_duration_mins"] != float64(300) {
			t.Fatalf("five_hour.window_duration_mins = %#v", fiveHour["window_duration_mins"])
		}
		if fiveHour["used_percent"] != float64(100) {
			t.Fatalf("five_hour.used_percent = %#v", fiveHour["used_percent"])
		}
		if fiveHour["resets_at"] != float64(1787685137) {
			t.Fatalf("five_hour.resets_at = %#v", fiveHour["resets_at"])
		}
		if fiveHour["resets_at_rfc3339"] != "2026-08-25T19:12:17Z" {
			t.Fatalf("five_hour.resets_at_rfc3339 = %#v", fiveHour["resets_at_rfc3339"])
		}
		weekly, ok := decoded["weekly"].(map[string]any)
		if !ok || weekly["window_duration_mins"] != float64(10080) {
			t.Fatalf("weekly = %#v", decoded["weekly"])
		}
		if decoded["plan_type"] != "plus" {
			t.Fatalf("plan_type = %#v", decoded["plan_type"])
		}
		if decoded["rate_limit_reached_type"] != "rate_limit_reached" {
			t.Fatalf("rate_limit_reached_type = %#v", decoded["rate_limit_reached_type"])
		}
		assertNoPresentationSentinel(t, decoded, "plan_type", "rate_limit_reached_type")
		assertNoPresentationSentinel(t, fiveHour, "resets_at_rfc3339")
	})

	t.Run("secondary位置の5h window", func(t *testing.T) {
		rendered, err := executeCodexLimit(t, writeCodexLimitFake(t, codexLimitFiveHourSecondaryScript))
		if err != nil {
			t.Fatal(err)
		}

		decoded := decodeSingleLineJSON(t, rendered)
		fiveHour, ok := decoded["five_hour"].(map[string]any)
		if !ok {
			t.Fatalf("five_hourがJSON objectではありません: %#v", decoded["five_hour"])
		}
		if fiveHour["window_duration_mins"] != float64(300) {
			t.Fatalf("five_hour.window_duration_mins = %#v", fiveHour["window_duration_mins"])
		}
		if fiveHour["resets_at"] != float64(1787703163) {
			t.Fatalf("five_hour.resets_at = %#v", fiveHour["resets_at"])
		}
		weekly, ok := decoded["weekly"].(map[string]any)
		if !ok || weekly["resets_at"] != float64(1788271937) {
			t.Fatalf("weekly = %#v", decoded["weekly"])
		}
	})

	t.Run("weekly不在はnull", func(t *testing.T) {
		rendered, err := executeCodexLimit(t, writeCodexLimitFake(t, codexLimitFiveHourSecondaryScript))
		if err != nil {
			t.Fatal(err)
		}
		decoded := decodeSingleLineJSON(t, rendered)
		if _, exists := decoded["weekly"]; !exists {
			t.Fatalf("weekly keyが省略されています: %v", decoded)
		}
	})
}

func TestExecuteCodexLimitFailClosedAsProcessError(t *testing.T) {
	_, err := executeCodexLimit(t, writeCodexLimitFake(t, codexLimitNoFiveHourScript))
	if err == nil {
		t.Fatal("errorなし")
	}
	var codexLimitErr *CodexLimitError
	if !errors.As(err, &codexLimitErr) {
		t.Fatalf("error = %#v want *CodexLimitError", err)
	}
	if codexLimitErr.Phase != "window_selection" {
		t.Fatalf("phase = %q", codexLimitErr.Phase)
	}

	var stderr bytes.Buffer
	if writeErr := WriteProcessError(&stderr, err); writeErr != nil {
		t.Fatal(writeErr)
	}
	decoded := decodeSingleLineJSON(t, stderr.String())
	errorBody, ok := decoded["error"].(map[string]any)
	if !ok {
		t.Fatalf("error envelopeがありません: %v", decoded)
	}
	if errorBody["kind"] != "codex_limit_unavailable" {
		t.Fatalf("kind = %#v", errorBody["kind"])
	}
	detail, ok := errorBody["detail"].(map[string]any)
	if !ok || detail["phase"] != "window_selection" {
		t.Fatalf("detail = %#v", errorBody["detail"])
	}
}

func TestExecuteCodexLimitBinaryLookupFailure(t *testing.T) {
	_, err := executeCodexLimit(t, filepath.Join(t.TempDir(), "codex-absent"))
	if err == nil {
		t.Fatal("errorなし")
	}
	if !strings.Contains(err.Error(), "codex binary not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestCodexLimitPhaseMapping(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{codexlimit.ErrCodexBinaryNotFound, "codex_binary"},
		{codexlimit.ErrAppServerStart, "app_server_start"},
		{codexlimit.ErrAppServerTimeout, "app_server_timeout"},
		{codexlimit.ErrAppServerProtocol, "app_server_protocol"},
		{codexlimit.ErrRateLimitsRead, "rate_limits_read"},
		{codexlimit.ErrFiveHourWindowMissing, "window_selection"},
		{codexlimit.ErrFiveHourResetsAtMissing, "window_selection"},
		{codexlimit.ErrWindowAmbiguous, "window_selection"},
		{errors.New("別の障害"), "internal"},
	}
	for _, test := range tests {
		if got := codexLimitPhase(test.err); got != test.want {
			t.Fatalf("codexLimitPhase(%v) = %q want %q", test.err, got, test.want)
		}
	}
}

func TestParseCommandCodexLimit(t *testing.T) {
	command, err := ParseCommand([]string{"--codex-limit"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeCodexLimit {
		t.Fatalf("Mode = %d", command.Mode)
	}
	for _, args := range [][]string{
		{"--codex-limit", "extra"},
		{"--codex-limit", "--verbose"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid argsを受理しました: %#v", args)
		}
	}
}
