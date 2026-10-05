package observationexec

import (
	"errors"
	"testing"
)

func TestClassifyCompletedIsolatedGoTestOutcomeKeepsTargetFailureSource(t *testing.T) {
	outcome := classifyCompletedIsolatedGoTestOutcome(errors.New("test failed"))
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceTarget {
		t.Fatalf("completed target failure = %+v", outcome)
	}
}
