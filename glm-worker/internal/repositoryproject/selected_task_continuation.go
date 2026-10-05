package repositoryproject

type SelectedTaskContinuationInput struct {
	Task                 string
	RequiredAction       string
	StartRequired        bool
	TemporaryBlockReason string
	Interrupted          bool
}

func SelectedTaskParentRequestProjection(input SelectedTaskContinuationInput) ParentRequestCompletionProjection {
	if input.Task == "" {
		return ParentRequestCompletionProjection{Continuation: UnknownContinuation(ReasonActiveTaskUnresolved)}
	}
	requiredAction := input.RequiredAction
	if input.StartRequired {
		requiredAction = ActionStart
	}
	continuation := Continuation{
		State:          ContinuationContinueNow,
		Task:           input.Task,
		RequiredAction: requiredAction,
		Reason:         ReasonCurrentTask,
	}
	if input.TemporaryBlockReason != "" {
		continuation.State = ContinuationBlocked
		continuation.Reason = input.TemporaryBlockReason
	}
	if input.Interrupted {
		continuation.State = ContinuationExplicitStop
		continuation.Reason = ReasonUserInterruption
	}
	return ParentRequestCompletionProjection{Continuation: continuation}
}
