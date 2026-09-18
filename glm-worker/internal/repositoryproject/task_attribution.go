package repositoryproject

type TaskAttribution struct {
	LifecycleTask   string `json:"lifecycle_task,omitempty"`
	AuthorityTask   string `json:"authority_task,omitempty"`
	ActiveTask      string `json:"active_task,omitempty"`
	Matches         bool   `json:"matches"`
	Handover        bool   `json:"handover"`
	Reason          string `json:"reason"`
	LegalNextAction string `json:"legal_next_action,omitempty"`
}

func DeriveTaskAttribution(lifecycleTask, activeTask string, continuation Continuation) TaskAttribution {
	attribution := TaskAttribution{
		LifecycleTask: lifecycleTask,
		ActiveTask:    activeTask,
	}
	if activeTask == "" {
		attribution.Reason = ReasonActiveTaskUnresolved
		return attribution
	}
	if lifecycleTask == "" {
		attribution.Reason = ReasonActiveTaskNotStarted
		if continuation.State == ContinuationContinueNow && continuation.Task == activeTask {
			attribution.LegalNextAction = ActionStart
		}
		return attribution
	}
	if lifecycleTask == activeTask {
		attribution.Matches = true
		attribution.Reason = ReasonCurrentTask
		attribution.LegalNextAction = continuation.RequiredAction
		return attribution
	}
	if continuation.State == ContinuationContinueNow &&
		continuation.Task == activeTask &&
		continuation.RequiredAction == ActionStart {
		attribution.Handover = true
		attribution.Reason = continuation.Reason
		attribution.LegalNextAction = ActionStart
		return attribution
	}
	attribution.Reason = ReasonActiveTaskMismatch
	return attribution
}

func BindTaskAuthority(attribution TaskAttribution, authorityTask string) TaskAttribution {
	attribution.AuthorityTask = authorityTask
	if attribution.Handover && authorityTask != attribution.LifecycleTask {
		attribution.Handover = false
		attribution.Reason = ReasonActiveTaskMismatch
		attribution.LegalNextAction = ""
	}
	return attribution
}
