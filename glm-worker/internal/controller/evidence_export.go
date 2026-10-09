package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type EvidenceExportRequest struct {
	TaskID string
}

type EvidenceExportTarget struct {
	Kind           string          `json:"kind"`
	Task           SemanticTaskRef `json:"task"`
	AttemptID      string          `json:"attempt_id,omitempty"`
	EpisodeID      string          `json:"episode_id,omitempty"`
	RuntimeTaskID  string          `json:"runtime_task_id,omitempty"`
	SelectionBasis string          `json:"selection_basis"`
	Live           bool            `json:"live"`
}

type EvidenceExportWindow struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	EndBasis string    `json:"end_basis"`
}

type EvidenceExportEntry struct {
	Path             string                    `json:"path"`
	Source           string                    `json:"source"`
	SHA256           string                    `json:"sha256"`
	Bytes            int64                     `json:"bytes"`
	CollectedAt      time.Time                 `json:"collected_at,omitempty"`
	InProgress       bool                      `json:"in_progress,omitempty"`
	Changing         bool                      `json:"changing,omitempty"`
	TrailingFragment bool                      `json:"trailing_fragment,omitempty"`
	Records          int                       `json:"records,omitempty"`
	Symlink          string                    `json:"symlink,omitempty"`
	Missing          bool                      `json:"missing,omitempty"`
	Unreadable       string                    `json:"unreadable,omitempty"`
	Unattributed     string                    `json:"unattributed,omitempty"`
	Basis            string                    `json:"basis,omitempty"`
	CanonicalDigest  string                    `json:"canonical_digest,omitempty"`
	CanonicalKind    string                    `json:"canonical_kind,omitempty"`
	LogicalIdentity  string                    `json:"logical_identity,omitempty"`
	Window           *evidenceTranscriptWindow `json:"window,omitempty"`
	OmittedPayload   string                    `json:"omitted_payload,omitempty"`
}

type EvidenceExportReference struct {
	Kind            string `json:"kind"`
	Digest          string `json:"digest"`
	LogicalIdentity string `json:"logical_identity,omitempty"`
	Relation        string `json:"relation,omitempty"`
}

type EvidenceExportAttemptSection struct {
	Status          string                             `json:"status"`
	AbsenceReason   string                             `json:"absence_reason,omitempty"`
	SealDigest      string                             `json:"seal_digest,omitempty"`
	Disposition     string                             `json:"disposition,omitempty"`
	Coverage        string                             `json:"coverage,omitempty"`
	Missing         []string                           `json:"missing,omitempty"`
	Unreadable      []string                           `json:"unreadable,omitempty"`
	RuntimeTaskID   string                             `json:"runtime_task_id,omitempty"`
	RuntimeEvidence bool                               `json:"runtime_evidence"`
	Window          *EvidenceExportWindow              `json:"window,omitempty"`
	Predecessors    []EvidenceExportPredecessorSection `json:"predecessors,omitempty"`
	References      []EvidenceExportReference          `json:"references,omitempty"`
}

type EvidenceExportPredecessorSection struct {
	AttemptID     string                `json:"attempt_id"`
	Task          SemanticTaskRef       `json:"task"`
	SealDigest    string                `json:"seal_digest,omitempty"`
	EntriesPrefix string                `json:"entries_prefix"`
	Relation      string                `json:"relation"`
	Status        string                `json:"status"`
	Problem       string                `json:"problem,omitempty"`
	RuntimeTaskID string                `json:"runtime_task_id,omitempty"`
	SessionIDs    []string              `json:"session_ids,omitempty"`
	Window        *EvidenceExportWindow `json:"window,omitempty"`
}

type EvidenceExportRuntimeSection struct {
	Status         string                `json:"status"`
	AbsenceReason  string                `json:"absence_reason,omitempty"`
	Coverage       string                `json:"coverage"`
	Mode           string                `json:"mode,omitempty"`
	RuntimeTaskID  string                `json:"runtime_task_id,omitempty"`
	Basis          string                `json:"basis,omitempty"`
	WorkspaceID    string                `json:"workspace_id,omitempty"`
	SessionIDs     []string              `json:"session_ids,omitempty"`
	ParentThreadID string                `json:"parent_thread_id,omitempty"`
	Window         *EvidenceExportWindow `json:"window,omitempty"`
	Missing        []string              `json:"missing,omitempty"`
	Unreadable     []string              `json:"unreadable,omitempty"`
	Unattributed   []string              `json:"unattributed,omitempty"`
}

type EvidenceExportGitArchive struct {
	LogicalIdentity string                 `json:"logical_identity"`
	Digest          string                 `json:"digest"`
	Bytes           int64                  `json:"bytes"`
	ObjectFormat    string                 `json:"object_format"`
	PackDigest      string                 `json:"pack_digest"`
	Roots           []GitObjectArchiveRoot `json:"roots"`
}

type EvidenceExportGitTaskDiff struct {
	Basis string `json:"basis"`
	Path  string `json:"path"`
}

type EvidenceExportGitAudit struct {
	ExecutionBaseOID     string                     `json:"execution_base_oid"`
	HeadOID              string                     `json:"head_oid,omitempty"`
	WorkspaceID          string                     `json:"workspace_id,omitempty"`
	Snapshot             *WorkspaceSnapshot         `json:"snapshot,omitempty"`
	BaselineIndexTree    string                     `json:"baseline_index_tree,omitempty"`
	BaselineWorktreeTree string                     `json:"baseline_worktree_tree,omitempty"`
	CurrentIndexTree     string                     `json:"current_index_tree,omitempty"`
	CurrentWorktreeTree  string                     `json:"current_worktree_tree,omitempty"`
	CandidateCommitOID   string                     `json:"candidate_commit_oid,omitempty"`
	CandidateTreeOID     string                     `json:"candidate_tree_oid,omitempty"`
	CandidateBaseOID     string                     `json:"candidate_base_oid,omitempty"`
	TaskDiff             *EvidenceExportGitTaskDiff `json:"task_diff,omitempty"`
	Archives             []EvidenceExportGitArchive `json:"archives,omitempty"`
}

type EvidenceExportAnalysisRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
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
	Attempt            EvidenceExportAttemptSection        `json:"attempt_section"`
	Runtime            EvidenceExportRuntimeSection        `json:"runtime_section"`
	Git                *EvidenceExportGitAudit             `json:"git_audit,omitempty"`
	AnalysisIndex      EvidenceExportAnalysisRef           `json:"analysis_index"`
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
	AttemptSectionStatus         string
	RuntimeStatus                string
	Coverage                     string
	AuthorityChangedDuringExport bool
	ControllerGenerationBefore   uint64
	ControllerGenerationAfter    uint64
}

type evidenceExportBuilder struct {
	store          *Store
	head           RepositoryControllerHead
	observedAt     time.Time
	files          []EvidenceExportFile
	entries        []EvidenceExportEntry
	attemptSection EvidenceExportAttemptSection
	analysisRef    EvidenceExportAnalysisRef
	association    *runtimeSessionAssociation
	archiveRefs    []evidenceExportArchiveSource
	validationRuns []collectedValidationRun

	workspaceRoot           string
	workspaceRootResolved   bool
	validationStore         *state.StateStore
	validationStoreResolved bool
}

type evidenceExportLiveCandidate struct {
	attempt       AttemptRecord
	exists        bool
	runtimeTaskID string
}

type evidenceExportTaskMatch struct {
	attempt AttemptRecord
	basis   string
}

const evidenceExportFormat = "glm-controller-evidence-export-v2"

const (
	evidenceExportTargetTaskID  = "task-id"
	evidenceExportTargetRunning = "running-task"
	evidenceExportTargetRecent  = "recent-task"

	evidenceExportAttemptPresent = "present"
	evidenceExportAttemptAbsent  = "absent"

	evidenceExportRuntimeCollected = "collected"
	evidenceExportRuntimeAbsent    = "absent"

	evidenceExportRuntimeModeLive  = "in-progress"
	evidenceExportRuntimeModeBound = "bound"

	evidenceExportCoveragePartial     = "partial"
	evidenceExportCoverageAttemptOnly = "attempt-only"
	evidenceExportCoverageOpen        = "open"

	evidenceExportBasisLiveBinding        = "live-runtime-binding"
	evidenceExportBasisSessionAssociation = "session-association"
	evidenceExportBasisRuntimeBinding     = "runtime-binding"
	evidenceExportBasisLiveAttempt        = "live-attempt"
	evidenceExportBasisExecutionTimeline  = "canonical-execution-timeline"
	evidenceExportBasisAttemptSeal        = "attempt-seal"
	evidenceExportBasisPredecessorSeal    = "predecessor-attempt-seal"
	evidenceExportBasisLiveRuntime        = "live-runtime"

	evidenceExportPredecessorCollected           = "collected"
	evidenceExportPredecessorPartial             = "partial"
	evidenceExportPredecessorRelation            = "resumed-from"
	evidenceExportPredecessorAssociationRelation = "runtime-task-association"
	evidenceExportPredecessorRoot                = "predecessors"
)

const evidenceExportPayloadOmission = "git pack payload omitted from portable projection; canonical object retained by the controller evidence authority"

var errEvidenceExportRuntimeTaskUnresolved = errors.New("evidence export cannot resolve the runtime task identity required by the fixed archive contract")

func (s *Store) ExportEvidence(request EvidenceExportRequest) (EvidenceExport, EvidenceExportResult, error) {
	head, err := s.LoadHead()
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	target, attempt, err := s.resolveEvidenceExportTarget(head, request)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	builder := &evidenceExportBuilder{store: s, head: head, observedAt: time.Now().UTC()}
	gitAudit, err := builder.collectAttemptSection(target, attempt)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	runtimeSection, liveGit, err := s.collectRuntimeSection(builder, target, attempt)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	if gitAudit == nil {
		gitAudit = liveGit
	} else {
		evidenceExportMergeGitAudit(gitAudit, liveGit)
	}
	headAfter, err := s.LoadHead()
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	observation := evidenceExportControllerObservation(head, headAfter)
	if err := builder.buildAnalysisIndex(runtimeSection, gitAudit); err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	manifest, err := builder.finish(target, runtimeSection, gitAudit, observation)
	if err != nil {
		return EvidenceExport{}, EvidenceExportResult{}, err
	}
	manifestDigest := digestBytes(builder.files[0].Data)
	result := evidenceExportResult(target, manifest, observation, manifestDigest)
	return EvidenceExport{Manifest: manifest, Files: builder.files}, result, nil
}

func evidenceExportMergeGitAudit(attemptAudit, liveGit *EvidenceExportGitAudit) {
	if attemptAudit == nil || liveGit == nil {
		return
	}
	attemptAudit.HeadOID = liveGit.HeadOID
	attemptAudit.WorkspaceID = liveGit.WorkspaceID
	attemptAudit.Snapshot = liveGit.Snapshot
}

func (s *Store) resolveEvidenceExportTarget(head RepositoryControllerHead, request EvidenceExportRequest) (EvidenceExportTarget, AttemptRecord, error) {
	live, err := s.liveExportCandidate(head)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	if request.TaskID != "" {
		return s.resolveTaskIDExportTarget(head, request.TaskID, live)
	}
	return s.resolveDefaultExportTarget(head, live)
}

func (s *Store) liveExportCandidate(head RepositoryControllerHead) (evidenceExportLiveCandidate, error) {
	if head.LiveAttemptID == "" {
		return evidenceExportLiveCandidate{}, nil
	}
	attempt, err := s.loadAttempt(head.LiveAttemptID)
	if err != nil {
		return evidenceExportLiveCandidate{}, fmt.Errorf("evidence export live attempt is unavailable: %w", err)
	}
	if err := verifyEvidenceExportLiveAttemptBinding(head, attempt); err != nil {
		return evidenceExportLiveCandidate{}, err
	}
	candidate := evidenceExportLiveCandidate{attempt: attempt, exists: true}
	_, taskID, err := s.resolveExportRuntime(attempt)
	if err != nil && !errors.Is(err, errEvidenceExportRuntimeAbsent) {
		return evidenceExportLiveCandidate{}, err
	}
	candidate.runtimeTaskID = taskID
	return candidate, nil
}

func verifyEvidenceExportLiveAttemptBinding(head RepositoryControllerHead, attempt AttemptRecord) error {
	if head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(attempt.SemanticTaskRef) {
		return fmt.Errorf("evidence export live attempt task binding is inconsistent with controller execution task")
	}
	return nil
}

func (s *Store) resolveTaskIDExportTarget(head RepositoryControllerHead, taskID string, live evidenceExportLiveCandidate) (EvidenceExportTarget, AttemptRecord, error) {
	if taskID == "" || filepath.Base(taskID) != taskID || strings.ContainsAny(taskID, `/\`) {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export task id %q is not a valid runtime task id", taskID)
	}
	if live.exists && live.runtimeTaskID == taskID {
		return evidenceExportTargetFor(live.attempt, evidenceExportTargetTaskID, evidenceExportBasisLiveBinding, taskID, true), live.attempt, nil
	}
	matches, err := s.attemptsForRuntimeTaskID(head, taskID)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	if len(matches) == 0 {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export task id %s is unknown to the controller and runtime authorities", taskID)
	}
	match, err := evidenceExportTaskIDTargetMatch(taskID, matches)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	if match.attempt.AttemptID == head.LiveAttemptID {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export task id %s is bound to the live attempt without a usable runtime source", taskID)
	}
	return evidenceExportTargetFor(match.attempt, evidenceExportTargetTaskID, match.basis, taskID, false), match.attempt, nil
}

func evidenceExportTaskIDTargetMatch(taskID string, matches []evidenceExportTaskMatch) (evidenceExportTaskMatch, error) {
	taskPath := matches[0].attempt.SemanticTaskRef.TaskPath
	latest := matches[0]
	for _, match := range matches[1:] {
		if match.attempt.SemanticTaskRef.TaskPath != taskPath {
			return evidenceExportTaskMatch{}, fmt.Errorf("evidence export task id %s binds attempts of different tasks", taskID)
		}
		if attemptOrderLess(latest.attempt, match.attempt) {
			latest = match
		}
	}
	return latest, nil
}

func (s *Store) resolveDefaultExportTarget(head RepositoryControllerHead, live evidenceExportLiveCandidate) (EvidenceExportTarget, AttemptRecord, error) {
	if live.exists {
		if live.runtimeTaskID == "" {
			return EvidenceExportTarget{}, AttemptRecord{}, errEvidenceExportRuntimeTaskUnresolved
		}
		return evidenceExportTargetFor(live.attempt, evidenceExportTargetRunning, evidenceExportBasisLiveAttempt, live.runtimeTaskID, true), live.attempt, nil
	}
	attempts, err := s.listAttempts()
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	if len(attempts) == 0 {
		return EvidenceExportTarget{}, AttemptRecord{}, fmt.Errorf("evidence export requires a running task or a previously executed task")
	}
	attempt := attempts[len(attempts)-1]
	taskID, err := s.attemptRuntimeTaskIdentity(head, attempt)
	if err != nil {
		return EvidenceExportTarget{}, AttemptRecord{}, err
	}
	return evidenceExportTargetFor(attempt, evidenceExportTargetRecent, evidenceExportBasisExecutionTimeline, taskID, false), attempt, nil
}

func (s *Store) attemptRuntimeTaskIdentity(head RepositoryControllerHead, attempt AttemptRecord) (string, error) {
	if attempt.AttemptSealID == "" {
		_, taskID, err := s.resolveExportRuntime(attempt)
		if err != nil || taskID == "" {
			return "", errEvidenceExportRuntimeTaskUnresolved
		}
		return taskID, nil
	}
	ids, err := s.attemptRuntimeTaskIDs(head, attempt)
	if err != nil || len(ids) == 0 {
		return "", errEvidenceExportRuntimeTaskUnresolved
	}
	for _, id := range ids[1:] {
		if id != ids[0] {
			return "", errEvidenceExportRuntimeTaskUnresolved
		}
	}
	return ids[0], nil
}

func evidenceExportTargetFor(attempt AttemptRecord, kind, basis, runtimeTaskID string, live bool) EvidenceExportTarget {
	return EvidenceExportTarget{
		Kind: kind, Task: attempt.SemanticTaskRef,
		AttemptID: attempt.AttemptID, EpisodeID: attempt.EpisodeID,
		RuntimeTaskID: runtimeTaskID, SelectionBasis: basis, Live: live,
	}
}

func (s *Store) attemptsForRuntimeTaskID(head RepositoryControllerHead, taskID string) ([]evidenceExportTaskMatch, error) {
	attempts, err := s.listAttempts()
	if err != nil {
		return nil, err
	}
	matches, err := s.sealedRuntimeTaskMatches(head, taskID, attempts)
	if err != nil {
		return nil, err
	}
	for _, match := range s.runtimeBindingMatches(taskID) {
		if _, sealed := matches[match.attempt.AttemptID]; !sealed {
			matches[match.attempt.AttemptID] = match
		}
	}
	result := make([]evidenceExportTaskMatch, 0, len(matches))
	for _, match := range matches {
		result = append(result, match)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].attempt.AttemptID < result[j].attempt.AttemptID })
	return result, nil
}

func (s *Store) sealedRuntimeTaskMatches(head RepositoryControllerHead, taskID string, attempts []AttemptRecord) (map[string]evidenceExportTaskMatch, error) {
	matches := map[string]evidenceExportTaskMatch{}
	for _, attempt := range attempts {
		if attempt.AttemptID == head.LiveAttemptID || attempt.AttemptSealID == "" {
			continue
		}
		associated, err := s.attemptRuntimeTaskIDs(head, attempt)
		if err != nil {
			return nil, err
		}
		if containsString(associated, taskID) {
			matches[attempt.AttemptID] = evidenceExportTaskMatch{attempt: attempt, basis: evidenceExportBasisSessionAssociation}
		}
	}
	return matches, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (s *Store) attemptRuntimeTaskIDs(head RepositoryControllerHead, attempt AttemptRecord) ([]string, error) {
	_, seal, err := s.findAttemptSeal(head, attempt)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(seal.SessionAssociationRefs))
	for _, ref := range seal.SessionAssociationRefs {
		id, err := s.associationRuntimeTaskID(ref, attempt.AttemptID)
		if err != nil {
			return nil, err
		}
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (s *Store) associationRuntimeTaskID(ref EvidenceObjectRef, attemptID string) (string, error) {
	data, err := s.LoadEvidenceObject(ref)
	if err != nil {
		return "", err
	}
	var association runtimeSessionAssociation
	if err := json.Unmarshal(data, &association); err != nil {
		return "", fmt.Errorf("evidence export session association for attempt %s is invalid: %w", attemptID, err)
	}
	if association.AttemptID != attemptID {
		return "", fmt.Errorf("evidence export session association binds attempt %s, not %s", association.AttemptID, attemptID)
	}
	return association.RuntimeTaskID, nil
}

func (s *Store) findAttemptSeal(head RepositoryControllerHead, attempt AttemptRecord) (EvidenceObjectRef, AttemptSeal, error) {
	root, err := s.attemptTaskEvidenceRoot(head, attempt)
	if err != nil {
		return EvidenceObjectRef{}, AttemptSeal{}, err
	}
	current := root
	for {
		revision, err := s.LoadTaskIndexRevision(current)
		if err != nil {
			return EvidenceObjectRef{}, AttemptSeal{}, err
		}
		ref, seal, found, err := s.attemptSealInRevision(revision, attempt)
		if err != nil {
			return EvidenceObjectRef{}, AttemptSeal{}, err
		}
		if found {
			return ref, seal, nil
		}
		if revision.PreviousRevision == nil {
			return EvidenceObjectRef{}, AttemptSeal{}, fmt.Errorf("attempt %s has no published attempt seal", attempt.AttemptID)
		}
		current = *revision.PreviousRevision
	}
}

func (s *Store) attemptTaskEvidenceRoot(head RepositoryControllerHead, attempt AttemptRecord) (EvidenceObjectRef, error) {
	if head.EvidenceHeadRef == nil {
		return EvidenceObjectRef{}, fmt.Errorf("attempt %s has no published evidence authority", attempt.AttemptID)
	}
	evidenceHead, err := s.LoadEvidenceHead(*head.EvidenceHeadRef)
	if err != nil {
		return EvidenceObjectRef{}, err
	}
	subject, ok := evidenceSubjectHead(evidenceHead.TaskHeads, taskEvidenceSubjectID(attempt.SemanticTaskRef))
	if !ok {
		return EvidenceObjectRef{}, fmt.Errorf("attempt %s task evidence root is not published", attempt.AttemptID)
	}
	return subject.RevisionRef, nil
}

func (s *Store) attemptSealInRevision(revision TaskIndexRevision, attempt AttemptRecord) (EvidenceObjectRef, AttemptSeal, bool, error) {
	for _, ref := range revision.AttemptSeals {
		seal, err := s.LoadAttemptSeal(ref)
		if err != nil {
			return EvidenceObjectRef{}, AttemptSeal{}, false, err
		}
		if seal.AttemptID == attempt.AttemptID && (attempt.AttemptSealID == "" || seal.AttemptSealID == attempt.AttemptSealID) {
			return ref, seal, true, nil
		}
	}
	return EvidenceObjectRef{}, AttemptSeal{}, false, nil
}

func (s *Store) runtimeBindingMatches(taskID string) []evidenceExportTaskMatch {
	entries, err := os.ReadDir(s.config.StateBase)
	if err != nil {
		return nil
	}
	var matches []evidenceExportTaskMatch
	for _, entry := range entries {
		if match, ok := s.runtimeDirTaskMatch(entry, taskID); ok {
			matches = append(matches, match)
		}
	}
	return matches
}

func (s *Store) runtimeDirTaskMatch(entry os.DirEntry, taskID string) (evidenceExportTaskMatch, bool) {
	if !entry.IsDir() {
		return evidenceExportTaskMatch{}, false
	}
	runtime := state.AttachStateStore(config.AppConfig{StateBase: s.config.StateBase, RepoHash: entry.Name()})
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.AttemptID == "" {
		return evidenceExportTaskMatch{}, false
	}
	bindingTaskID, err := runtime.TaskID()
	if err != nil || bindingTaskID != taskID {
		return evidenceExportTaskMatch{}, false
	}
	if entry.Name() != digestStrings(s.identity.LineageID, binding.TaskPath) {
		return evidenceExportTaskMatch{}, false
	}
	attempt, err := s.loadAttempt(binding.AttemptID)
	if err != nil || attempt.SemanticTaskRef.TaskPath != binding.TaskPath {
		return evidenceExportTaskMatch{}, false
	}
	return evidenceExportTaskMatch{attempt: attempt, basis: evidenceExportBasisRuntimeBinding}, true
}

func (s *Store) listAttempts() ([]AttemptRecord, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "attempts"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	attempts := make([]AttemptRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		var attempt AttemptRecord
		if err := readJSON(filepath.Join(s.dir, "attempts", entry.Name()), &attempt); err != nil {
			return nil, err
		}
		attempts = append(attempts, attempt)
	}
	sort.Slice(attempts, func(i, j int) bool {
		return attemptOrderLess(attempts[i], attempts[j])
	})
	return attempts, nil
}

func attemptOrderLess(left, right AttemptRecord) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.Before(right.CreatedAt)
	}
	if left.StartControllerGeneration != right.StartControllerGeneration {
		return left.StartControllerGeneration < right.StartControllerGeneration
	}
	return left.AttemptID < right.AttemptID
}

func (b *evidenceExportBuilder) hasEntry(path string) bool {
	for _, entry := range b.entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func (b *evidenceExportBuilder) finish(
	target EvidenceExportTarget,
	runtimeSection EvidenceExportRuntimeSection,
	git *EvidenceExportGitAudit,
	observation EvidenceExportControllerObservation,
) (EvidenceExportManifest, error) {
	if runtimeSection.Status == evidenceExportRuntimeCollected {
		runtimeSection.Coverage = evidenceExportCoveragePartial
	}
	manifest := EvidenceExportManifest{
		SchemaVersion:      evidenceSchemaVersion,
		Format:             evidenceExportFormat,
		CreatedAt:          b.observedAt,
		RepositoryIdentity: b.store.identity.LineageID,
		Target:             target,
		Controller:         observation,
		Attempt:            b.attemptSection,
		Runtime:            runtimeSection,
		Git:                git,
		AnalysisIndex:      b.analysisRef,
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
		AttemptSectionStatus:         manifest.Attempt.Status,
		RuntimeStatus:                manifest.Runtime.Status,
		AuthorityChangedDuringExport: observation.AuthorityChangedDuringExport,
		ControllerGenerationBefore:   observation.GenerationBefore,
		ControllerGenerationAfter:    observation.GenerationAfter,
	}
	switch {
	case manifest.Runtime.Status == evidenceExportRuntimeCollected:
		result.Coverage = evidenceExportCoveragePartial
	case manifest.Attempt.Status == evidenceExportAttemptPresent:
		result.Coverage = evidenceExportCoverageAttemptOnly
	default:
		result.Coverage = evidenceExportCoverageOpen
	}
	result.ArchiveName = evidenceExportArchiveName(target)
	result.ManifestDigest = manifestDigest
	return result
}

func evidenceExportArchiveName(target EvidenceExportTarget) string {
	return target.RuntimeTaskID + ".zip"
}
