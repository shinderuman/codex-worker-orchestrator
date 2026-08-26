package codexlimit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const initializeResponseLine = `{"id":1,"result":{"userAgent":"glm-worker/0.149.1","codexHome":"/tmp","platformFamily":"unix","platformOs":"macos"}}`

const statusNotificationLine = `{"method":"remoteControl/status/changed","params":{"status":"disabled"},"emittedAtMs":1787687070389}`

const primaryFiveHourRateLimits = `{"id":2,"result":{"rateLimits":{"limitId":"codex","limitName":null,"primary":{"usedPercent":100,"windowDurationMins":300,"resetsAt":1787685137},"secondary":{"usedPercent":16,"windowDurationMins":10080,"resetsAt":1788271937},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"individualLimit":null,"spendControlReached":false,"planType":"plus","rateLimitReachedType":"rate_limit_reached"},"rateLimitsByLimitId":{"codex":{"limitId":"codex","primary":{"usedPercent":100,"windowDurationMins":300,"resetsAt":1787685137},"secondary":{"usedPercent":16,"windowDurationMins":10080,"resetsAt":1788271937},"planType":"plus","rateLimitReachedType":"rate_limit_reached"}},"rateLimitResetCredits":{"availableCount":0,"credits":[]}}}`

const secondaryFiveHourRateLimits = `{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":25,"windowDurationMins":10080,"resetsAt":1788271937},"secondary":{"usedPercent":60,"windowDurationMins":300,"resetsAt":1787703163},"planType":"plus","rateLimitReachedType":null}}}`

func writeFakeCodex(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "codex-fake")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func fakeServerScript(lines ...string) string {
	readLine := "IFS= read -r line\n"
	script := "#!/bin/sh\n" + readLine
	rest := lines
	if len(rest) > 0 {
		script += "printf '%s\\n' '" + rest[0] + "'\n"
		rest = rest[1:]
	}
	script += readLine + readLine
	for _, line := range rest {
		script += "printf '%s\\n' '" + line + "'\n"
	}
	return script + readLine
}

func TestReadSelectsFiveHourFromPrimaryPosition(t *testing.T) {
	bin := writeFakeCodex(t, fakeServerScript(initializeResponseLine, statusNotificationLine, primaryFiveHourRateLimits))

	snapshot, err := ReadWithTimeout(bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	fiveHour := snapshot.FiveHour
	if fiveHour.WindowDurationMins != 300 {
		t.Fatalf("five_hour.window_duration_mins = %d", fiveHour.WindowDurationMins)
	}
	if fiveHour.UsedPercent == nil || *fiveHour.UsedPercent != 100 {
		t.Fatalf("five_hour.used_percent = %#v", fiveHour.UsedPercent)
	}
	if fiveHour.ResetsAt == nil || *fiveHour.ResetsAt != 1787685137 {
		t.Fatalf("five_hour.resets_at = %#v", fiveHour.ResetsAt)
	}
	if fiveHour.ResetsAtRFC3339 == nil || *fiveHour.ResetsAtRFC3339 != "2026-08-25T19:12:17Z" {
		t.Fatalf("five_hour.resets_at_rfc3339 = %#v", fiveHour.ResetsAtRFC3339)
	}
	if snapshot.Weekly == nil || snapshot.Weekly.WindowDurationMins != 10080 || snapshot.Weekly.ResetsAt == nil || *snapshot.Weekly.ResetsAt != 1788271937 {
		t.Fatalf("weekly = %#v", snapshot.Weekly)
	}
	if snapshot.PlanType == nil || *snapshot.PlanType != "plus" {
		t.Fatalf("plan_type = %#v", snapshot.PlanType)
	}
	if snapshot.RateLimitReachedType == nil || *snapshot.RateLimitReachedType != "rate_limit_reached" {
		t.Fatalf("rate_limit_reached_type = %#v", snapshot.RateLimitReachedType)
	}
}

func TestReadSelectsFiveHourFromSecondaryPosition(t *testing.T) {
	bin := writeFakeCodex(t, fakeServerScript(initializeResponseLine, secondaryFiveHourRateLimits))

	snapshot, err := ReadWithTimeout(bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.FiveHour.ResetsAt == nil || *snapshot.FiveHour.ResetsAt != 1787703163 {
		t.Fatalf("five_hour.resets_at = %#v", snapshot.FiveHour.ResetsAt)
	}
	if snapshot.FiveHour.UsedPercent == nil || *snapshot.FiveHour.UsedPercent != 60 {
		t.Fatalf("five_hour.used_percent = %#v", snapshot.FiveHour.UsedPercent)
	}
	if snapshot.Weekly == nil || snapshot.Weekly.ResetsAt == nil || *snapshot.Weekly.ResetsAt != 1788271937 {
		t.Fatalf("weekly = %#v", snapshot.Weekly)
	}
}

func TestReadFallsBackToRateLimitsByLimitID(t *testing.T) {
	byLimitID := `{"id":2,"result":{"rateLimitsByLimitId":{"codex":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1787685137},"secondary":{"usedPercent":5,"windowDurationMins":10080,"resetsAt":1788271937},"planType":"plus","rateLimitReachedType":null}}}}`
	bin := writeFakeCodex(t, fakeServerScript(initializeResponseLine, byLimitID))

	snapshot, err := ReadWithTimeout(bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.FiveHour.ResetsAt == nil || *snapshot.FiveHour.ResetsAt != 1787685137 {
		t.Fatalf("five_hour.resets_at = %#v", snapshot.FiveHour.ResetsAt)
	}
	if snapshot.PlanType == nil || *snapshot.PlanType != "plus" {
		t.Fatalf("plan_type = %#v", snapshot.PlanType)
	}
}

func TestReadWeeklyAbsentStaysNull(t *testing.T) {
	weeklyAbsent := `{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1787685137},"secondary":null,"planType":"plus","rateLimitReachedType":null}}}`
	bin := writeFakeCodex(t, fakeServerScript(initializeResponseLine, weeklyAbsent))

	snapshot, err := ReadWithTimeout(bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}

	if snapshot.Weekly != nil {
		t.Fatalf("weekly = %#v want nil", snapshot.Weekly)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"weekly":null`) {
		t.Fatalf("weeklyがnullとして直列化されていません: %s", encoded)
	}
}

func TestReadFailures(t *testing.T) {
	tests := []struct {
		name       string
		script     string
		wantErr    error
		wantInMess string
	}{
		{
			name:       "300分windowなし",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":10080,"resetsAt":1788271937}}}}`),
			wantErr:    ErrFiveHourWindowMissing,
			wantInMess: "",
		},
		{
			name:       "300分windowが重複",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"windowDurationMins":300,"resetsAt":1787685137},"secondary":{"windowDurationMins":300,"resetsAt":1787685138}}}}`),
			wantErr:    ErrWindowAmbiguous,
			wantInMess: "300",
		},
		{
			name:       "300分windowのresetsAtなし",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"result":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300},"secondary":{"windowDurationMins":10080,"resetsAt":1788271937}}}}`),
			wantErr:    ErrFiveHourResetsAtMissing,
			wantInMess: "",
		},
		{
			name:       "codex limit entryなし",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"result":{"rateLimitsByLimitId":{}}}`),
			wantErr:    ErrRateLimitsRead,
			wantInMess: "no codex limit entry",
		},
		{
			name:       "rateLimits/readがrpc error",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"error":{"code":-32000,"message":"not logged in"}}`),
			wantErr:    ErrRateLimitsRead,
			wantInMess: "not logged in",
		},
		{
			name:       "initializeがrpc error",
			script:     fakeServerScript(`{"id":1,"error":{"code":-32000,"message":"unsupported protocol"}}`),
			wantErr:    ErrAppServerProtocol,
			wantInMess: "initialize error",
		},
		{
			name:       "id 2のresultなし",
			script:     fakeServerScript(initializeResponseLine, `{"id":2}`),
			wantErr:    ErrRateLimitsRead,
			wantInMess: "no result",
		},
		{
			name:       "resultがobject以外",
			script:     fakeServerScript(initializeResponseLine, `{"id":2,"result":"scalar"}`),
			wantErr:    ErrRateLimitsRead,
			wantInMess: "decode",
		},
		{
			name:       "応答前にserver終了",
			script:     "#!/bin/sh\nexit 0\n",
			wantErr:    ErrAppServerProtocol,
			wantInMess: "response stream ended",
		},
		{
			name:       "非JSON行",
			script:     fakeServerScript("not json"),
			wantErr:    ErrAppServerProtocol,
			wantInMess: "malformed line",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bin := writeFakeCodex(t, test.script)
			_, err := ReadWithTimeout(bin, 10*time.Second)
			if err == nil {
				t.Fatal("errorなし")
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v want %v", err, test.wantErr)
			}
			if test.wantInMess != "" && !strings.Contains(err.Error(), test.wantInMess) {
				t.Fatalf("error %q に %q が含まれていません", err.Error(), test.wantInMess)
			}
		})
	}
}

func TestReadSendsSequentialHandshakeInContractOrder(t *testing.T) {
	dir := t.TempDir()
	recordPath := filepath.Join(dir, "requests")
	script := "#!/bin/sh\n" +
		"IFS= read -r line\n" +
		"printf '%s\\n' \"$line\" >'" + recordPath + "'\n" +
		"printf '%s\\n' '" + initializeResponseLine + "'\n" +
		"IFS= read -r line\n" +
		"printf '%s\\n' \"$line\" >>'" + recordPath + "'\n" +
		"IFS= read -r line\n" +
		"printf '%s\\n' \"$line\" >>'" + recordPath + "'\n" +
		"printf '%s\\n' '" + primaryFiveHourRateLimits + "'\n" +
		"IFS= read -r line\n"
	bin := writeFakeCodex(t, script)

	snapshot, err := ReadWithTimeout(bin, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FiveHour.UsedPercent == nil || *snapshot.FiveHour.UsedPercent != 100 {
		t.Fatalf("five_hour.used_percent = %#v", snapshot.FiveHour.UsedPercent)
	}
	requests := readRecordLines(t, recordPath)
	if len(requests) != 3 {
		t.Fatalf("request数 = %d want 3: %#v", len(requests), requests)
	}
	if !strings.Contains(requests[0], `"method":"initialize"`) || !strings.Contains(requests[0], `"id":1`) {
		t.Fatalf("1番目のrequest = %s want initialize", requests[0])
	}
	if requests[1] != `{"jsonrpc":"2.0","method":"initialized"}` {
		t.Fatalf("2番目のrequest = %s want initialized notification", requests[1])
	}
	if !strings.Contains(requests[2], `"method":"account/rateLimits/read"`) || !strings.Contains(requests[2], `"id":2`) {
		t.Fatalf("3番目のrequest = %s want account/rateLimits/read", requests[2])
	}
}

func TestExchangeWaitsForInitializeResponseBeforeSendingInitialized(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	type handshakeOutcome struct {
		requests   []string
		violations []string
	}
	done := make(chan handshakeOutcome, 1)
	go func() {
		outcome := handshakeOutcome{}
		reader := bufio.NewReader(server)
		if err := server.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			outcome.violations = append(outcome.violations, err.Error())
			done <- outcome
			return
		}
		first, err := reader.ReadString('\n')
		if err != nil {
			outcome.violations = append(outcome.violations, "initialize requestを受ける前にread error: "+err.Error())
			done <- outcome
			return
		}
		outcome.requests = append(outcome.requests, strings.TrimSpace(first))
		if err := server.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
			outcome.violations = append(outcome.violations, err.Error())
			done <- outcome
			return
		}
		if _, err := server.Read(make([]byte, 1)); err == nil {
			outcome.violations = append(outcome.violations, "initialize response前に次のrequestが到着しています")
			done <- outcome
			return
		}
		if err := server.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			outcome.violations = append(outcome.violations, err.Error())
			done <- outcome
			return
		}
		if _, err := server.Write([]byte(initializeResponseLine + "\n")); err != nil {
			outcome.violations = append(outcome.violations, "initialize responseを書けません: "+err.Error())
			done <- outcome
			return
		}
		second, err := reader.ReadString('\n')
		if err != nil {
			outcome.violations = append(outcome.violations, "initialized待ちでread error: "+err.Error())
			done <- outcome
			return
		}
		outcome.requests = append(outcome.requests, strings.TrimSpace(second))
		third, err := reader.ReadString('\n')
		if err != nil {
			outcome.violations = append(outcome.violations, "rateLimits/read待ちでread error: "+err.Error())
			done <- outcome
			return
		}
		outcome.requests = append(outcome.requests, strings.TrimSpace(third))
		if _, err := server.Write([]byte(primaryFiveHourRateLimits + "\n")); err != nil {
			outcome.violations = append(outcome.violations, "rateLimits responseを書けません: "+err.Error())
		}
		done <- outcome
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := exchange(ctx, client, client)
	if err != nil {
		t.Fatal(err)
	}
	outcome := <-done
	if len(outcome.violations) != 0 {
		t.Fatalf("handshake違反: %#v", outcome.violations)
	}
	if len(outcome.requests) != 3 {
		t.Fatalf("request数 = %d want 3: %#v", len(outcome.requests), outcome.requests)
	}
	if !strings.Contains(outcome.requests[0], `"method":"initialize"`) {
		t.Fatalf("1番目のrequest = %s", outcome.requests[0])
	}
	if !strings.Contains(outcome.requests[1], `"method":"initialized"`) {
		t.Fatalf("2番目のrequest = %s", outcome.requests[1])
	}
	if !strings.Contains(outcome.requests[2], `"method":"account/rateLimits/read"`) {
		t.Fatalf("3番目のrequest = %s", outcome.requests[2])
	}
	if result == nil {
		t.Fatal("rateLimits resultがnil")
	}
}

func readRecordLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("record読み込み: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func TestReadBinaryNotFound(t *testing.T) {
	_, err := ReadWithTimeout(filepath.Join(t.TempDir(), "codex-absent"), 10*time.Second)
	if !errors.Is(err, ErrCodexBinaryNotFound) {
		t.Fatalf("error = %v want ErrCodexBinaryNotFound", err)
	}
}

func TestReadStartFailure(t *testing.T) {
	bin := writeFakeCodex(t, "#!/nonexistent-interpreter\n")

	_, err := ReadWithTimeout(bin, 10*time.Second)
	if !errors.Is(err, ErrAppServerStart) {
		t.Fatalf("error = %v want ErrAppServerStart", err)
	}
}

func TestReadTimeoutOnSilentServer(t *testing.T) {
	bin := writeFakeCodex(t, "#!/bin/sh\nsleep 30\n")

	started := time.Now()
	_, err := ReadWithTimeout(bin, 300*time.Millisecond)
	if !errors.Is(err, ErrAppServerTimeout) {
		t.Fatalf("error = %v want ErrAppServerTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("timeoutまで %s かかっています", elapsed)
	}
}

func TestReadTerminatesServerAfterResponse(t *testing.T) {
	script := fakeServerScript(initializeResponseLine, primaryFiveHourRateLimits) + "sleep 30\n"
	bin := writeFakeCodex(t, script)

	started := time.Now()
	snapshot, err := ReadWithTimeout(bin, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FiveHour.ResetsAt == nil {
		t.Fatalf("five_hour.resets_at = nil")
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("応答後 %s 待機しています", elapsed)
	}
}

func TestReadLiveCodexAppServer(t *testing.T) {
	if os.Getenv("GLM_WORKER_CODEX_LIMIT_LIVE") != "1" {
		t.Skip("GLM_WORKER_CODEX_LIMIT_LIVE=1のときだけ実app-serverへ接続する")
	}

	snapshot, err := Read("codex")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.FiveHour.WindowDurationMins != 300 {
		t.Fatalf("five_hour.window_duration_mins = %d", snapshot.FiveHour.WindowDurationMins)
	}
	if snapshot.FiveHour.ResetsAt == nil {
		t.Fatalf("five_hour.resets_at = nil")
	}
	if snapshot.FiveHour.ResetsAtRFC3339 == nil {
		t.Fatalf("five_hour.resets_at_rfc3339 = nil")
	}
}
