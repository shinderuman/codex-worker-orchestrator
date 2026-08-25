package runner

import (
	"fmt"
	"sync"
	"time"
)

// 停止のTERM→KILL昇格までの猶予と、KILL後のgroup消滅確認budget。実Claude CLIは
// TERMで即時終了する(PoC実測)。猶予を過ぎても残るchildだけがKILL対象になる。
// testから短絡できるためvarにしている。
var (
	stopTermGrace       = 10 * time.Second
	killSettleTimeout   = 2 * time.Second
	processGroupPollGap = 50 * time.Millisecond
)

// InterruptedCallErrorは親Codexの--stop要求で実行中taskが安全停止したことを表す。
// 停止されたinvocationはこのerrorで非zero終端し、checkpoint/sessionはinterrupted
// 状態として保存済みのため--resumeで同一sessionから再開できる。
type InterruptedCallError struct {
	Phase    string
	TaskID   string
	RepoRoot string
	// CleanupWarningは停止後にprocess groupへ残存が観測された場合の診断。空なら
	// group非残存を確認済み。
	CleanupWarning string
}

func (e *InterruptedCallError) Error() string {
	message := fmt.Sprintf("task interrupted by glm-worker --stop at phase %s; task stopped, resumable via glm-worker --resume", e.Phase)
	if e.CleanupWarning != "" {
		message += ": " + e.CleanupWarning
	}
	return message
}

// StopOutcomeは停止要求への応答として確定したinvocation側の終着。
// Interrupted=trueはinterrupted checkpoint保存済み、falseは停止より先に自然終端した。
// CleanupWarningは停止後にprocess groupへ残存が観測された場合の診断で、空ならgroup非残存を
// 確認済みである。残存があるinterruptedは安全停止authorityから除外される。
type StopOutcome struct {
	Interrupted    bool
	TaskID         string
	CleanupWarning string
}

// StopControllerは1回のworkflow実行invocationに対する停止要求の受け渡しを調停する。
// --stop endpointはRequestで停止を申し込み、workflow/runnerはRequestedでそれを観測して
// 停止処理へ遷移し、結果をNotify*で1回だけ確定する。Requestの冪等化に合わせ、確定した
// outcomeは確定前の全waiterと確定後の待機へ同一値で配られる。endpoint接続とowner側ackが
// 停止のauthorityであり、lock fileのPIDは診断値にとどまる。
type StopController struct {
	mu        sync.Mutex
	requested bool
	notified  bool
	outcome   StopOutcome
	requestCh chan struct{}
	doneCh    chan struct{}
}

func NewStopController() *StopController {
	return &StopController{
		requestCh: make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Requestは停止要求を出す。複数回の要求は同じ停止へ冪等に畳まる。
func (c *StopController) Request() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.requested {
		return
	}
	c.requested = true
	close(c.requestCh)
}

// Requestedは停止要求のbroadcast channel。要求後にcloseされ、以後観測し続けられる。
func (c *StopController) Requested() <-chan struct{} {
	return c.requestCh
}

// StopRequestedは現在までに停止要求が出ているか。
func (c *StopController) StopRequested() bool {
	select {
	case <-c.requestCh:
		return true
	default:
		return false
	}
}

// WaitOutcomeはinvocation側の確定を待つ。NotifyInterrupted・NotifyFinishedの先に
// 到達した方の結果だけを返し、確定前に入った全waiterと確定後の呼出も同じ値を受け取る。
func (c *StopController) WaitOutcome() StopOutcome {
	<-c.doneCh
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.outcome
}

// NotifyInterruptedはinterrupted checkpoint保存完了を確定する。最初の確定だけ有効。
// cleanupWarningは停止後のprocess group残存診断で、空ならgroup非残存確認済みの安全停止になる。
func (c *StopController) NotifyInterrupted(taskID string, cleanupWarning string) {
	c.notify(StopOutcome{Interrupted: true, TaskID: taskID, CleanupWarning: cleanupWarning})
}

// NotifyFinishedは停止が効果を持つ前にinvocationが終端したことを確定する。
func (c *StopController) NotifyFinished() {
	c.notify(StopOutcome{})
}

func (c *StopController) notify(outcome StopOutcome) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.notified {
		return
	}
	c.notified = true
	c.outcome = outcome
	close(c.doneCh)
}
