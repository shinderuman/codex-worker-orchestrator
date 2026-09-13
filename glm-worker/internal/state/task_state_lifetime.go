package state

type taskBoundStateLifetime uint8

type taskBoundStatePolicy struct {
	name     string
	lifetime taskBoundStateLifetime
}

const (
	taskBoundStateFreshTaskClear taskBoundStateLifetime = iota
	taskBoundStateTaskIDBound
)

const (
	QualitySurfaceBaselineStateFile      = "quality-surface-baseline"
	RepositoryHarnessActivationStateFile = "repository-harness"
	InstructionSurfaceBaselineStateFile  = "instruction-surface-baseline-v1"
)

var taskBoundStatePolicies = []taskBoundStatePolicy{
	{name: "task.status", lifetime: taskBoundStateFreshTaskClear},
	{name: parentCodexIdentityFile, lifetime: taskBoundStateFreshTaskClear},
	{name: "isolation.policy", lifetime: taskBoundStateFreshTaskClear},
	{name: "active-task", lifetime: taskBoundStateFreshTaskClear},
	{name: "last-request", lifetime: taskBoundStateFreshTaskClear},
	{name: "last-decision", lifetime: taskBoundStateFreshTaskClear},
	{name: "pending-decision", lifetime: taskBoundStateFreshTaskClear},
	{name: parentReviewStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: "last-review", lifetime: taskBoundStateFreshTaskClear},
	{name: "baseline-status", lifetime: taskBoundStateFreshTaskClear},
	{name: "baseline-head", lifetime: taskBoundStateFreshTaskClear},
	{name: "baseline-worktree.patch", lifetime: taskBoundStateFreshTaskClear},
	{name: "baseline-index.patch", lifetime: taskBoundStateFreshTaskClear},
	{name: baselineUntrackedFile, lifetime: taskBoundStateFreshTaskClear},
	{name: "accepted-fix-scope.json", lifetime: taskBoundStateFreshTaskClear},
	{name: ExecutionMilestonesStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: ResultCorrectionStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: stopWorktreePatchFile, lifetime: taskBoundStateFreshTaskClear},
	{name: stopIndexPatchFile, lifetime: taskBoundStateFreshTaskClear},
	{name: isolationStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: resumeStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: workerEndSnapshotFile, lifetime: taskBoundStateFreshTaskClear},
	{name: reviewStartSnapshotFile, lifetime: taskBoundStateFreshTaskClear},
	{name: reportOnlyStartSnapshotFile, lifetime: taskBoundStateFreshTaskClear},
	{name: poCStartSnapshotFile, lifetime: taskBoundStateFreshTaskClear},
	{name: snapshotComparisonFile, lifetime: taskBoundStateFreshTaskClear},
	{name: guardRepairStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: runtimeInstallEvidenceFile, lifetime: taskBoundStateFreshTaskClear},
	{name: QualitySurfaceBaselineStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: RepositoryHarnessActivationStateFile, lifetime: taskBoundStateFreshTaskClear},
	{name: InstructionSurfaceBaselineStateFile, lifetime: taskBoundStateTaskIDBound},
}

func newTaskTransitionStateFileNames() []string {
	names := make([]string, 0, len(taskBoundStatePolicies))
	for _, policy := range taskBoundStatePolicies {
		if policy.lifetime == taskBoundStateFreshTaskClear {
			names = append(names, policy.name)
		}
	}
	return names
}
