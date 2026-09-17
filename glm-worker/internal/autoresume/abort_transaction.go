package autoresume

type AutoResumeFallbackPlan struct {
	TaskID          string
	RepoRoot        string
	ParentThreadID  string
	ResetAtRFC3339  string
	ResumeAtRFC3339 string
}

const AutoResumeAbortAutomationUpdateUnavailable = "automation_update_unavailable"

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
