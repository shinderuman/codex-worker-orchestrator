package runner

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const limitStopSignal = "API Error: Request rejected (429) · [1308][Usage limit reached for 5 hour. Your limit will reset at 2026-08-23 12:00:00]"

// limitStopHoldSecondsはsignal出力後も動き続けるfake childの保持時間。早期終了が働けば
// この待機の途中でRunが返るため、終了までの上限計測基準になる。
const limitStopHoldSeconds = 30

// limitStopElapsedBoundは早期終了ありきのRun復帰上限。killとpipe解放待ちの
// zaiLimitStopWaitを含んでもこの範囲に収まる。
const limitStopElapsedBound = 20 * time.Second

func writeLimitStopClaude(t *testing.T, script string) string {
	t.Helper()
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return commandPath
}

func TestZaiLimitStopperKillsOnceOnlyOnExactSignal(t *testing.T) {
	killed := 0
	stopper := newZaiLimitStopper(func() { killed++ })

	if stopper.observeSignal("API Error: Request rejected (429)") ||
		stopper.observeSignal("API Error: 503 Service Unavailable") {
		t.Fatal("generic 429やtransient信号がexact signal判定されました")
	}
	if killed != 0 {
		t.Fatalf("generic 429やtransient信号でkillされました: %d", killed)
	}
	select {
	case <-stopper.stopped:
		t.Fatal("非limit観測でstopped channelが閉じられました")
	default:
	}

	if !stopper.observeSignal(limitStopSignal) || !stopper.observeSignal(limitStopSignal) {
		t.Fatal("exact signalが検出されません")
	}
	if killed != 1 {
		t.Fatalf("exact signalの再観測でkillは1回である必要があります: %d", killed)
	}
	select {
	case <-stopper.stopped:
	default:
		t.Fatal("exact signal検出でstopped channelが閉じられていません")
	}
}

func TestZaiLimitStderrWatchDetectsSignalAcrossChunks(t *testing.T) {
	killed := 0
	watch := &zaiLimitStderrWatch{stopper: newZaiLimitStopper(func() { killed++ })}

	cut := strings.Index(limitStopSignal, "[1308]") + 3
	if _, err := watch.Write([]byte(limitStopSignal[:cut])); err != nil || killed != 0 {
		t.Fatalf("前半chunkでkill済み: err=%v killed=%d", err, killed)
	}
	if _, err := watch.Write([]byte(limitStopSignal[cut:])); err != nil {
		t.Fatal(err)
	}
	if killed != 1 {
		t.Fatalf("chunk分割されたsignalの検出後はkill 1回である必要があります: %d", killed)
	}
}

// stderrへexact 5h limit signalを出した後も動き続けるfake childは、最初のsignal観測で
// terminateされ後続出力・marker到達まで進まない。stderrはfile記録が残るため終端分類の
// 入力も維持される。
func TestClaudeRunnerKillsChildOnStderrFiveHourLimitSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	marker := filepath.Join(t.TempDir(), "after-limit")
	script := "#!/bin/sh\n" +
		"echo '" + limitStopSignal + "' >&2\n" +
		"sleep " + strconv.Itoa(limitStopHoldSeconds) + "\n" +
		"printf '%s\\n' 'later stdout after limit'\n" +
		"touch \"" + marker + "\"\n" +
		"exit 1\n"
	commandPath := writeLimitStopClaude(t, script)
	r, _, _ := newStreamFixtureRunner(t, commandPath)
	outputPath := filepath.Join(t.TempDir(), "out.log")

	started := time.Now()
	_, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", outputPath)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("5h上限signalでのkillはerrorとして返る必要があります")
	}
	data, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "Usage limit reached") {
		t.Fatalf("終端分類入力のstderr signalがoutputPathへ残っていません: %q", data)
	}
	if strings.Contains(string(data), "later stdout after limit") {
		t.Fatalf("kill後の後続出力まで進んでいます: %q", data)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("signal観測後にchildが後続処理へ進みました: %v", statErr)
	}
	if elapsed >= limitStopElapsedBound {
		t.Fatalf("kill後%.1f秒で復帰していません(保持%d秒を待った)", elapsed.Seconds(), limitStopHoldSeconds)
	}
}

// plain stdout行へexact 5h limit signalが出た場合も同じく早期終了し、分類構造値は
// RunResult.PlainFailureへ載る。
func TestClaudeRunnerKillsChildOnPlainStdoutFiveHourLimitSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	marker := filepath.Join(t.TempDir(), "after-limit")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '" + limitStopSignal + "'\n" +
		"sleep " + strconv.Itoa(limitStopHoldSeconds) + "\n" +
		"touch \"" + marker + "\"\n" +
		"exit 1\n"
	commandPath := writeLimitStopClaude(t, script)
	r, _, _ := newStreamFixtureRunner(t, commandPath)

	started := time.Now()
	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "out.log"))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("5h上限signalでのkillはerrorとして返る必要があります")
	}
	want := ProviderFailureClass{
		Kind:          ProviderFailureZaiFiveHour,
		FiveHourLimit: ZaiFiveHourLimit{ResetAtCST: "2026-08-23 12:00:00", ResetAtRFC3339: "2026-08-23T12:00:00+08:00"},
	}
	if result.PlainFailure != want {
		t.Fatalf("PlainFailure = %#v, want %#v", result.PlainFailure, want)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("signal観測後にchildが後続処理へ進みました: %v", statErr)
	}
	if elapsed >= limitStopElapsedBound {
		t.Fatalf("kill後%.1f秒で復帰していません(保持%d秒を待った)", elapsed.Seconds(), limitStopHoldSeconds)
	}
}

// JSON stream event内へexact 5h limit signalが出た場合も同じclassifier・同じstopperで
// 最初の観測でchildを終了させる。JSON event行はplain分類bufferに入らないため、kill判断に
// 使った観測行が終端分類入力へ残り、RunResult.PlainFailureが5h limit構造値を返す。
func TestClaudeRunnerKillsChildOnJSONStreamEventFiveHourLimitSignal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	marker := filepath.Join(t.TempDir(), "after-limit")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"system\",\"subtype\":\"error\",\"message\":\"" + limitStopSignal + "\"}'\n" +
		"sleep " + strconv.Itoa(limitStopHoldSeconds) + "\n" +
		"touch \"" + marker + "\"\n" +
		"exit 1\n"
	commandPath := writeLimitStopClaude(t, script)
	r, _, _ := newStreamFixtureRunner(t, commandPath)

	started := time.Now()
	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "out.log"))
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("5h上限signalでのkillはerrorとして返る必要があります")
	}
	want := ProviderFailureClass{
		Kind:          ProviderFailureZaiFiveHour,
		FiveHourLimit: ZaiFiveHourLimit{ResetAtCST: "2026-08-23 12:00:00", ResetAtRFC3339: "2026-08-23T12:00:00+08:00"},
	}
	if result.PlainFailure != want {
		t.Fatalf("PlainFailure = %#v, want %#v", result.PlainFailure, want)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("signal観測後にchildが後続処理へ進みました: %v", statErr)
	}
	if elapsed >= limitStopElapsedBound {
		t.Fatalf("kill後%.1f秒で復帰していません(保持%d秒を待った)", elapsed.Seconds(), limitStopHoldSeconds)
	}
}

// limitStopOrphanHoldSecondsはchild本体が成功で先に終了した後、descendantがstdout/stderr
// pipeを握り続ける時間。zaiLimitStopWaitより長く取り、非limit runで新timeoutが働けば
// この途中でerrorへ切り替わる。
const limitStopOrphanHoldSeconds = 8

// 非limit runではchild本体終了後にpipeを握るdescendantがいても成功結果を保ち、
// EOFまで無制限に待つ。全run boundedな解放を入れると成功result取得済みのrunが
// 新たにerror化する回帰を固定する。
func TestClaudeRunnerNonLimitRunKeepsUnboundedPipeWait(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	script := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\\n\",\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n" +
		"sleep " + strconv.Itoa(limitStopOrphanHoldSeconds) + " &\n" +
		"exit 0\n"
	commandPath := writeLimitStopClaude(t, script)
	r, _, _ := newStreamFixtureRunner(t, commandPath)

	started := time.Now()
	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "out.log"))
	elapsed := time.Since(started)

	if err != nil {
		t.Fatalf("非limit runがpipe保持中にerror化されました: %v", err)
	}
	if !structuredOutputPresent(result.StructuredOutput) {
		t.Fatalf("成功結果が失われました: %#v", result)
	}
	if elapsed <= zaiLimitStopWait {
		t.Fatalf("非limit runが%.1f秒で打ち切られました(bounded待機はlimit検出run限定)", elapsed.Seconds())
	}
}

// generic 429・overload・5xx相当の信号では早期終了せず、childは結果eventまで完走する。
// stderrとJSON stream eventの両面で固定し、5h limit以外の既存挙動が早期停止へ
// 巻き込まれない境界を保つ。
func TestClaudeRunnerKeepsChildAliveOnNonLimitSignals(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtureはUnix系環境向け")
	}
	cases := []struct {
		name string
		line string
		emit string
	}{
		{"generic 429 stderr", "API Error: Request rejected (429)", "stderr"},
		{"generic 429 json event", "API Error: Request rejected (429)", "json"},
		{"transient 503 stderr", "API Error: 503 Service Unavailable", "stderr"},
		{"overload 529 stderr", "API Error: 529 Overloaded", "stderr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			signalLine := c.line
			if c.emit == "json" {
				signalLine = "{\"type\":\"system\",\"subtype\":\"error\",\"message\":\"" + c.line + "\"}"
			}
			script := "#!/bin/sh\n" +
				"printf '%s\\n' '" + signalLine + "'" + stderrSuffix(c.emit) + "\n" +
				"printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"ok\\n\",\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n" +
				"exit 0\n"
			commandPath := writeLimitStopClaude(t, script)
			r, _, _ := newStreamFixtureRunner(t, commandPath)

			result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt", filepath.Join(t.TempDir(), "out.log"))
			if err != nil {
				t.Fatalf("5h limit以外の信号でchildが終了させられました: %v", err)
			}
			if !structuredOutputPresent(result.StructuredOutput) {
				t.Fatalf("結果eventまで完走していません: %#v", result)
			}
		})
	}
}

// stderrSuffixは信号行の出力面指定をshell redirectへ変える。json面はstdout行のまま流す。
func stderrSuffix(surface string) string {
	if surface == "stderr" {
		return " >&2"
	}
	return ""
}

// TestZaiFiveHourSignalMatcherIsSingleSourceは5h上限signatureの文字列literalが
// classifier(zai_limit.go)以外のproduction fileへ増えないことを固定する。runner/workflowで
// 別々の文字列判定を作ると早期停止と終端分類の精度が分岐するため。符号形式は文字列
// literalのみに限り、comment中の説明言及は許容する。
func TestZaiFiveHourSignalMatcherIsSingleSource(t *testing.T) {
	signatures := []string{`"1308"`, `"Usage limit reached for 5 hour`}
	for _, dir := range []string{".", "../workflow"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			for _, signature := range signatures {
				if name != "zai_limit.go" && strings.Contains(string(data), signature) {
					t.Fatalf("%sが5h上限signature %qを直接扱っています。判定はDetectZaiFiveHourLimitText(zai_limit.go)へ集約してください", filepath.Join(dir, name), signature)
				}
			}
		}
	}
}
