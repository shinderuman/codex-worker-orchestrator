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
	SchemaVersion      int             `json:"schema_version"`
	EpisodeID          string          `json:"episode_id"`
	RepositoryIdentity string          `json:"repository_identity"`
	RootTaskRef        SemanticTaskRef `json:"root_task_ref"`
	ScopeRootTaskRef   SemanticTaskRef `json:"scope_root_task_ref"`
	OpenedByFindingID  string          `json:"opened_by_finding_id"`
	CreatedAt          time.Time       `json:"created_at"`
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
	Intent      FindingIntentKind      `json:"intent"`
	Reason      string                 `json:"reason,omitempty"`
	NextTaskRef *SemanticTaskRef       `json:"next_task_ref,omitempty"`
}

type episodeClosureWalker struct {
	refs                map[string]SemanticTaskRef
	canonical           map[string][]string
	episodeDependencies map[string][]string
	seen                map[string]bool
	visiting            map[string]bool
	order               []SemanticTaskRef
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
	lease, project, previous, err := s.blockingFindingContext(finding, head, target)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	revision, err := s.planBlockingRevision(finding, target, head, lease, project, previous)
	if err != nil {
		return FindingDispositionResult{}, err
	}
	if err := s.persistBlockingRevision(finding, target, revision, previous); err != nil {
		return FindingDispositionResult{}, err
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
	return blockingFindingResult(finding, disposition, revision, previous), nil
}

func (s *Store) blockingFindingContext(
	finding FindingRecord,
	head RepositoryControllerHead,
	target SemanticTaskRef,
) (ExecutionLease, ProjectSnapshot, *BlockerEpisodeRevision, error) {
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil || head.LiveLeaseID == "" {
		return ExecutionLease{}, ProjectSnapshot{}, nil,
			fmt.Errorf("blocking finding requires complete live controller authority")
	}
	lease, err := s.loadLease(head.LiveLeaseID)
	if err != nil {
		return ExecutionLease{}, ProjectSnapshot{}, nil, err
	}
	if lease.AttemptID != finding.SourceAttemptID || !lease.SemanticTaskRef.Equal(finding.SourceSemanticTaskRef) {
		return ExecutionLease{}, ProjectSnapshot{}, nil, fmt.Errorf("blocking finding source lease is stale")
	}
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return ExecutionLease{}, ProjectSnapshot{}, nil, err
	}
	previous, err := s.currentEpisodeRevision(head)
	if err != nil {
		return ExecutionLease{}, ProjectSnapshot{}, nil, err
	}
	binding, err := s.loadProblemBinding(finding.ProblemKey)
	if err != nil {
		return ExecutionLease{}, ProjectSnapshot{}, nil, err
	}
	if binding.TargetTaskRef != nil && !binding.TargetTaskRef.Equal(target) {
		return ExecutionLease{}, ProjectSnapshot{}, nil,
			fmt.Errorf("finding problem is already bound to a different semantic target")
	}
	return lease, project, previous, nil
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
	if err := validateBlockingAuthority(refs, finding, target); err != nil {
		return BlockerEpisodeRevision{}, err
	}
	revision, err := s.newBlockingRevision(finding, target, head, lease, project, previous)
	if err != nil {
		return BlockerEpisodeRevision{}, err
	}
	return completeBlockingRevision(revision, finding, target, refs, dependencies)
}

func validateBlockingAuthority(
	refs map[string]SemanticTaskRef,
	finding FindingRecord,
	target SemanticTaskRef,
) error {
	if ref, ok := refs[target.TaskPath]; !ok || !ref.Equal(target) {
		return fmt.Errorf("blocker target is not present in project dependency authority")
	}
	if ref, ok := refs[finding.SourceSemanticTaskRef.TaskPath]; !ok || !ref.Equal(finding.SourceSemanticTaskRef) {
		return fmt.Errorf("blocker source is not present in project dependency authority")
	}
	return nil
}

func (s *Store) newBlockingRevision(
	finding FindingRecord,
	target SemanticTaskRef,
	head RepositoryControllerHead,
	lease ExecutionLease,
	project ProjectSnapshot,
	previous *BlockerEpisodeRevision,
) (BlockerEpisodeRevision, error) {
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
		return revision, nil
	}
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
	return revision, nil
}

func completeBlockingRevision(
	revision BlockerEpisodeRevision,
	finding FindingRecord,
	target SemanticTaskRef,
	refs map[string]SemanticTaskRef,
	dependencies map[string][]string,
) (BlockerEpisodeRevision, error) {
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

func (s *Store) persistBlockingRevision(
	finding FindingRecord,
	target SemanticTaskRef,
	revision BlockerEpisodeRevision,
	previous *BlockerEpisodeRevision,
) error {
	if err := s.writeEpisodeRevision(revision); err != nil {
		return err
	}
	if previous == nil {
		record := BlockerEpisodeRecord{
			SchemaVersion:      controllerSchemaVersion,
			EpisodeID:          revision.EpisodeID,
			RepositoryIdentity: s.identity.LineageID,
			RootTaskRef:        revision.RootTaskRef,
			ScopeRootTaskRef:   revision.ScopeRootTaskRef,
			OpenedByFindingID:  finding.FindingID,
			CreatedAt:          revision.CreatedAt,
		}
		if err := s.writeEpisodeRecord(record); err != nil {
			return err
		}
	}
	binding, err := s.bindFindingTarget(finding, target)
	if err != nil {
		return err
	}
	if binding.TargetTaskRef == nil || !binding.TargetTaskRef.Equal(target) {
		return fmt.Errorf("finding target binding changed during blocker planning")
	}
	return nil
}

func blockingFindingResult(
	finding FindingRecord,
	disposition FindingDisposition,
	revision BlockerEpisodeRevision,
	previous *BlockerEpisodeRevision,
) FindingDispositionResult {
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
	}
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
	for path := range adjacency {
		if err := visitEpisodeDependency(path, adjacency, states); err != nil {
			return err
		}
	}
	return nil
}

func visitEpisodeDependency(path string, adjacency map[string][]string, states map[string]uint8) error {
	switch states[path] {
	case 1:
		return fmt.Errorf("blocker dependency cycle detected at %s", path)
	case 2:
		return nil
	}
	states[path] = 1
	for _, dependency := range adjacency[path] {
		if err := visitEpisodeDependency(dependency, adjacency, states); err != nil {
			return err
		}
	}
	states[path] = 2
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
	walker := episodeClosureWalker{
		refs:                refs,
		canonical:           canonical,
		episodeDependencies: buildEpisodeDependencies(edges, satisfied),
		seen:                map[string]bool{},
		visiting:            map[string]bool{},
	}
	if err := walker.walk(root.TaskPath); err != nil {
		return nil, nil, err
	}
	closure := append([]SemanticTaskRef(nil), walker.order...)
	sort.Slice(closure, func(i, j int) bool { return closure[i].TaskPath < closure[j].TaskPath })
	return closure, walker.order, nil
}

func buildEpisodeDependencies(
	edges []EpisodeDependencyEdge,
	satisfied []SemanticTaskRef,
) map[string][]string {
	dependencies := map[string][]string{}
	for _, edge := range edges {
		if taskRefIn(satisfied, edge.DependencyTaskRef) {
			continue
		}
		dependencies[edge.BlockedTaskRef.TaskPath] = appendUniqueString(
			dependencies[edge.BlockedTaskRef.TaskPath],
			edge.DependencyTaskRef.TaskPath,
		)
	}
	return dependencies
}

func (w *episodeClosureWalker) walk(path string) error {
	if w.seen[path] {
		return nil
	}
	if w.visiting[path] {
		return fmt.Errorf("blocker episode closure contains a cycle at %s", path)
	}
	ref, ok := w.refs[path]
	if !ok {
		return fmt.Errorf("blocker episode dependency %s is outside project authority", path)
	}
	w.visiting[path] = true
	dependencies := append([]string(nil), w.canonical[path]...)
	dependencies = append(dependencies, w.episodeDependencies[path]...)
	sort.Strings(dependencies)
	for _, dependency := range uniqueStrings(dependencies) {
		if err := w.walk(dependency); err != nil {
			return err
		}
	}
	w.visiting[path] = false
	w.seen[path] = true
	w.order = append(w.order, ref)
	return nil
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
	head, err := s.LoadHead()
	if err != nil {
		return EpisodeScheduleResult{}, err
	}
	if err := s.ensureTerminalMetadataFinalized(head); err != nil {
		return EpisodeScheduleResult{}, err
	}
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
