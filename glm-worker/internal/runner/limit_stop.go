package runner

import (
	"io"
	"os"
	"sync"
	"time"
)

// zaiLimitStopWaitはexact 5h limit signalを観測してchildをkillした後、descendant
// processがstdout/stderr pipeを握ったままであるときにreaderのdrain待機を打ち切る上限。
// limitを検出したrunだけがこのbounded待機へ移り、非limit runのwait/pipe semanticsへは
// timeoutを追加しない。
const zaiLimitStopWait = 5 * time.Second

// zaiLimitStopperはClaude CLI実行中の出力からZ.ai 5h上限のexact signalを最初に観測した
// 時点でchild processを終了させる。判定はstderr・plain stdout・JSON stream eventの全受信面で
// workflow終端分類と同じDetectZaiFiveHourLimitTextを単一の正として使い、generic 429や
// transient信号では発動しない。停止はkill 1回だけですみ、観測の重複が二重killを生まない。
// stopped channelはlimitを検出したrunだけがrunner側のbounded drainへ切り替える合図。
type zaiLimitStopper struct {
	kill     func()
	stopped  chan struct{}
	killOnce sync.Once
}

func newZaiLimitStopper(kill func()) *zaiLimitStopper {
	return &zaiLimitStopper{kill: kill, stopped: make(chan struct{})}
}

// observeSignalは観測済み出力本文を既存classifierへ通し、exact signalのときだけchildを
// 終了させてtrueを返す。
func (s *zaiLimitStopper) observeSignal(text string) bool {
	if _, ok := DetectZaiFiveHourLimitText(text); !ok {
		return false
	}
	s.killOnce.Do(func() {
		close(s.stopped)
		s.kill()
	})
	return true
}

// zaiLimitStderrWatchはstderr本文を早期検出へ渡すための受けてあり、fileへの記録は
// io.MultiWriter側が行う。分類入力と同じ末尾bounded本文を保持し、chunk境界で信号が
// 分断されても蓄積全体で検出する。Writeは決してerrorを返さない(検出処理の失敗が
// stderr file本体への記録を止めない)。
type zaiLimitStderrWatch struct {
	stopper *zaiLimitStopper
	seen    []byte
}

func (w *zaiLimitStderrWatch) Write(p []byte) (int, error) {
	w.seen = append(w.seen, p...)
	if excess := len(w.seen) - plainSignalMaxBytes; excess > 0 {
		w.seen = w.seen[excess:]
	}
	w.stopper.observeSignal(string(w.seen))
	return len(p), nil
}

// drainPipeはchild stdout/stderr pipeのread endからsinkへcopyしてreaderを閉じる。
func drainPipe(reader *os.File, sink io.Writer) error {
	_, copyErr := io.Copy(sink, reader)
	closeErr := reader.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// waitPipeDrainはstdout/stderr両readerのdrain完了を待つ。limit停止したrunだけ
// zaiLimitStopWaitでbounded待機し、timeout時はread endを強制closeして確定復帰させる。
// 非limit runはEOFまで無制限に待ち、exec.Cmd内部copy goroutineをWaitが待っていた従来の
// wait semanticsと同じ挙動を保つ。
func waitPipeDrain(
	stdoutReader, stderrReader *os.File,
	drainErrors chan error,
	stopped <-chan struct{},
) error {
	firstErr := error(nil)
	collect := func(err error) {
		if firstErr == nil {
			firstErr = err
		}
	}
	remaining := 2
	for remaining > 0 {
		select {
		case err := <-drainErrors:
			remaining--
			collect(err)
			continue
		case <-stopped:
			// 以降の待機はboundedへ切り替える。nil化でこの分岐へ二度入らない。
			stopped = nil
		}
		timer := time.NewTimer(zaiLimitStopWait)
		for remaining > 0 {
			select {
			case err := <-drainErrors:
				remaining--
				collect(err)
			case <-timer.C:
				// closeで待機中のReadは即座にerrorで返るため、残りは回収すれば確定する。
				stdoutReader.Close()
				stderrReader.Close()
				for remaining > 0 {
					collect(<-drainErrors)
					remaining--
				}
			}
		}
		timer.Stop()
	}
	return firstErr
}
