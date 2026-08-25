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

const ptyStartupFeedRuns = 5

func stdinReadyMarker() string {
	line, err := marshalEventLine(stdinReadyControlEvent{Type: "control", Event: "stdin_ready"})
	if err != nil {
		panic(err)
	}
	return strings.TrimSuffix(string(line), "\n")
}

func TestStdinPayloadSelfContainedPTY(t *testing.T) {
	ptyTransportCase(t, "")
}

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

			detail, _ := os.ReadFile(outPath)
			t.Fatalf("run %d: READY marker観測前に失敗: %v result=%q", run, err, detail)
		}
	case <-ctx.Done():
		t.Fatalf("run %d: READY markerが現れません", run)
	}

	if _, err := stdin.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}

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
