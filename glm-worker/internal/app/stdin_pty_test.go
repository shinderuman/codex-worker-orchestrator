//go:build darwin || linux

package app

import (
	"context"
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

// TestStdinPayloadSelfContainedPTYはcaller契約「固定command起動とpayload 1回writeだけ」が
// 事前sttyなしの実PTY上で成立することをAI callなしで検証する。helperはscriptが用意した
// canonical+echo有効の初期termiosを前提にproduction経路(enterStdinRawMode→readStdinPayload→
// 復元)を通し、raw適用中・復元後のtermiosと宣言byte数どおりのpayload本文を固定する。
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

	payload := "GLMPTYMARK self-contained 1行目\nNUL:\x00 CR:\r CtrlC:\x03 quote\" dollar$ `git ls-files -s -z` 日本語"
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
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if data, readErr := os.ReadFile(outPath); readErr == nil && string(data) == "ready" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("helperがreadyにならない: %q %v", output.String(), cmd.Wait())
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 末尾改行なしの本文を1回だけ書き、stdin pipeは開いたまま保つ(EOF不要契約)。
	if _, err := stdin.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	select {
	case err := <-waitErr:
		if scenario == "mismatch" {
			if err == nil {
				t.Fatalf("sha256不一致が成功扱いになっています: output=%q", output.String())
			}
			break
		}
		if err != nil {
			detail, readErr := os.ReadFile(outPath)
			if readErr != nil {
				t.Fatalf("helper終了: %v output=%q result=unreadable", err, output.String())
			}
			t.Fatalf("helper終了: %v result=%q output=%q", err, detail, output.String())
		}
	case <-ctx.Done():
		t.Fatalf("宣言byte数へ到達せず停止: output=%q", output.String())
	}

	received, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if scenario == "mismatch" {
		if !strings.Contains(string(received), "restored=true") || !strings.Contains(string(received), "stdin payload sha256 mismatch") {
			t.Fatalf("sha256不一致時の復元付きfail closedが成立していません: %q output=%q", received, output.String())
		}
	} else {
		if string(received) != payload {
			t.Fatalf("PTY経由payloadが宣言byte数どおり保存されていません: got %q want %q", received, payload)
		}
		for _, b := range []string{"\x00", "\r", "\x03"} {
			if !strings.Contains(string(received), b) {
				t.Fatalf("NUL/CR/Ctrl-C相当byteが失われています: %q", received)
			}
		}
	}
	if strings.Contains(output.String(), "GLMPTYMARK") {
		t.Fatalf("本文がechoでtool outputへ複製されています: %q", output.String())
	}
}

// stdinSelfContainedPTYHelperは子process側のhelper本体。親と同じtest binaryを起動し、
// callerがterminal設定をしていない前提(canonical+echo)を確認してからproduction経路で
// 宣言byte数だけstdinから読み取り、termios復元までの成否を結果fileへ書く。
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

	restore, err := enterStdinRawMode(os.Stdin)
	if err != nil {
		ptyHelperFail(outPath, "raw mode setup: "+err.Error())
	}
	var during syscall.Termios
	if err := getTerminalAttrs(os.Stdin.Fd(), &during); err != nil {
		ptyHelperFail(outPath, "termios during read: "+err.Error())
	}
	if during.Lflag&syscall.ECHO != 0 || during.Lflag&syscall.ICANON != 0 || during.Lflag&syscall.ISIG != 0 ||
		during.Iflag&syscall.ICRNL != 0 || during.Oflag&syscall.OPOST != 0 {
		ptyHelperFail(outPath, "raw mode not applied")
	}
	if err := os.WriteFile(outPath, []byte("ready"), 0o600); err != nil {
		os.Exit(2)
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
// 非file readerのいずれでもtermios変更を行わずno-op復元を返すことを検証する。
func TestEnterStdinRawModeSkipsTermiosForNonTerminal(t *testing.T) {
	pipeReader, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pipeReader.Close()

	restore, err := enterStdinRawMode(pipeReader)
	if err != nil {
		t.Fatal(err)
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
	restore, err = enterStdinRawMode(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("regular file stdinの復元がerrorを返しています: %v", err)
	}

	restore, err = enterStdinRawMode(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
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

	_, err = enterStdinRawMode(closed)
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

	restore, err := stdinUnsupportedPlatformTerminal(nullDevice)
	if err == nil || !strings.Contains(err.Error(), "raw mode is not implemented") {
		t.Fatalf("terminalらしきstdinをfail closedする必要があります: err=%v", err)
	}

	pipeReader, _, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	defer pipeReader.Close()
	restore, err = stdinUnsupportedPlatformTerminal(pipeReader)
	if err != nil {
		t.Fatal(err)
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
	restore, err = stdinUnsupportedPlatformTerminal(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("regular file stdinの復元がerrorを返しています: %v", err)
	}
}
