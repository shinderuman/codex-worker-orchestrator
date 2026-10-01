package controller

func IsPristine(head RepositoryControllerHead) bool {
	return head.Status == ControllerStatusActive &&
		head.ControllerGeneration == 0 &&
		head.ProjectSnapshotID == "" &&
		head.RootTaskRef == nil &&
		head.ExecutionTaskRef == nil &&
		head.ActiveEpisodeID == "" &&
		head.ActiveEpisodeRevision == 0 &&
		head.LiveAttemptID == "" &&
		head.LiveLeaseID == "" &&
		head.PendingTransitionID == "" &&
		head.FailureID == ""
}
