package harnesslint

import (
	"errors"
	"os"
	"testing"
	"time"
)

const deadcodeTimeoutHelperEnv = "HARNESSLINT_DEADCODE_TIMEOUT_HELPER"

func init() {
	if os.Getenv(deadcodeTimeoutHelperEnv) != "1" {
		return
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestDeadcodeCommandStopsHungAnalysisWithinBound(t *testing.T) {
	previousTimeout := deadcodeCommandTimeout
	previousWaitDelay := deadcodeCommandWaitDelay
	deadcodeCommandTimeout = 100 * time.Millisecond
	deadcodeCommandWaitDelay = 100 * time.Millisecond
	t.Cleanup(func() {
		deadcodeCommandTimeout = previousTimeout
		deadcodeCommandWaitDelay = previousWaitDelay
	})
	t.Setenv(deadcodeTimeoutHelperEnv, "1")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	runner := realCommandRunner{deadcodePath: executable}
	started := time.Now()
	_, _, err = runDeadcodeConfig(t.TempDir(), "", deadcodeProductionConfigs[0], runner)
	var timeoutFailure *qualityToolCommandTimeoutError
	if err == nil || !errors.As(err, &timeoutFailure) {
		t.Fatalf("deadcode timeout must return typed failure: %v", err)
	}
	if timeoutFailure.tool != deadcodeToolName {
		t.Fatalf("timeout tool = %q", timeoutFailure.tool)
	}
	if timeoutFailure.QualityToolClassification() != QualityToolEnvironmentFailure {
		t.Fatalf("timeout classification = %q", timeoutFailure.QualityToolClassification())
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("hung deadcode command exceeded bounded return: %s", elapsed)
	}
}
