package observationexec

import (
	"context"
	"errors"
	"testing"
)

func TestBoundedCommandDeadlineRemainsFailureAfterCleanChildExit(t *testing.T) {
	err := boundedCommandDeadlineError(nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline with clean child exit = %v, want context deadline exceeded", err)
	}
	if code := commandExitCode(err); code == 0 {
		t.Fatalf("deadline with clean child exit produced success exit code %d", code)
	}
}

func TestBoundedCommandDeadlinePreservesWaitFailure(t *testing.T) {
	waitErr := errors.New("wait failed")
	if err := boundedCommandDeadlineError(waitErr); !errors.Is(err, waitErr) {
		t.Fatalf("deadline wait error = %v want %v", err, waitErr)
	}
}
