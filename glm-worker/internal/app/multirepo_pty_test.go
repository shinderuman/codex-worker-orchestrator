//go:build darwin || linux

package app

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestStdinPayloadPTYParallelNonInterferenceは2つの独立PTY(glm-worker呼出ごとに親が用意
// するCodex PTY A/B相当)でstdin payload輸送を時間的に重ね、mode変更とpayloadが
// 互相干しないことを実PTY 2本で検証する。PTY Aをraw適用済み・本文未送の状態で保持した
// 間にPTY Bがraw適用→READY marker→本文受取→termios復元まで完結し、その後のPTY A本文が
// NUL/CR/Ctrl-C含みでbyte正確に届けば、Bのmode変更がAのline disciplineへ影響していない
// ことを意味する。
func TestStdinPayloadPTYParallelNonInterference(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PTY transport契約の実機検証はmacOSのscript前提")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skipf("script command not found: %v", err)
	}

	ptyA := startPTYParallelTransport(t, "GLMPTYA", 1)
	defer ptyA.close()
	ptyB := startPTYParallelTransport(t, "GLMPTYB", 2)
	defer ptyB.close()

	finishPTYParallelTransport(t, ptyB, "GLMPTYB")
	finishPTYParallelTransport(t, ptyA, "GLMPTYA")

	assertPTYParallelIsolated(t, ptyA, "GLMPTYA", "GLMPTYB")
	assertPTYParallelIsolated(t, ptyB, "GLMPTYB", "GLMPTYA")
}

// ptyParallelTransportは1本の実PTY輸送の進行状態。script子process・stdin writer・
// marker待ち・全文受取の各段階を保持する。
type ptyParallelTransport struct {
	payload    string
	outPath    string
	ctx        context.Context
	cancel     context.CancelFunc
	command    *exec.Cmd
	stdin      io.WriteCloser
	output     chan string
	outputText string
}

func (p *ptyParallelTransport) close() {
	_ = p.stdin.Close()
	p.cancel()
}

// startPTYParallelTransportはproduction caller契約(固定command起動・事前terminal設定なし)
// でPTYを起動し、READY marker確認まで待つ。この時点でhelperはraw modeで本文待ちになり、
// 複数PTYのraw期間を重ねられる。
func startPTYParallelTransport(t *testing.T, marker string, run int) *ptyParallelTransport {
	t.Helper()
	payload := marker + " parallel run=" + strconv.Itoa(run) +
		" 1行目\nNUL:\x00 CR:\r CtrlC:\x03 quote\" dollar$ `git ls-files -s -z` 日本語"
	sha := stdinPayloadSHA(payload)
	outPath := filepath.Join(t.TempDir(), "pty-payload")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	command := exec.CommandContext(ctx, "script", "-q", "/dev/null", "sh", "-c",
		`exec "$GLM_WORKER_STDIN_PTY_BIN" -test.run=TestStdinPayloadSelfContainedPTY`)
	command.Env = append(os.Environ(),
		"GLM_WORKER_STDIN_PTY_HELPER=1",
		"GLM_WORKER_STDIN_PTY_BIN="+os.Args[0],
		"GLM_WORKER_STDIN_PTY_OUT="+outPath,
		"GLM_WORKER_STDIN_PTY_BYTES="+strconv.Itoa(len(payload)),
		"GLM_WORKER_STDIN_PTY_SHA="+sha,
	)
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdoutPipe, err := command.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	var scriptStderr strings.Builder
	command.Stderr = &scriptStderr
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}

	markerReady := make(chan error, 1)
	outputText := make(chan string, 1)
	go ptyDrainOutput(stdoutPipe, markerReady, outputText)

	transport := &ptyParallelTransport{
		payload: payload,
		outPath: outPath,
		ctx:     ctx,
		cancel:  cancel,
		command: command,
		stdin:   stdin,
		output:  outputText,
	}
	select {
	case err := <-markerReady:
		if err != nil {
			detail, _ := os.ReadFile(outPath)
			transport.close()
			t.Fatalf("%s: READY marker観測前に失敗: %v result=%q", marker, err, detail)
		}
	case <-ctx.Done():
		transport.close()
		t.Fatalf("%s: READY markerが現れません", marker)
	}
	return transport
}

// finishPTYParallelTransportはmarker確認済みのPTYへ本文を1回だけ書き、helper終了・
// 出力読切りまで待つ。
func finishPTYParallelTransport(t *testing.T, transport *ptyParallelTransport, marker string) {
	t.Helper()
	if _, err := transport.stdin.Write([]byte(transport.payload)); err != nil {
		t.Fatalf("%s: 本文write失敗: %v", marker, err)
	}
	output := ptyWaitOutput(transport.ctx, transport.output)
	waitErr := transport.command.Wait()
	if waitErr != nil {
		detail, readErr := os.ReadFile(transport.outPath)
		if readErr != nil {
			t.Fatalf("%s: helper終了: %v result=unreadable output=%q", marker, waitErr, output)
		}
		t.Fatalf("%s: helper終了: %v result=%q output=%q", marker, waitErr, detail, output)
	}
	transport.outputText = output
}

// assertPTYParallelIsolatedは自PTYの輸送結果がbyte正確で、marker 1回・echoなし・相手
// marker語の混入なしを検査する。
func assertPTYParallelIsolated(t *testing.T, transport *ptyParallelTransport, own string, other string) {
	t.Helper()
	received, err := os.ReadFile(transport.outPath)
	if err != nil {
		t.Fatalf("%s: 結果fileを読めません: %v", own, err)
	}
	if string(received) != transport.payload {
		t.Fatalf("%s: PTY経由payloadがbyte正確ではありません: got %q want %q", own, received, transport.payload)
	}
	for _, b := range []string{"\x00", "\r", "\x03"} {
		if !strings.Contains(string(received), b) {
			t.Fatalf("%s: NUL/CR/Ctrl-C相当byteが失っています: %q", own, received)
		}
	}
	if strings.Count(transport.outputText, stdinReadyMarker) != 1 {
		t.Fatalf("%s: READY markerがexactly onceではありません: %q", own, transport.outputText)
	}
	if strings.Contains(transport.outputText, own) {
		t.Fatalf("%s: 本文がechoでtool outputへ複製されています: %q", own, transport.outputText)
	}
	if strings.Contains(transport.outputText, other) {
		t.Fatalf("%s: PTY出力へ相手 %s の本文が混入しています: %q", own, other, transport.outputText)
	}
}
