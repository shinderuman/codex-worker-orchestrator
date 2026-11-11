package runner

import (
	"fmt"
	"os/exec"
	"time"
)

type ScheduledPauseError struct {
	Phase                  string
	TaskID                 string
	RepoRoot               string
	ResumeAt               time.Time
	Started                bool
	CleanupWarning         string
	RecoveryClassification string
}

func (e *ScheduledPauseError) Error() string {
	return fmt.Sprintf("GLM invocation paused by configured schedule until %s", e.ResumeAt.Format(time.RFC3339))
}

func (r *ClaudeRunner) runManagedCommand(command *exec.Cmd, deadline time.Time, probe bool) error {
	active, next := r.evaluatePause(time.Now())
	if active {
		return &ScheduledPauseError{ResumeAt: next}
	}
	if !probe && r.stop == nil && next.IsZero() {
		return command.Run()
	}
	if err := command.Start(); err != nil {
		return err
	}
	return r.awaitManagedCommand(command, deadline, probe, next)
}

func (r *ClaudeRunner) awaitManagedCommand(command *exec.Cmd, deadline time.Time, probe bool, next time.Time) error {
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()
	var stopRequested <-chan struct{}
	if r.stop != nil {
		stopRequested = r.stop.Requested()
	}
	var deadlineReached <-chan time.Time
	var stopDeadline func()
	if probe {
		deadlineReached, stopDeadline = probeDeadlineChannel(deadline)
		defer stopDeadline()
	}
	for {
		pauseReached, stopTimer := pauseBoundaryChannel(next)
		select {
		case err := <-waitDone:
			stopTimer()
			return err
		case <-stopRequested:
			stopTimer()
			return finishInterruptedProbeCommand(command, waitDone)
		case <-deadlineReached:
			stopTimer()
			return finishDeadlineProbeCommand(command, waitDone)
		case <-pauseReached:
			stopTimer()
			ended, boundary, err := r.onPauseBoundary(command, waitDone)
			if ended {
				return err
			}
			next = boundary
		}
	}
}

func (r *ClaudeRunner) onPauseBoundary(command *exec.Cmd, waitDone <-chan error) (bool, time.Time, error) {
	active, next := r.evaluatePause(time.Now())
	if !active {
		return false, next, nil
	}
	if err, completed := completedProbeCommand(waitDone); completed {
		return true, next, err
	}
	warning := terminateProcessGroup(command.Process.Pid, stopTermGrace)
	<-waitDone
	return true, next, &ScheduledPauseError{ResumeAt: next, Started: true, CleanupWarning: warning}
}

func pauseBoundaryChannel(next time.Time) (<-chan time.Time, func()) {
	if next.IsZero() {
		return nil, func() {}
	}
	timer := time.NewTimer(time.Until(next))
	return timer.C, func() { timer.Stop() }
}

func (r *ClaudeRunner) evaluatePause(now time.Time) (bool, time.Time) {
	if r.pauseEvaluator != nil {
		return r.pauseEvaluator(now)
	}
	return r.config.PeakPause.Evaluate(now)
}
