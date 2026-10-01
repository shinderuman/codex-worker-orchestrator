package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
)

type EpisodeState string

type EpisodeDependencyEdge struct {
	BlockedTaskRef    SemanticTaskRef `json:"blocked_task_ref"`
	DependencyTaskRef SemanticTaskRef `json:"dependency_task_ref"`
	FindingID         string          `json:"finding_id"`
}

type BlockerEpisodeRecord struct {
	SchemaVersion     int             `json:"schema_version"`
	EpisodeID         string          `json:"episode_id"`
	RepositoryIdentity string         `json:"repository_identity"`
	RootTaskRef       SemanticTaskRef `json:"root_task_ref"`
	ScopeRootTaskRef  SemanticTaskRef `json:"scope_root_task_ref"`
	OpenedByFindingID string          `json:"opened_by_finding_id"`
	CreatedAt         time.Time       `json:"created_at"`
}

type BlockerEpisodeRevision struct {
	SchemaVersion              int                     `json:"schema_version"`
	EpisodeID                  string                  `json:"episode_id"`
	Revision                   uint64                  `json:"revision"`
	RevisionID                 string                  `json:"revision_id"`
	PreviousRevisionID         string                  `json:"previous_revision_id,omitempty"`
	ProjectSnapshotID          string                  `json:"project_snapshot_id"`
	RootTaskRef                SemanticTaskRef         `json:"root_task_ref"`
	ScopeRootTaskRef           SemanticTaskRef         `json:"scope_root_task_ref"`
	TriggerFindingID           string                  `json:"trigger_finding_id,omitempty"`
	DependencyEdges            []EpisodeDependencyEdge `json:"dependency_edges"`
	SatisfiedTaskRefs          []SemanticTaskRef       `json:"satisfied_task_refs,omitempty"`
	ExecutionHistory           []SemanticTaskRef       `json:"execution_history,omitempty"`
	AdmittedClosure            []SemanticTaskRef       `json:"admitted_closure"`
	AdmittedOrder              []SemanticTaskRef       `json:"admitted_order"`
	SourceControllerGeneration uint64                  `json:"source_controller_generation"`
	SourceAttemptID            string                  `json:"source_attempt_id"`
	SourceLeaseID              string                  `json:"source_lease_id"`
	SourceWorkspaceID          string                  `json:"source_workspace_id"`
	SourceWorkspaceSnapshotID  string                  `json:"source_workspace_snapshot_id"`
	State                      EpisodeState            `json:"state"`
	CreatedAt                  time.Time               `json:"created_at"`
}

type EpisodeScheduleResult struct {
	Episode     BlockerEpisodeRevision `json:"episode"`
	Intent      FindingIntentKind       `json:"intent"`
	Reason      string                  `json:"reason,omitempty"`
	NextTaskRef *SemanticTaskRef        `json:"next_task_ref,omitempty"`
}

const (
	EpisodeStatePlanned      EpisodeState = "planned"
	EpisodeStateReplanning   EpisodeState = "replanning"
	EpisodeStateResumingRoot EpisodeState = "resuming-root"
	EpisodeStateClosed       EpisodeState = "closed"

	FindingIntentResumeRootTask FindingIntentKind = "resume-root-task"
)

func (s *Store) resolveBlockingFinding(
	finding FindingRecord,
	head RepositoryControllerHead,
	decision FindingDecision,
	target SemanticTaskRef,
) (FindingDispositionResult, error) {
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil || head.LiveLeaseID == "" {
		return FindingDispositionResult{}, fmt.Errorf("blocking finding requires complete live controller authority")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if lease.AttemptID != finding.SourceAttemptID || !lease.SemanticTaskRef.Equal(finding.SourceSemanticTaskRef) {
		return FindingDispositionResult{}, fmt.Errorf("blocking finding source lease is stale")
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	previous, err := s.currentEpisodeRevision(head)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	binding, err := s.loadProblemBinding(finding.ProblemKey)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if binding.TargetTaskRef != nil && !binding.TargetTaskRef.Equal(target) {
		return FindingDispositionResult{}, fmt.Errorf("finding problem is already bound to a different semantic target")
	}
	revision, err := s.planBlockingRevision(finding, decision, target, head, lease, project, previous)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if err := s.writeEpisodeRevision(revision); err != nil {
		return FindingDispositionResult{}, err
	}
	if previous == nil {
		record := BlockerEpisodeRecord{
			SchemaVersion:       controllerSchemaVersion,
			EpisodeID:           revision.EpisodeID,
			RepositoryIdentity:  s.identity.LineageID,
			RootTaskRef:         revision.RootTaskRef,
			ScopeRootTaskRef:    revision.ScopeRootTaskRef,
			OpenedByFindingID:   finding.FindingID,
			CreatedAt:           revision.CreatedAt,
		}
		if err := s.writeEpisodeRecord(record); err != nil {
			return FindingDispositionResult{}, err
		}
	}
	binding, err = s.bindFindingTarget(finding, target)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if binding.TargetTaskRef == nil || !binding.TargetTaskRef.Equal(target) {
		return FindingDispositionResult{}, fmt.Errorf("finding target binding changed during blocker planning")
	}
	disposition, err := s.commitFindingDisposition(finding, head.ControllerGeneration, FindingDisposition{
		Kind:             FindingDispositionIndependentBlocking,
		TargetTaskRef:    &target,
		BlockingBoundary: decision.BlockingBoundary,
		EpisodeID:        revision.EpisodeID,
		EpisodeRevision:  revision.Revision,
	})
	if err != nil {
		return FindingDispositionResult{}, err
	}
	schedule := scheduleEpisodeRevision(revision)
	intent := FindingIntentOpenBlockerEpisode
	if previous != nil {
		intent = FindingIntentReplanBlockerEpisode
	}
	if schedule.Intent == FindingIntentNoRunnable {
		intent = FindingIntentNoRunnable
	}
	return FindingDispositionResult{
		Finding:     finding,
		Disposition: &disposition,
		Intent:      intent,
		Reason:      schedule.Reason,
		Episode:     &revision,
		NextTaskRef: schedule.NextTaskRef,
	}, nil
}

func (s *Store) currentEpisodeRevision(head RepositoryControllerHead) (*BlockerEpisodeRevision, error) {
	if head.ActiveEpisodeID == "" {
		if head.ActiveEpisodeRevision != 0 {
			return nil, fmt.Errorf("controller has episode revision without episode identity")
		}
		return nil, nil
	}
	if head.ActiveEpisodeRevision == 0 {
		return nil, fmt.Errorf("controller has episode identity without revision")
	}
	revision, err := s.LoadEpisodeRevision(head.ActiveEpisodeID, head.ActiveEpisodeRevision)
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

func (s *Store) planBlockingRevision(
	finding FindingRecord,
	decision FindingDecision,
	target SemanticTaskRef,
	head RepositoryControllerHead,
	lease ExecutionLease,
	project ProjectSnapshot,
	previous *BlockerEpisodeRevision,
) (BlockerEpisodeRevision, error) {
	refs, dependencies, err := s.projectDependencyAuthority(project)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	if _, ok := refs[target.TaskPath]; !ok || !refs[target.TaskPath].Equal(target) {
		return BlockerEpisodeRevision{}, fmt.Errorf("blocker target is not present in project dependency authority")
	}
	if _, ok := refs[finding.SourceSemanticTaskRef.TaskPath]; !ok {
		return BlockerEpisodeRevision{}, fmt.Errorf("blocker source is not present in project dependency authority")
	}

	revision := BlockerEpisodeRevision{
		SchemaVersion:              controllerSchemaVersion,
		ProjectSnapshotID:          project.SnapshotID,
		RootTaskRef:                *head.RootTaskRef,
		TriggerFindingID:           finding.FindingID,
		SourceControllerGeneration: head.ControllerGeneration,
		SourceAttemptID:            finding.SourceAttemptID,
		SourceLeaseID:              lease.LeaseID,
		SourceWorkspaceID:          lease.WorkspaceID,
		SourceWorkspaceSnapshotID:  finding.SourceWorkspaceSnapshotID,
		CreatedAt:                  time.Now().UTC(),
	}
	if previous == nil {
		revision.EpisodeID = blockerEpisodeID(s.identity.LineageID, *head.RootTaskRef, finding, target)
		revision.Revision = 1
		revision.ScopeRootTaskRef = target
		revision.State = EpisodeStatePlanned
	} else {
		if previous.State == EpisodeStateClosed {
			return BlockerEpisodeRevision{}, fmt.Errorf("closed blocker episode cannot be replanned")
		}
		if !previous.RootTaskRef.Equal(*head.RootTaskRef) {
			return BlockerEpisodeRevision{}, fmt.Errorf("active blocker episode root does not match controller root")
		}
		revision.EpisodeID = previous.EpisodeID
		revision.Revision = previous.Revision + 1
		revision.PreviousRevisionID = previous.RevisionID
		revision.ScopeRootTaskRef = previous.ScopeRootTaskRef
		revision.DependencyEdges = append([]EpisodeDependencyEdge(nil), previous.DependencyEdges...)
		revision.SatisfiedTaskRefs = append([]SemanticTaskRef(nil), previous.SatisfiedTaskRefs...)
		revision.ExecutionHistory = append([]SemanticTaskRef(nil), previous.ExecutionHistory...)
		revision.State = EpisodeStateReplanning
	}
	revision.ExecutionHistory = appendTaskRefUnique(revision.ExecutionHistory, finding.SourceSemanticTaskRef)
	edge := EpisodeDependencyEdge{
		BlockedTaskRef:    finding.SourceSemanticTaskRef,
		DependencyTaskRef: target,
		FindingID:         finding.FindingID,
	}
	if !hasEpisodeEdge(revision.DependencyEdges, edge) {
		revision.DependencyEdges = append(revision.DependencyEdges, edge)
	}
	if err := validateEpisodeAcyclic(dependencies, revision.DependencyEdges, revision.SatisfiedTaskRefs); err != nil {
		return BlockerEpisodeRevision{}, err
	}
	closure, order, err := buildEpisodeClosure(
		revision.ScopeRootTaskRef,
		refs,
		dependencies,
		revision.DependencyEdges,
		revision.SatisfiedTaskRefs,
	)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	revision.AdmittedClosure = closure
	revision.AdmittedOrder = order
	revision.RevisionID = blockerEpisodeRevisionID(revision)
	return revision, nil
}

func (s *Store) projectDependencyAuthority(
	project ProjectSnapshot,
) (map[string]SemanticTaskRef, map[string][]string, error) {
	_, refs, contents, err := committedTaskCorpus(s.identity.PrimaryRoot, project.HeadOID)
	if err != nil {
		return nil, nil, err
	}
	paths := make([]string, 0, len(refs))
	byPath := make(map[string]SemanticTaskRef, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.TaskPath)
		byPath[ref.TaskPath] = ref
	}
	graph, err := repositoryproject.BuildTaskGraph(paths, contents)
	if err != nil {
		return nil, nil, err
	}
	dependencies := make(map[string][]string, len(refs))
	for _, dependency := range graph.Dependencies() {
		dependencies[dependency.Task] = append([]string(nil), dependency.Outstanding...)
	}
	return byPath, dependencies, nil
}

func validateEpisodeAcyclic(
	canonical map[string][]string,
	edges []EpisodeDependencyEdge,
	satisfied []SemanticTaskRef,
) error {
	adjacency := cloneDependencies(canonical)
	for _, edge := range edges {
		if taskRefIn(satisfied, edge.DependencyTaskRef) {
			continue
		}
		adjacency[edge.BlockedTaskRef.TaskPath] = appendUniqueString(
			adjacency[edge.BlockedTaskRef.TaskPath],
			edge.DependencyTaskRef.TaskPath,
		)
	}
	states := map[string]uint8{}
	var visit func(string) error
	visit = func(path string) error {
		switch states[path] {
		case 1:
			return fmt.Errorf("blocker dependency cycle detected at %s", path)
		case 2:
			return nil
		}
		states[path] = 1
		for _, dependency := range adjacency[path] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		states[path] = 2
		return nil
	}
	for path := range adjacency {
		if err := visit(path); err != nil {
			return err
		}
	}
	return nil
}

func buildEpisodeClosure(
	root SemanticTaskRef,
	refs map[string]SemanticTaskRef,
	canonical map[string][]string,
	edges []EpisodeDependencyEdge,
	satisfied []SemanticTaskRef,
) ([]SemanticTaskRef, []SemanticTaskRef, error) {
	if root.Empty() {
		return nil, nil, fmt.Errorf("blocker episode scope root is empty")
	}
	if _, ok := refs[root.TaskPath]; !ok {
		return nil, nil, fmt.Errorf("blocker episode scope root is outside project authority")
	}
	episodeDependencies := map[string][]string{}
	for _, edge := range edges {
		if taskRefIn(satisfied, edge.DependencyTaskRef) {
			continue
		}
		episodeDependencies[edge.BlockedTaskRef.TaskPath] = appendUniqueString(
			episodeDependencies[edge.BlockedTaskRef.TaskPath],
			edge.DependencyTaskRef.TaskPath,
		)
	}
	seen := map[string]bool{}
	visiting := map[string]bool{}
	order := []SemanticTaskRef{}
	var walk func(string) error
	walk = func(path string) error {
		if seen[path] {
			return nil
		}
		if visiting[path] {
			return fmt.Errorf("blocker episode closure contains a cycle at %s", path)
		}
		ref, ok := refs[path]
		if !ok {
			return fmt.Errorf("blocker episode dependency %s is outside project authority", path)
		}
		visiting[path] = true
		dependencies := append([]string(nil), canonical[path]...)
		dependencies = append(dependencies, episodeDependencies[path]...)
		sort.Strings(dependencies)
		dependencies = uniqueStrings(dependencies)
		for _, dependency := range dependencies {
			if err := walk(dependency); err != nil {
				return err
			}
		}
		visiting[path] = false
		seen[path] = true
		order = append(order, ref)
		return nil
	}
	if err := walk(root.TaskPath); err != nil {
		return nil, nil, err
	}
	closure := append([]SemanticTaskRef(nil), order...)
	sort.Slice(closure, func(i, j int) bool { return closure[i].TaskPath < closure[j].TaskPath })
	return closure, order, nil
}

func scheduleEpisodeRevision(revision BlockerEpisodeRevision) EpisodeScheduleResult {
	for _, candidate := range revision.AdmittedOrder {
		if taskRefIn(revision.SatisfiedTaskRefs, candidate) {
			continue
		}
		if episodeTaskReady(revision, candidate) {
			next := candidate
			intent := FindingIntentStartBlockerTask
			if taskRefIn(revision.ExecutionHistory, candidate) {
				intent = FindingIntentResumeBlockerTask
			}
			return EpisodeScheduleResult{Episode: revision, Intent: intent, NextTaskRef: &next}
		}
	}
	if taskRefIn(revision.SatisfiedTaskRefs, revision.ScopeRootTaskRef) {
		return EpisodeScheduleResult{Episode: revision, Intent: FindingIntentResumeRootTask}
	}
	return EpisodeScheduleResult{
		Episode: revision,
		Intent:  FindingIntentNoRunnable,
		Reason:  "admitted blocker closure has unresolved work but no runnable task",
	}
}

func episodeTaskReady(revision BlockerEpisodeRevision, task SemanticTaskRef) bool {
	for _, edge := range revision.DependencyEdges {
		if !edge.BlockedTaskRef.Equal(task) {
			continue
		}
		if !taskRefIn(revision.SatisfiedTaskRefs, edge.DependencyTaskRef) {
			return false
		}
	}
	orderIndex := map[string]int{}
	for index, ref := range revision.AdmittedOrder {
		orderIndex[ref.TaskPath] = index
	}
	candidateIndex := orderIndex[task.TaskPath]
	for _, dependency := range revision.AdmittedOrder[:candidateIndex] {
		if !taskRefIn(revision.SatisfiedTaskRefs, dependency) && dependsTransitively(revision, task, dependency) {
			return false
		}
	}
	return true
}

func dependsTransitively(revision BlockerEpisodeRevision, blocked, dependency SemanticTaskRef) bool {
	for _, edge := range revision.DependencyEdges {
		if edge.BlockedTaskRef.Equal(blocked) && edge.DependencyTaskRef.Equal(dependency) {
			return true
		}
	}
	return false
}

func (s *Store) ScheduleEpisode(episodeID string, revision uint64) (EpisodeScheduleResult, error) {
	record, err := s.LoadEpisodeRevision(episodeID, revision)
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	return scheduleEpisodeRevision(record), nil
}

func (s *Store) LoadEpisodeRevision(episodeID string, revision uint64) (BlockerEpisodeRevision, error) {
	var record BlockerEpisodeRevision
	if err := readJSON(s.episodeRevisionPath(episodeID, revision), &record); err != nil {
		return BlockerEpisodeRevision{}, err
	}
	if record.SchemaVersion != controllerSchemaVersion ||
		record.EpisodeID != episodeID ||
		record.Revision != revision ||
		record.RevisionID != blockerEpisodeRevisionID(record) {
		return BlockerEpisodeRevision{}, fmt.Errorf("blocker episode revision identity is invalid")
	}
	return record, nil
}

func (s *Store) writeEpisodeRecord(record BlockerEpisodeRecord) error {
	if err := s.ensureFindingStore(); err != nil {
		return err
	}
	path := s.episodeRecordPath(record.EpisodeID)
	if existing, err := s.loadEpisodeRecord(record.EpisodeID); err == nil {
		if !existing.RootTaskRef.Equal(record.RootTaskRef) ||
			!existing.ScopeRootTaskRef.Equal(record.ScopeRootTaskRef) ||
			existing.OpenedByFindingID != record.OpenedByFindingID {
			return fmt.Errorf("blocker episode identity collision")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeJSONAtomic(path, record)
}

func (s *Store) loadEpisodeRecord(episodeID string) (BlockerEpisodeRecord, error) {
	var record BlockerEpisodeRecord
	if err := readJSON(s.episodeRecordPath(episodeID), &record); err != nil {
		return BlockerEpisodeRecord{}, err
	}
	if record.SchemaVersion != controllerSchemaVersion ||
		record.RepositoryIdentity != s.identity.LineageID ||
		record.EpisodeID != episodeID {
		return BlockerEpisodeRecord{}, fmt.Errorf("blocker episode record identity is invalid")
	}
	return record, nil
}

func (s *Store) writeEpisodeRevision(record BlockerEpisodeRevision) error {
	if err := s.ensureFindingStore(); err != nil {
		return err
	}
	dir := filepath.Join(s.dir, "episodes", record.EpisodeID, "revisions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := s.episodeRevisionPath(record.EpisodeID, record.Revision)
	if existing, err := s.LoadEpisodeRevision(record.EpisodeID, record.Revision); err == nil {
		if existing.RevisionID != record.RevisionID {
			return fmt.Errorf("blocker episode revision collision")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeJSONAtomic(path, record)
}

func (s *Store) episodeRecordPath(episodeID string) string {
	return filepath.Join(s.dir, "episodes", episodeID, "episode.json")
}

func (s *Store) episodeRevisionPath(episodeID string, revision uint64) string {
	return filepath.Join(s.dir, "episodes", episodeID, "revisions", fmt.Sprintf("%020d.json", revision))
}

func blockerEpisodeID(repositoryIdentity string, root SemanticTaskRef, finding FindingRecord, target SemanticTaskRef) string {
	return digestStrings(
		"blocker-episode-v1",
		repositoryIdentity,
		root.TaskPath,
		root.ContractDigest,
		finding.CanonicalFindingID,
		target.TaskPath,
		target.ContractDigest,
	)
}

func blockerEpisodeRevisionID(record BlockerEpisodeRevision) string {
	parts := []string{
		"blocker-episode-revision-v1",
		record.EpisodeID,
		fmt.Sprintf("%d", record.Revision),
		record.PreviousRevisionID,
		record.ProjectSnapshotID,
		record.RootTaskRef.TaskPath,
		record.RootTaskRef.ContractDigest,
		record.ScopeRootTaskRef.TaskPath,
		record.ScopeRootTaskRef.ContractDigest,
		record.TriggerFindingID,
		fmt.Sprintf("%d", record.SourceControllerGeneration),
		record.SourceAttemptID,
		record.SourceLeaseID,
		record.SourceWorkspaceID,
		record.SourceWorkspaceSnapshotID,
		string(record.State),
	}
	for _, edge := range record.DependencyEdges {
		parts = append(parts,
			edge.BlockedTaskRef.TaskPath,
			edge.BlockedTaskRef.ContractDigest,
			edge.DependencyTaskRef.TaskPath,
			edge.DependencyTaskRef.ContractDigest,
			edge.FindingID,
		)
	}
	for _, ref := range record.SatisfiedTaskRefs {
		parts = append(parts, "satisfied", ref.TaskPath, ref.ContractDigest)
	}
	for _, ref := range record.ExecutionHistory {
		parts = append(parts, "executed", ref.TaskPath, ref.ContractDigest)
	}
	for _, ref := range record.AdmittedClosure {
		parts = append(parts, "closure", ref.TaskPath, ref.ContractDigest)
	}
	for _, ref := range record.AdmittedOrder {
		parts = append(parts, "order", ref.TaskPath, ref.ContractDigest)
	}
	return digestStrings(parts...)
}

func hasEpisodeEdge(edges []EpisodeDependencyEdge, candidate EpisodeDependencyEdge) bool {
	for _, edge := range edges {
		if edge.BlockedTaskRef.Equal(candidate.BlockedTaskRef) &&
			edge.DependencyTaskRef.Equal(candidate.DependencyTaskRef) {
			return true
		}
	}
	return false
}

func appendTaskRefUnique(refs []SemanticTaskRef, candidate SemanticTaskRef) []SemanticTaskRef {
	if taskRefIn(refs, candidate) {
		return refs
	}
	return append(refs, candidate)
}

func taskRefIn(refs []SemanticTaskRef, candidate SemanticTaskRef) bool {
	for _, ref := range refs {
		if ref.Equal(candidate) {
			return true
		}
	}
	return false
}

func cloneDependencies(input map[string][]string) map[string][]string {
	result := make(map[string][]string, len(input))
	for key, values := range input {
		result[key] = append([]string(nil), values...)
	}
	return result
}

func appendUniqueString(values []string, candidate string) []string {
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
