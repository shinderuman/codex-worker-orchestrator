package controller

import (
	"encoding/json"
	"fmt"
	"time"
)

type EvidenceExportRequest struct {
	TaskPath  string
	AttemptID string
}

type EvidenceExportTarget struct {
	Kind      string          `json:"kind"`
	Task      SemanticTaskRef `json:"task"`
	AttemptID string          `json:"attempt_id,omitempty"`
	EpisodeID string          `json:"episode_id,omitempty"`
	Live      bool            `json:"live"`
}

type EvidenceExportEntry struct {
	Path             string    `json:"path"`
	Source           string    `json:"source"`
	SHA256           string    `json:"sha256"`
	Bytes            int64     `json:"bytes"`
	CollectedAt      time.Time `json:"collected_at,omitempty"`
	InProgress       bool      `json:"in_progress,omitempty"`
	Changing         bool      `json:"changing,omitempty"`
	TrailingFragment bool      `json:"trailing_fragment,omitempty"`
	Records          int       `json:"records,omitempty"`
	Symlink          string    `json:"symlink,omitempty"`
	Missing          bool      `json:"missing,omitempty"`
	Unreadable       string    `json:"unreadable,omitempty"`
}

type EvidenceExportBundleSummary struct {
	Kind                string `json:"kind"`
	RootDigest          string `json:"root_digest"`
	EvidenceGraphDigest string `json:"evidence_graph_digest"`
	Objects             int    `json:"objects"`
}

type EvidenceExportSealedSection struct {
	Status        string                       `json:"status"`
	AbsenceReason string                       `json:"absence_reason,omitempty"`
	TaskBundle    *EvidenceExportBundleSummary `json:"task_bundle,omitempty"`
	EpisodeBundle *EvidenceExportBundleSummary `json:"episode_bundle,omitempty"`
}

type EvidenceExportLiveSection struct {
	Status         string   `json:"status"`
	AbsenceReason  string   `json:"absence_reason,omitempty"`
	Coverage       string   `json:"coverage"`
	RuntimeTaskID  string   `json:"runtime_task_id,omitempty"`
	WorkspaceID    string   `json:"workspace_id,omitempty"`
	SessionIDs     []string `json:"session_ids,omitempty"`
	ParentThreadID string   `json:"parent_thread_id,omitempty"`
	Missing        []string `json:"missing,omitempty"`
	Unreadable     []string `json:"unreadable,omitempty"`
}

type EvidenceExportGitObservation struct {
	Snapshot            WorkspaceSnapshot      `json:"snapshot"`
	WorkspaceID         string                 `json:"workspace_id"`
	ObjectArchiveDigest string                 `json:"object_archive_digest,omitempty"`
	ObjectArchiveRoots  []GitObjectArchiveRoot `json:"object_archive_roots,omitempty"`
}

type EvidenceExportControllerObservation struct {
	GenerationBefore             uint64           `json:"generation_before"`
	GenerationAfter              uint64           `json:"generation_after"`
	LiveAttemptBefore            string           `json:"live_attempt_before,omitempty"`
	LiveAttemptAfter             string           `json:"live_attempt_after,omitempty"`
	LiveLeaseBefore              string           `json:"live_lease_before,omitempty"`
	LiveLeaseAfter               string           `json:"live_lease_after,omitempty"`
	ExecutionTaskBefore          *SemanticTaskRef `json:"execution_task_before,omitempty"`
	ExecutionTaskAfter           *SemanticTaskRef `json:"execution_task_after,omitempty"`
	AuthorityChangedDuringExport bool             `json:"authority_changed_during_export"`
}

type EvidenceExportManifest struct {
	SchemaVersion      int                                 `json:"schema_version"`
	Format             string                              `json:"format"`
	CreatedAt          time.Time                           `json:"created_at"`
	RepositoryIdentity string                              `json:"repository_identity"`
	Target             EvidenceExportTarget                `json:"target"`
	Controller         EvidenceExportControllerObservation `json:"controller"`
	Sealed             EvidenceExportSealedSection         `json:"sealed_section"`
	Live               EvidenceExportLiveSection           `json:"live_section"`
	Git                *EvidenceExportGitObservation       `json:"git_observation,omitempty"`
	Entries            []EvidenceExportEntry               `json:"entries"`
}

type EvidenceExportFile struct {
	Path string
	Data []byte
}

type EvidenceExport struct {
	Manifest EvidenceExportManifest
	Files    []EvidenceExportFile
}

type EvidenceExportResult struct {
	Target                       EvidenceExportTarget
	ArchiveName                  string
	ManifestDigest               string
	SealedStatus                 string
	LiveStatus                   string
	Coverage                     string
	AuthorityChangedDuringExport bool
	ControllerGenerationBefore   uint64
	ControllerGenerationAfter    uint64
}

type evidenceExportBuilder struct {
	store   *Store
	head    RepositoryControllerHead
	files   []EvidenceExportFile
	entries []EvidenceExportEntry
}

const evidenceExportFormat = "glm-controller-evidence-export-v1"

const (
	evidenceExportTargetExecutionTask = "execution-task"
	evidenceExportTargetTaskPath      = "task-path"
	evidenceExportTargetAttemptID     = "attempt-id"

	evidenceExportSealedPresent = "present"
	evidenceExportSealedAbsent  = "absent"

	evidenceExportLiveCollected = "collected"
	evidenceExportLiveAbsent    = "absent"

	evidenceExportCoveragePartial    = "partial"
	evidenceExportCoverageSealedOnly = "sealed-only"
	evidenceExportCoverageOpen       = "open"
)

func (s *Store) ExportEvidence(request EvidenceExportRequest) (EvidenceExport, EvidenceExportResult, error) {
	if request.TaskPath != "" && request.AttemptID != "" {
		return EvidenceExport{}, EvidenceExportResult{}, fmt.Errorf("evidence export target is ambiguous: specify either task_path or attempt_id")
	}
	head, err := s.LoadHead()
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	target, attempt, err := s.resolveEvidenceExportTarget(head, request)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	builder := &evidenceExportBuilder{store: s, head: head}
	sealed, err := builder.collectSealedSection(target)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	live, git, err := s.collectLiveSection(builder, target, attempt)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	headAfter, err := s.LoadHead()
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	observation := evidenceExportControllerObservation(head, headAfter)
	manifest, err := builder.finish(target, sealed, live, git, observation)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	manifestDigest := digestBytes(builder.files[0].Data)
	result := evidenceExportResult(target, manifest, observation, manifestDigest)
	return EvidenceExport{Manifest: manifest, Files: builder.files}, result, nil
}

func (s *Store) resolveEvidenceExportTarget(head RepositoryControllerHead, request EvidenceExportRequest) (EvidenceExportTarget, AttemptRecord, error) {
	switch {
	case request.AttemptID != "":
		return s.resolveAttemptExportTarget(head, request.AttemptID)
	case request.TaskPath != "":
		return s.resolveTaskPathExportTarget(head, request.TaskPath)
	default:
		return s.resolveExecutionExportTarget(head)
	}
}

func (s *Store) resolveAttemptExportTarget(head RepositoryControllerHead, attemptID string) (EvidenceExportTarget, AttemptRecord, error) {
	attempt, err := s.loadAttempt(attemptID)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export attempt %s is unknown to the controller: %w", attemptID, err)
	}
	live := head.LiveAttemptID == attemptID
	target := EvidenceExportTarget{
		Kind:      evidenceExportTargetAttemptID,
		Task:      attempt.SemanticTaskRef,
		AttemptID: attemptID,
		EpisodeID: attempt.EpisodeID,
		Live:      live,
	}
	if !live {
		return target, attempt, nil
	}
	if err := verifyEvidenceExportLiveAttemptBinding(head, attempt); err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	return target, attempt, nil
}

func (s *Store) resolveTaskPathExportTarget(head RepositoryControllerHead, taskPath string) (EvidenceExportTarget, AttemptRecord, error) {
	candidates, err := s.taskPathExportCandidates(head, taskPath)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	switch len(candidates) {
	case 0:
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export task path %q is unknown to the controller evidence authority", taskPath)
	case 1:
		task := candidates[0]
		if head.ExecutionTaskRef != nil && head.ExecutionTaskRef.Equal(task) && head.LiveAttemptID != "" {
			attempt, err := s.resolveLiveAttempt(head)
			if err != nil {
				return EvidenceExportTarget{}, AttemptRecord{}, err
			}
			return EvidenceExportTarget{Kind: evidenceExportTargetTaskPath, Task: task, AttemptID: attempt.AttemptID, EpisodeID: attempt.EpisodeID, Live: true}, attempt, nil
		}
		return EvidenceExportTarget{Kind: evidenceExportTargetTaskPath, Task: task}, AttemptRecord{}, nil
	default:
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export task path %q is ambiguous across %d contract authorities", taskPath, len(candidates))
	}
}

func (s *Store) resolveExecutionExportTarget(head RepositoryControllerHead) (EvidenceExportTarget, AttemptRecord, error) {
	if head.ExecutionTaskRef == nil {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export requires a current execution task or an explicit target")
	}
	task := *head.ExecutionTaskRef
	if head.LiveAttemptID == "" {
		return EvidenceExportTarget{Kind: evidenceExportTargetExecutionTask, Task: task}, AttemptRecord{}, nil
	}
	attempt, err := s.resolveLiveAttempt(head)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	return EvidenceExportTarget{
		Kind: evidenceExportTargetExecutionTask, Task: task,
		AttemptID: attempt.AttemptID, EpisodeID: attempt.EpisodeID, Live: true,
	}, attempt, nil
}

func (s *Store) resolveLiveAttempt(head RepositoryControllerHead) (AttemptRecord, error) {
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return AttemptRecord{}, fmt.Errorf("evidence export live attempt is unavailable: %w", err)
	}
	if err := verifyEvidenceExportLiveAttemptBinding(head, attempt); err != nil {
		return AttemptRecord{}, err
	}
	return attempt, nil
}

func verifyEvidenceExportLiveAttemptBinding(head RepositoryControllerHead, attempt AttemptRecord) error {
	if head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(attempt.SemanticTaskRef) {
		return fmt.Errorf("evidence export live attempt task binding is inconsistent with controller execution task")
	}
	return nil
}

func (s *Store) taskPathExportCandidates(head RepositoryControllerHead, taskPath string) ([]SemanticTaskRef, error) {
	seen := map[string]SemanticTaskRef{}
	if head.ExecutionTaskRef != nil && head.ExecutionTaskRef.TaskPath == taskPath {
		seen[semanticTaskEvidenceKey(*head.ExecutionTaskRef)] = *head.ExecutionTaskRef
	}
	if head.EvidenceHeadRef == nil {
		return semanticTaskRefValues(seen), nil
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return nil, err
	}
	for _, subject := range evidenceHead.TaskHeads {
		revision, err := s.LoadTaskIndexRevision(subject.RevisionRef)
		if err != nil {
			return nil, err
		}
		if revision.TaskRef.TaskPath == taskPath {
			seen[semanticTaskEvidenceKey(revision.TaskRef)] = revision.TaskRef
		}
	}
	return semanticTaskRefValues(seen), nil
}

func semanticTaskRefValues(refs map[string]SemanticTaskRef) []SemanticTaskRef {
	result := make([]SemanticTaskRef, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref)
	}
	return result
}

func (b *evidenceExportBuilder) collectSealedSection(target EvidenceExportTarget) (EvidenceExportSealedSection, error) {
	if b.head.EvidenceHeadRef == nil {
		return EvidenceExportSealedSection{Status: evidenceExportSealedAbsent, AbsenceReason: "controller evidence authority is not published yet"}, nil
	}
	evidenceHead, err := b.store.LoadEvidenceHead(*b.head.EvidenceHeadRef)
	if err != nil {
		return EvidenceExportSealedSection{}, err
	}
	subject, ok := evidenceSubjectHead(evidenceHead.TaskHeads, taskEvidenceSubjectID(target.Task))
	if !ok {
		return EvidenceExportSealedSection{Status: evidenceExportSealedAbsent, AbsenceReason: "task evidence root is not published yet"}, nil
	}
	taskBundle, err := b.store.BuildTaskEvidenceBundle(subject.RevisionRef)
	if err != nil {
		return EvidenceExportSealedSection{}, err
	}
	sealed := EvidenceExportSealedSection{
		Status:     evidenceExportSealedPresent,
		TaskBundle: evidenceExportBundleSummary(&taskBundle),
	}
	if err := b.addProjection("sealed/task-bundle.json", taskBundle); err != nil {
		return EvidenceExportSealedSection{}, err
	}
	episodeID := target.EpisodeID
	if episodeID == "" {
		episodeID, err = b.store.episodeIDForTaskRevision(subject.RevisionRef)
		if err != nil {
			return EvidenceExportSealedSection{}, err
		}
	}
	sealed.EpisodeBundle, err = b.appendEpisodeBundle(evidenceHead, episodeID)
	return sealed, err
}

func (b *evidenceExportBuilder) appendEpisodeBundle(evidenceHead EvidenceHead, episodeID string) (*EvidenceExportBundleSummary, error) {
	if episodeID == "" {
		return nil, nil
	}
	episodeSubject, ok := evidenceSubjectHead(evidenceHead.EpisodeHeads, episodeID)
	if !ok {
		return nil, nil
	}
	episodeBundle, err := b.store.BuildEpisodeEvidenceBundle(episodeSubject.RevisionRef)
	if err != nil {
		return nil, err
	}
	if err := b.addProjection("sealed/episode-bundle.json", episodeBundle); err != nil {
		return nil, err
	}
	return evidenceExportBundleSummary(&episodeBundle), nil
}

func (s *Store) episodeIDForTaskRevision(rootRef EvidenceObjectRef) (string, error) {
	current := rootRef
	for {
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return "", err
		}
		for _, sealRef := range revision.AttemptSeals {
			seal, err := s.LoadAttemptSeal(sealRef)
			if err != nil {
				return "", err
			}
			if seal.EpisodeID != "" {
				return seal.EpisodeID, nil
			}
		}
		if revision.PreviousRevision == nil {
			return "", nil
		}
		current = *revision.PreviousRevision
	}
}

func evidenceExportBundleSummary(bundle *EvidenceBundleProjection) *EvidenceExportBundleSummary {
	return &EvidenceExportBundleSummary{
		Kind:                bundle.Kind,
		RootDigest:          bundle.RootRef.Digest,
		EvidenceGraphDigest: bundle.EvidenceGraphDigest,
		Objects:             len(bundle.Objects),
	}
}

func (b *evidenceExportBuilder) addProjection(path string, bundle EvidenceBundleProjection) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return err
	}
	b.files = append(b.files, EvidenceExportFile{Path: path, Data: data})
	b.entries = append(b.entries, EvidenceExportEntry{
		Path: path, Source: evidenceExportSourceProjection,
		SHA256: digestBytes(data), Bytes: int64(len(data)),
	})
	return nil
}

func (b *evidenceExportBuilder) finish(
	target EvidenceExportTarget,
	sealed EvidenceExportSealedSection,
	live EvidenceExportLiveSection,
	git *EvidenceExportGitObservation,
	observation EvidenceExportControllerObservation,
) (EvidenceExportManifest, error) {
	if live.Status == evidenceExportLiveCollected {
		live.Coverage = evidenceExportCoveragePartial
	}
	manifest := EvidenceExportManifest{
		SchemaVersion:      evidenceSchemaVersion,
		Format:             evidenceExportFormat,
		CreatedAt:          time.Now().UTC(),
		RepositoryIdentity: b.store.identity.LineageID,
		Target:             target,
		Controller:         observation,
		Sealed:             sealed,
		Live:               live,
		Git:                git,
		Entries:            b.entries,
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return EvidenceExportManifest{}, err
	}
	b.files = append([]EvidenceExportFile{{Path: "manifest.json", Data: append(data, '\n')}}, b.files...)
	return manifest, nil
}

func evidenceExportControllerObservation(before, after RepositoryControllerHead) EvidenceExportControllerObservation {
	observation := EvidenceExportControllerObservation{
		GenerationBefore:  before.ControllerGeneration,
		GenerationAfter:   after.ControllerGeneration,
		LiveAttemptBefore: before.LiveAttemptID,
		LiveAttemptAfter:  after.LiveAttemptID,
		LiveLeaseBefore:   before.LiveLeaseID,
		LiveLeaseAfter:    after.LiveLeaseID,
	}
	if before.ExecutionTaskRef != nil {
		task := *before.ExecutionTaskRef
		observation.ExecutionTaskBefore = &task
	}
	if after.ExecutionTaskRef != nil {
		task := *after.ExecutionTaskRef
		observation.ExecutionTaskAfter = &task
	}
	observation.AuthorityChangedDuringExport = before.ControllerGeneration != after.ControllerGeneration ||
		before.LiveAttemptID != after.LiveAttemptID || before.LiveLeaseID != after.LiveLeaseID ||
		evidenceExportExecutionTaskChanged(before.ExecutionTaskRef, after.ExecutionTaskRef)
	return observation
}

func evidenceExportExecutionTaskChanged(before, after *SemanticTaskRef) bool {
	if before == nil || after == nil {
		return before != after
	}
	return !before.Equal(*after)
}

func evidenceExportResult(target EvidenceExportTarget, manifest EvidenceExportManifest, observation EvidenceExportControllerObservation, manifestDigest string) EvidenceExportResult {
	result := EvidenceExportResult{
		Target:                       target,
		SealedStatus:                 manifest.Sealed.Status,
		LiveStatus:                   manifest.Live.Status,
		AuthorityChangedDuringExport: observation.AuthorityChangedDuringExport,
		ControllerGenerationBefore:   observation.GenerationBefore,
		ControllerGenerationAfter:    observation.GenerationAfter,
	}
	switch {
	case manifest.Live.Status == evidenceExportLiveCollected:
		result.Coverage = evidenceExportCoveragePartial
	case manifest.Sealed.Status == evidenceExportSealedPresent:
		result.Coverage = evidenceExportCoverageSealedOnly
	default:
		result.Coverage = evidenceExportCoverageOpen
	}
	result.ArchiveName = evidenceExportArchiveName(target)
	result.ManifestDigest = manifestDigest
	return result
}

func evidenceExportArchiveName(target EvidenceExportTarget) string {
	if target.AttemptID != "" {
		return target.AttemptID + ".zip"
	}
	return "task-" + taskEvidenceSubjectID(target.Task) + ".zip"
}
