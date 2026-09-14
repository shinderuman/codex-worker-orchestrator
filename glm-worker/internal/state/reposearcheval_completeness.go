package state

func BuildRepoSearchReportWithCompleteness(
	events []TaskEvents,
	statsByTask map[string]TaskStats,
	reviews map[string]TestImpactReviewSummary,
	incompleteTasks map[string]bool,
) RepoSearchReport {
	if len(incompleteTasks) == 0 {
		return BuildRepoSearchReport(events, statsByTask, reviews)
	}
	completeEvents := make([]TaskEvents, 0, len(events))
	for _, task := range events {
		if incompleteTasks[task.TaskID] {
			continue
		}
		completeEvents = append(completeEvents, task)
	}
	return BuildRepoSearchReport(completeEvents, statsByTask, reviews)
}
