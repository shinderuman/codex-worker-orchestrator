package autoresume

import "testing"

func TestRejectedCreateResponseCleansUpOnlyTransactionOwnedAutomation(t *testing.T) {
	autoResumeTx := autoResumeTransaction{ExpectedAutomationID: "glm-worker-resume-expected"}
	mismatchedAutoResume := advanceAutoResumeCreate(
		autoResumeTx,
		"transaction",
		automationResponseFacts{AutomationID: "glm-worker-resume-unrelated"},
		"automation ID mismatch",
	)
	if mismatchedAutoResume.Status != AutoResumeStatusFailed || mismatchedAutoResume.Cleanup != nil {
		t.Fatalf("mismatched auto-resume cleanup = %#v", mismatchedAutoResume)
	}
	ownedAutoResume := advanceAutoResumeCreate(
		autoResumeTx,
		"transaction",
		automationResponseFacts{AutomationID: autoResumeTx.ExpectedAutomationID},
		"automation tool returned isError=true",
	)
	if ownedAutoResume.Cleanup == nil || ownedAutoResume.Cleanup.AutomationID != autoResumeTx.ExpectedAutomationID {
		t.Fatalf("owned auto-resume cleanup = %#v", ownedAutoResume.Cleanup)
	}

	codexWakeTx := codexWakeTransaction{ExpectedAutomationID: "codex-5h-wake-expected"}
	mismatchedCodexWake := advanceCodexWakeCreate(
		codexWakeTx,
		"transaction",
		automationResponseFacts{AutomationID: "codex-5h-wake-unrelated"},
		"automation ID mismatch",
	)
	if mismatchedCodexWake.Status != CodexWakeStatusFailed || mismatchedCodexWake.Cleanup != nil {
		t.Fatalf("mismatched codex-wake cleanup = %#v", mismatchedCodexWake)
	}
	ownedCodexWake := advanceCodexWakeCreate(
		codexWakeTx,
		"transaction",
		automationResponseFacts{AutomationID: codexWakeTx.ExpectedAutomationID},
		"automation tool returned isError=true",
	)
	if ownedCodexWake.Cleanup == nil || ownedCodexWake.Cleanup.AutomationID != codexWakeTx.ExpectedAutomationID {
		t.Fatalf("owned codex-wake cleanup = %#v", ownedCodexWake.Cleanup)
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
