package runner

import (
	"fmt"
	"sync"
	"time"
)

type InterruptedCallError struct {
	Phase    string
	TaskID   string
	RepoRoot string

	CleanupWarning string
}

type StopOutcome struct {
	Interrupted    bool
	TaskID         string
	CleanupWarning string
}

type StopController struct {
	mu        sync.Mutex
	requested bool
	notified  bool
	outcome   StopOutcome
	requestCh chan struct{}
	doneCh    chan struct{}
}

var (
	stopTermGrace       = 10 * time.Second
	killSettleTimeout   = 2 * time.Second
	processGroupPollGap = 50 * time.Millisecond
)

func (e *InterruptedCallError) Error() string {
	message := fmt.Sprintf("task interrupted by glm-worker --stop at phase %s; task stopped, resumable via glm-worker --resume", e.Phase)
	if e.CleanupWarning != "" {
		message += ": " + e.CleanupWarning
	}
	return message
}

func NewStopController() *StopController {
	return &StopController{
		requestCh: make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

func (c *StopController) Request() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.requested {
		return
	}
	c.requested = true
	close(c.requestCh)
}

func (c *StopController) Requested() <-chan struct{} {
	return c.requestCh
}

func (c *StopController) StopRequested() bool {
	select {
	case <-c.requestCh:
		return true
	default:
		return false
	}
}

func (c *StopController) WaitOutcome() StopOutcome {
	<-c.doneCh
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.outcome
}

func (c *StopController) NotifyInterrupted(taskID string, cleanupWarning string) {
	c.notify(StopOutcome{Interrupted: true, TaskID: taskID, CleanupWarning: cleanupWarning})
}

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
