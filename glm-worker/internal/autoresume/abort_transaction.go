package autoresume

const AutoResumeAbortAutomationUpdateUnavailable = "automation_update_unavailable"

type AutoResumeFallbackPlan struct {
	TaskID          string
	RepoRoot        string
	ParentThreadID  string
	ResetAtRFC3339  string
	ResumeAtRFC3339 string
}

func AutoResumeFallbackPlanFromToken(token string) (AutoResumeFallbackPlan, error) {
	transaction, _, err := decodeAutoResumeTransaction(token)
	if err != nil {
		return AutoResumeFallbackPlan{}, err
	}
	return AutoResumeFallbackPlan{
		TaskID:          transaction.TaskID,
		RepoRoot:        transaction.RepoRoot,
		ParentThreadID:  transaction.ParentThreadID,
		ResetAtRFC3339:  transaction.ResetAtRFC3339,
		ResumeAtRFC3339: transaction.ResumeAtRFC3339,
	}, nil
}

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
