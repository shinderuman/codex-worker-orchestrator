package autoresume

import "testing"

func TestRejectedCodexWakeCreateCleansUpOnlyTransactionOwnedAutomation(t *testing.T) {
	transaction := codexWakeTransaction{ExpectedAutomationID: "codex-5h-wake-expected"}
	mismatched := advanceCodexWakeCreate(
		transaction,
		"transaction",
		automationResponseFacts{AutomationID: "codex-5h-wake-unrelated"},
		"automation ID mismatch",
	)
	if mismatched.Status != CodexWakeStatusFailed || mismatched.Cleanup != nil {
		t.Fatalf("mismatched codex-wake cleanup = %#v", mismatched)
	}
	owned := advanceCodexWakeCreate(
		transaction,
		"transaction",
		automationResponseFacts{AutomationID: transaction.ExpectedAutomationID},
		"automation tool returned isError=true",
	)
	if owned.Cleanup == nil || owned.Cleanup.AutomationID != transaction.ExpectedAutomationID {
		t.Fatalf("owned codex-wake cleanup = %#v", owned.Cleanup)
	}
}

func TestInvalidUpdateResponseIsNotRetried(t *testing.T) {
	transaction := codexWakeTransaction{
		Version:              codexWakeTransactionVersion,
		Stage:                stageUpdateOneShot,
		ExpectedAutomationID: "codex-5h-wake-expected",
		Attempt:              1,
	}
	output := advanceCodexWakeUpdate(
		transaction,
		"transaction",
		automationResponseFacts{},
		"malformed automation tool response",
		"",
		"",
		fixedDBReader(nil, nil),
	)
	if output.Status != CodexWakeStatusFailed || output.Attempt != 1 || output.Write != nil {
		t.Fatalf("invalid response was retried: %#v", output)
	}
}
