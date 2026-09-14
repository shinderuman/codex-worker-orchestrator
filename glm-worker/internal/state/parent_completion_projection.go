package state

func (s *StateStore) projectParentCompletionOutcome(kind string, resolved ParentReviewOpenState, terminal string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ParentReviewOpen = nil
		stats.recordParentOutcome(kind, "", resolved)
		stats.AcceptedRisk = resolved.Risk
		stats.CompletionTerminal = terminal
	})
}

func (s *StateStore) projectCurrentParentCompletionOutcome(outcome ParentCompletionOutcome) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.AcceptedRisk = outcome.Risk
		stats.CompletionTerminal = outcome.Terminal
	})
}
