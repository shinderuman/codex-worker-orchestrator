package controller

import "fmt"

type episodeScheduleAuthority struct {
	project      ProjectSnapshot
	refs         map[string]SemanticTaskRef
	dependencies map[string][]string
}

func (s *Store) scheduleEpisodeAgainstProject(revision BlockerEpisodeRevision) (EpisodeScheduleResult, error) {
	head, err := s.LoadHead()
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if err := s.ensureTerminalMetadataFinalized(head); err != nil {
		return EpisodeScheduleResult{}, err
	}
	project, err := s.LoadProjectSnapshot(revision.ProjectSnapshotID)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	refs, dependencies, err := s.projectDependencyAuthority(project)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	authority := episodeScheduleAuthority{project: project, refs: refs, dependencies: dependencies}
	if err := validateEpisodeScheduleAuthority(revision, authority); err != nil {
		return EpisodeScheduleResult{}, err
	}
	return scheduleEpisodeWithAuthority(revision, authority), nil
}

func validateEpisodeScheduleAuthority(revision BlockerEpisodeRevision, authority episodeScheduleAuthority) error {
	if revision.ProjectSnapshotID != authority.project.SnapshotID {
		return fmt.Errorf("blocker episode project authority is stale")
	}
	for _, ref := range revision.AdmittedClosure {
		if taskPathSatisfied(revision.SatisfiedTaskRefs, ref.TaskPath) {
			continue
		}
		current, ok := authority.refs[ref.TaskPath]
		if !ok {
			return fmt.Errorf("unsatisfied blocker task %s is missing from project authority", ref.TaskPath)
		}
		if !current.Equal(ref) {
			return fmt.Errorf("unsatisfied blocker task %s has stale semantic authority", ref.TaskPath)
		}
	}
	return nil
}

func scheduleEpisodeWithAuthority(
	revision BlockerEpisodeRevision,
	authority episodeScheduleAuthority,
) EpisodeScheduleResult {
	if taskPathSatisfied(revision.SatisfiedTaskRefs, revision.ScopeRootTaskRef.TaskPath) {
		return EpisodeScheduleResult{Episode: revision, Intent: FindingIntentResumeRootTask}
	}
	if reason := episodeScheduleStateFailure(revision, authority.project); reason != "" {
		return EpisodeScheduleResult{Episode: revision, Intent: FindingIntentNoRunnable, Reason: reason}
	}
	if next := episodeResumeCandidate(revision, authority); next != nil {
		return EpisodeScheduleResult{Episode: revision, Intent: FindingIntentResumeBlockerTask, NextTaskRef: next}
	}
	if next := episodeFreshCandidate(revision, authority); next != nil {
		return EpisodeScheduleResult{Episode: revision, Intent: FindingIntentStartBlockerTask, NextTaskRef: next}
	}
	return EpisodeScheduleResult{
		Episode: revision,
		Intent:  FindingIntentNoRunnable,
		Reason:  "admitted blocker closure has unresolved work but no runnable task",
	}
}

func episodeScheduleStateFailure(revision BlockerEpisodeRevision, project ProjectSnapshot) string {
	next := stringSet(project.Next)
	blocked := stringSet(project.Blocked)
	for _, ref := range revision.AdmittedClosure {
		if taskPathSatisfied(revision.SatisfiedTaskRefs, ref.TaskPath) {
			continue
		}
		if blocked[ref.TaskPath] {
			return fmt.Sprintf("in-scope blocker task %s is in BLOCKED schedule state", ref.TaskPath)
		}
		if !next[ref.TaskPath] {
			return fmt.Sprintf("in-scope blocker task %s is outside executable NEXT schedule state", ref.TaskPath)
		}
	}
	return ""
}

func episodeResumeCandidate(
	revision BlockerEpisodeRevision,
	authority episodeScheduleAuthority,
) *SemanticTaskRef {
	scope := taskPathSet(revision.AdmittedClosure)
	for index := len(revision.ExecutionHistory) - 1; index >= 0; index-- {
		path := revision.ExecutionHistory[index].TaskPath
		if !scope[path] || taskPathSatisfied(revision.SatisfiedTaskRefs, path) {
			continue
		}
		candidate, ok := authority.refs[path]
		if !ok || !episodeTaskReadyAgainstProject(revision, candidate, authority.dependencies) {
			continue
		}
		return &candidate
	}
	return nil
}

func episodeFreshCandidate(
	revision BlockerEpisodeRevision,
	authority episodeScheduleAuthority,
) *SemanticTaskRef {
	scope := taskPathSet(revision.AdmittedClosure)
	executed := taskPathSet(revision.ExecutionHistory)
	for _, path := range authority.project.Next {
		if !scope[path] || executed[path] || taskPathSatisfied(revision.SatisfiedTaskRefs, path) {
			continue
		}
		candidate, ok := authority.refs[path]
		if !ok || !episodeTaskReadyAgainstProject(revision, candidate, authority.dependencies) {
			continue
		}
		return &candidate
	}
	return nil
}

func episodeTaskReadyAgainstProject(
	revision BlockerEpisodeRevision,
	task SemanticTaskRef,
	canonical map[string][]string,
) bool {
	if len(canonical[task.TaskPath]) != 0 {
		return false
	}
	for _, edge := range revision.DependencyEdges {
		if edge.BlockedTaskRef.TaskPath != task.TaskPath {
			continue
		}
		if !taskPathSatisfied(revision.SatisfiedTaskRefs, edge.DependencyTaskRef.TaskPath) {
			return false
		}
	}
	return true
}

func taskPathSatisfied(refs []SemanticTaskRef, path string) bool {
	for _, ref := range refs {
		if ref.TaskPath == path {
			return true
		}
	}
	return false
}

func taskPathSet(refs []SemanticTaskRef) map[string]bool {
	set := make(map[string]bool, len(refs))
	for _, ref := range refs {
		set[ref.TaskPath] = true
	}
	return set
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}
