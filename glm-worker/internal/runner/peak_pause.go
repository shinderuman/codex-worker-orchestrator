package runner

import (
	"fmt"
	"os/exec"
	"time"
)

type ScheduledPauseError struct {
	Phase          string
	TaskID         string
	RepoRoot       string
	ResumeAt       time.Time
	CleanupWarning string
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
	waitDone := make(chan error, 1)
	go func() { waitDone <- command.Wait() }()

	var stopRequested <-chan struct{}
	if r.stop != nil {
		stopRequested = r.stop.Requested()
	}
	var deadlineReached <-chan time.Time
	if probe {
		var stopDeadline func()
		deadlineReached, stopDeadline = probeDeadlineChannel(deadline)
		defer stopDeadline()
	}
	for {
		var pauseReached <-chan time.Time
		var pauseTimer *time.Timer
		if !next.IsZero() {
			pauseTimer = time.NewTimer(time.Until(next))
			pauseReached = pauseTimer.C
		}
		select {
		case err := <-waitDone:
			if pauseTimer != nil {
				pauseTimer.Stop()
			}
			return err
		case <-stopRequested:
			if pauseTimer != nil {
				pauseTimer.Stop()
			}
			return finishInterruptedProbeCommand(command, waitDone)
		case <-deadlineReached:
			if pauseTimer != nil {
				pauseTimer.Stop()
			}
			return finishDeadlineProbeCommand(command, waitDone)
		case <-pauseReached:
			if pauseTimer != nil {
				pauseTimer.Stop()
			}
			var paused bool
			paused, next = r.evaluatePause(time.Now())
			if !paused {
				continue
			}
			if err, completed := completedProbeCommand(waitDone); completed {
				return err
			}
			warning := terminateProcessGroup(command.Process.Pid, stopTermGrace)
			<-waitDone
			return &ScheduledPauseError{ResumeAt: next, CleanupWarning: warning}
		}
	}
}

func (r *ClaudeRunner) evaluatePause(now time.Time) (bool, time.Time) {
	if r.pauseEvaluator != nil {
		return r.pauseEvaluator(now)
	}
	return r.config.PeakPause.Evaluate(now)
}
