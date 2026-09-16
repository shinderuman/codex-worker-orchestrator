package autoresume

const AutoResumeAbortAutomationUpdateUnavailable = "automation_update_unavailable"

func AbortAutoResumeTransaction(token, reason string) AutoResumeOutput {
	transaction, transactionID, err := decodeAutoResumeTransaction(token)
	if err != nil {
		authority := autoResumeAuthoritySpec()
		return AutoResumeOutput{Version: autoResumeTransactionVersion, Status: AutoResumeStatusFailed, Authority: &authority, Reason: err.Error()}
	}
	if reason != AutoResumeAbortAutomationUpdateUnavailable {
		return autoResumeFailureOutput(transaction, transactionID, "unsupported auto-resume abort reason")
	}
	return autoResumeFailureOutput(
		transaction,
		transactionID,
		"required automation_update capability is unavailable in the current parent thread; auto-resume transaction stopped without further external writes",
	)
}
