//go:build darwin || linux

package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ptyStartupFeedRunsはmarker確認直後の即writeでの輸送成立を反復観測する成功case回数。
const ptyStartupFeedRuns = 5

// stdinReadyMarkerはREADY control eventのmachine JSONL 1行本文。productionのtyped
// producerと同一encodingから組み立て、契約形式をtest側へ再定義しない。
func stdinReadyMarker() string {
	line, err := marshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		panic(err)
	}
	return strings.TrimSuffix(string(line), "\n")
}

// TestStdinPayloadSelfContainedPTYはcaller契約「固定command起動・READY marker確認後の
// payload 1回writeだけ」が事前sttyなしの実PTY上で成立することをAI callなしで検証する。
// helperはscriptが用意したcanonical+echo有効の初期termiosを前提にproduction経路
// (enterStdinRawMode→READY marker→readStdinPayload→復元)を通し、raw適用中・復元後の
// termiosと宣言byte数どおりのpayload本文を固定する。親側はproduction callerと同じ順序で
// marker確認直後に本文を1回だけ書き、反復実行で輸送の安定性を観測する。
// macOSの`script`でPTYを確保するため、darwin以外や`script`不在環境ではskipする。
func TestStdinPayloadSelfContainedPTY(t *testing.T) {
	ptyTransportCase(t, "")
}

// TestStdinPayloadPTYSHAMismatchRestoresTerminalはsha256不一致が読み取り後の復元を
// 含む同じerror pathでfail closedすることを実PTY上で検証する。
func TestStdinPayloadPTYSHAMismatchRestoresTerminal(t *testing.T) {
	ptyTransportCase(t, "mismatch")
}

func ptyTransportCase(t *testing.T, scenario string) {
	t.Helper()
	if os.Getenv("GLM_WORKER_STDIN_PTY_HELPER") == "1" {
		stdinSelfContainedPTYHelper()
		return
	}
	if runtime.GOOS != "darwin" {
		t.Skip("PTY transport契約の実機検証はmacOSのscript前提")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skipf("script command not found: %v", err)
	}

	if scenario == "mismatch" {
		ptyTransportRun(t, scenario, 1)
		return
	}
	for run := 1; run <= ptyStartupFeedRuns; run++ {
		ptyTransportRun(t, scenario, run)
	}
}

// ptyTransportRunは1回分の実PTY輸送をproduction caller順序
// 「process起動 → READY marker確認 → 即本文1回write」で実行する。成功caseはrun番号で
// 本文を変えてbyte-identicalな偶然の成立を排除し、marker直後の即writeがraw適用済み
// line disciplineだけで処理されることを反復観測する。
func ptyTransportRun(t *testing.T, scenario string, run int) {
	t.Helper()
	payload := "GLMPTYMARK self-contained run=" + strconv.Itoa(run) +
		" 1行目\nNUL:\x00 CR:\r CtrlC:\x03 quote\" dollar$ `git ls-files -s -z` 日本語"
	sha := stdinPayloadSHA(payload)
	if scenario == "mismatch" {
		sha = stdinPayloadSHA(payload + "corrupted")
	}
	outPath := filepath.Join(t.TempDir(), "pty-payload")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	// 固定wrapper: 本文はcommandへ入れず、caller側のstty等のterminal設定なしでtest binary(helper)へexecする。
	cmd := exec.CommandContext(ctx, "script", "-q", "/dev/null", "sh", "-c",
		`exec "$GLM_WORKER_STDIN_PTY_BIN" -test.run=TestStdinPayloadSelfContainedPTY`)
	cmd.Env = append(os.Environ(),
		"GLM_WORKER_STDIN_PTY_HELPER=1",
		"GLM_WORKER_STDIN_PTY_BIN="+os.Args[0],
		"GLM_WORKER_STDIN_PTY_OUT="+outPath,
		"GLM_WORKER_STDIN_PTY_BYTES="+strconv.Itoa(len(payload)),
		"GLM_WORKER_STDIN_PTY_SHA="+sha,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	// scriptはhelperのstdout/stderrをPTYで1本へ統合して自身のstdoutへ流すため、
	// marker観測もecho検査もstdout streamだけで行う。
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var scriptStderr strings.Builder
	cmd.Stderr = &scriptStderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	markerReady := make(chan error, 1)
	outputText := make(chan string, 1)
	go ptyDrainOutput(stdoutPipe, markerReady, outputText)
	select {
	case err := <-markerReady:
		if err != nil {
			// marker未観測・先行終了では本文を送らないcaller契約のままfailさせる。
			detail, _ := os.ReadFile(outPath)
			t.Fatalf("run %d: READY marker観測前に失敗: %v result=%q", run, err, detail)
		}
	case <-ctx.Done():
		t.Fatalf("run %d: READY markerが現れません", run)
	}

	// marker確認直後に末尾改行なしの本文を1回だけ書き、stdin pipeは開いたまま保つ(EOF不要契約)。
	if _, err := stdin.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}

	// 出力本文はdrain goroutineがEOF(process tree終了)まで読み切った後channel経由でだけ
	// 受け取る。StdoutPipeはWaitが読み取り側を閉じるため、残り出力を失わないよう
	// drain完了を確認してからWaitを呼ぶ。
	output := ptyWaitOutput(ctx, outputText)
	waitErr := cmd.Wait()
	if scenario == "mismatch" {
		if waitErr == nil {
			t.Fatalf("sha256不一致が成功扱いになっています: output=%q", output)
		}
	} else if waitErr != nil {
		detail, readErr := os.ReadFile(outPath)
		if readErr != nil {
			t.Fatalf("run %d: helper終了: %v output=%q result=unreadable", run, waitErr, output)
		}
		t.Fatalf("run %d: helper終了: %v result=%q output=%q", run, waitErr, detail, output)
	}

	received, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "mismatch" {
		if !strings.Contains(string(received), "restored=true") || !strings.Contains(string(received), "stdin payload sha256 mismatch") {
			t.Fatalf("sha256不一致時の復元付きfail closedが成立していません: %q output=%q", received, output)
		}
	} else {
		if string(received) != payload {
			t.Fatalf("run %d: PTY経由payloadが宣言byte数どおり保存されていません: got %q want %q", run, received, payload)
		}
		for _, b := range []string{"\x00", "\r", "\x03"} {
			if !strings.Contains(string(received), b) {
				t.Fatalf("run %d: NUL/CR/Ctrl-C相当byteが失われています: %q", run, received)
			}
		}
	}
	if strings.Count(output, stdinReadyMarker()) != 1 {
		t.Fatalf("run %d: READY markerがexactly onceではありません: %q", run, output)
	}
	if strings.Contains(output, "GLMPTYMARK") {
		t.Fatalf("run %d: 本文がechoでtool outputへ複製されています: %q", run, output)
	}
}

// ptyDrainOutputはstdout streamを行単位で読み、最初のREADY marker行を報告した後もEOFまで
// 読み続けて全出力本文を返す。marker前のEOFはmarker未観測としてerrorへ流す。
func ptyDrainOutput(r io.Reader, markerReady chan<- error, outputText chan<- string) {
	reader := bufio.NewReader(r)
	var output strings.Builder
	markerLine := stdinReadyMarker() + "\n"
	seen := false
	for {
		line, readErr := reader.ReadString('\n')
		output.WriteString(line)
		if line == markerLine && !seen {
			seen = true
			markerReady <- nil
		}
		if readErr != nil {
			if !seen {
				if readErr == io.EOF {
					markerReady <- fmt.Errorf("marker未観測のまま出力が終了しました")
				} else {
					markerReady <- fmt.Errorf("stdout読み取り失敗: %w", readErr)
				}
			}
			outputText <- output.String()
			return
		}
	}
}

// ptyWaitOutputはdrain goroutineがEOFまで読み切った出力本文を取り出す。出力はdrain
// goroutineだけが書きchannelで受信するためrace freeに読める。timeout後もdrainが完了
// しないときは診断用の省略文字列へfall throughし、呼び出し元のWaitが失敗を報告する。
func ptyWaitOutput(ctx context.Context, outputText <-chan string) string {
	select {
	case output := <-outputText:
		return output
	case <-ctx.Done():
		select {
		case output := <-outputText:
			return output
		case <-time.After(10 * time.Second):
			return "(drain未完了のため出力省略)"
		}
	}
}

// stdinSelfContainedPTYHelperは子process側のhelper本体。親と同じtest binaryを起動し、
// callerがterminal設定をしていない前提(canonical+echo)を確認してからproduction経路
// (enterStdinRawMode→READY marker→readStdinPayload→復元)で宣言byte数だけstdinから読み取り、
// termios復元までの成否を結果fileへ書く。
func stdinSelfContainedPTYHelper() {
	outPath := os.Getenv("GLM_WORKER_STDIN_PTY_OUT")
	want, err := strconv.ParseInt(os.Getenv("GLM_WORKER_STDIN_PTY_BYTES"), 10, 64)
	sha := os.Getenv("GLM_WORKER_STDIN_PTY_SHA")
	if err != nil || outPath == "" || want <= 0 || sha == "" {
		os.Exit(2)
	}

	var before syscall.Termios
	if err := getTerminalAttrs(os.Stdin.Fd(), &before); err != nil {
		ptyHelperFail(outPath, "stdin is not a terminal: "+err.Error())
	}
	if before.Lflag&syscall.ECHO == 0 || before.Lflag&syscall.ICANON == 0 {
		ptyHelperFail(outPath, "precondition: PTY starts in canonical+echo without caller stty")
	}

	restore, rawApplied, err := enterStdinRawMode(os.Stdin)
	if err != nil {
		ptyHelperFail(outPath, "raw mode setup: "+err.Error())
	}
	if !rawApplied {
		ptyHelperFail(outPath, "precondition: PTY stdin should enter raw mode")
	}
	var during syscall.Termios
	if err := getTerminalAttrs(os.Stdin.Fd(), &during); err != nil {
		ptyHelperFail(outPath, "termios during read: "+err.Error())
	}
	if during.Lflag&syscall.ECHO != 0 || during.Lflag&syscall.ICANON != 0 || during.Lflag&syscall.ISIG != 0 ||
		during.Iflag&syscall.ICRNL != 0 || during.Oflag&syscall.OPOST != 0 {
		ptyHelperFail(outPath, "raw mode not applied")
	}
	// markerはraw適用を保証した直後にだけ出す(production run()と同じ順序)。
	if err := emitStdinReadyControlEvent(os.Stderr); err != nil {
		ptyHelperFail(outPath, "ready marker: "+err.Error())
	}

	payload, readErr := readStdinPayload(os.Stdin, want, sha)
	restoreErr := restore()
	var after syscall.Termios
	stateErr := getTerminalAttrs(os.Stdin.Fd(), &after)
	restored := stateErr == nil && after.Iflag == before.Iflag && after.Oflag == before.Oflag &&
		after.Cflag == before.Cflag && after.Cc == before.Cc &&
		after.Lflag&^kernelStateLflagBits == before.Lflag&^kernelStateLflagBits
	if readErr != nil || restoreErr != nil || !restored {
		ptyHelperFail(outPath, "restored="+strconv.FormatBool(restored)+
			" read="+errString(readErr)+" restore="+errString(restoreErr)+" probe="+errString(stateErr)+
			" before="+termiosDebug(before)+" after="+termiosDebug(after))
	}
	if err := os.WriteFile(outPath, []byte(payload), 0o600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func ptyHelperFail(outPath string, detail string) {
	_ = os.WriteFile(outPath, []byte("ERR "+detail), 0o600)
	os.Exit(3)
}

func errString(err error) string {
	if err == nil {
		return "nil"
	}
	return err.Error()
}

func termiosDebug(t syscall.Termios) string {
	return strconv.FormatUint(uint64(t.Iflag), 16) + "/" + strconv.FormatUint(uint64(t.Oflag), 16) + "/" +
		strconv.FormatUint(uint64(t.Cflag), 16) + "/" + strconv.FormatUint(uint64(t.Lflag), 16) + "/" +
		strconv.FormatUint(uint64(t.Ispeed), 16) + "/" + strconv.FormatUint(uint64(t.Ospeed), 16)
}

// TestEnterStdinRawModeSkipsTermiosForNonTerminalはstdinがpipe・regular file・
// 非file readerのいずれでもtermios変更を行わずraw未適用(applied=false)のno-op復元を
// 返すことを検証する。非TTY stdinはREADY markerを出さない契約の条件になる。
func TestEnterStdinRawModeSkipsTermiosForNonTerminal(t *testing.T) {
	pipeReader, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeReader.Close()

	restore, applied, err := enterStdinRawMode(pipeReader)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("pipe stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("pipe stdinの復元がerrorを返しています: %v", err)
	}

	payloadPath := filepath.Join(t.TempDir(), "payload-source")
	if err := os.WriteFile(payloadPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadFile, err := os.Open(payloadPath)
	if err != nil {
		t.Fatal(err)
	}
	defer payloadFile.Close()
	restore, applied, err = enterStdinRawMode(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("regular file stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("regular file stdinの復元がerrorを返しています: %v", err)
	}

	restore, applied, err = enterStdinRawMode(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("非file stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("非file stdinの復元がerrorを返しています: %v", err)
	}
}

// TestEnterStdinRawModeFailsClosedOnProbeErrorはterminal probeのioctlがENOTTY以外で
// 失敗した場合をTTYでないと黙って解釈せず、payload読み取り前にfail closedすることを
// 閉じたfd(EBADF)で検証する。
func TestEnterStdinRawModeFailsClosedOnProbeError(t *testing.T) {
	closed, err := os.Create(filepath.Join(t.TempDir(), "closed"))
	if err != nil {
		t.Fatal(err)
	}
	if err := closed.Close(); err != nil {
		t.Fatal(err)
	}

	_, _, err = enterStdinRawMode(closed)
	if err == nil || !strings.Contains(err.Error(), "stdin terminal state probe failed") {
		t.Fatalf("probe失敗をfail closedする必要があります: err=%v", err)
	}
}

// TestStdinUnsupportedPlatformTerminalFailsClosedOnTerminalLikeInputはtermios非実装環境の
// 境界をunix上で実行して検証する。char device(console・/dev/null等)はterminal扱いで
// 読み取り前に明示的にfail closedし、pipe・regular fileはno-op復元で通る。
// /dev/nullがfalse positiveとなる点はfail closed側へ働くため許容する。
func TestStdinUnsupportedPlatformTerminalFailsClosedOnTerminalLikeInput(t *testing.T) {
	nullDevice, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("character deviceが開けません: %v", err)
	}
	defer nullDevice.Close()

	restore, applied, err := stdinUnsupportedPlatformTerminal(nullDevice)
	if err == nil || !strings.Contains(err.Error(), "raw mode is not implemented") {
		t.Fatalf("terminalらしきstdinをfail closedする必要があります: err=%v", err)
	}

	pipeReader, _, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	defer pipeReader.Close()
	restore, applied, err = stdinUnsupportedPlatformTerminal(pipeReader)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("pipe stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("pipe stdinの復元がerrorを返しています: %v", err)
	}

	payloadPath := filepath.Join(t.TempDir(), "payload-source")
	if err := os.WriteFile(payloadPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadFile, openErr := os.Open(payloadPath)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer payloadFile.Close()
	restore, applied, err = stdinUnsupportedPlatformTerminal(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("regular file stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("regular file stdinの復元がerrorを返しています: %v", err)
	}
}
