package controller

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type EvidenceAnalysisIndex struct {
	SchemaVersion             int                                  `json:"schema_version"`
	GeneratedAt               time.Time                            `json:"generated_at"`
	Window                    EvidenceAnalysisWindow               `json:"window"`
	Sessions                  EvidenceAnalysisSessions             `json:"sessions"`
	Parent                    *EvidenceAnalysisParent              `json:"parent,omitempty"`
	Guardian                  []EvidenceAnalysisGuardian           `json:"guardian,omitempty"`
	Predecessors              []EvidenceAnalysisPredecessor        `json:"predecessors,omitempty"`
	Instructions              EvidenceAnalysisInstructions         `json:"instructions"`
	CanonicalValidationRuns   []EvidenceAnalysisValidationRun      `json:"canonical_validation_runs,omitempty"`
	ObservedValidationActions []EvidenceAnalysisValidationRun      `json:"observed_validation_actions,omitempty"`
	Retries                   EvidenceAnalysisRetries              `json:"retries"`
	Workflow                  EvidenceAnalysisWorkflow             `json:"workflow"`
	TestTimingRegression      EvidenceAnalysisTestTimingRegression `json:"test_timing_regression"`
	ModelCalls                EvidenceAnalysisModelCalls           `json:"model_calls"`
	ReviewRounds              EvidenceAnalysisReviewRounds         `json:"review_rounds"`
	Git                       *EvidenceAnalysisGit                 `json:"git,omitempty"`
	EvidenceStates            EvidenceAnalysisStates               `json:"evidence_states"`
}

type EvidenceAnalysisWindow struct {
	Start    time.Time `json:"start,omitempty"`
	End      time.Time `json:"end,omitempty"`
	EndBasis string    `json:"end_basis,omitempty"`
}

type EvidenceAnalysisSession struct {
	SessionID string   `json:"session_id"`
	Role      string   `json:"role,omitempty"`
	Basis     string   `json:"basis"`
	Entries   []string `json:"entries,omitempty"`
}

type EvidenceAnalysisSessions struct {
	Status       string                    `json:"status"`
	Basis        string                    `json:"basis"`
	Sessions     []EvidenceAnalysisSession `json:"sessions,omitempty"`
	Unattributed []string                  `json:"unattributed,omitempty"`
	Detail       string                    `json:"detail,omitempty"`
}

type EvidenceAnalysisRolloutWindow struct {
	ID            string `json:"id,omitempty"`
	Entry         string `json:"entry,omitempty"`
	TotalBytes    int64  `json:"total_bytes"`
	StartOffset   int64  `json:"start_offset"`
	EndOffset     int64  `json:"end_offset"`
	RecordsBefore int    `json:"records_before,omitempty"`
	RecordsAfter  int    `json:"records_after,omitempty"`
	Basis         string `json:"basis"`
}

type EvidenceAnalysisParent struct {
	ThreadID         string                          `json:"thread_id,omitempty"`
	Status           string                          `json:"status"`
	AssociationBasis string                          `json:"association_basis,omitempty"`
	Rollouts         []EvidenceAnalysisRolloutWindow `json:"rollouts,omitempty"`
}

type EvidenceAnalysisGuardian struct {
	ID     string                         `json:"id"`
	Entry  string                         `json:"entry,omitempty"`
	Status string                         `json:"status"`
	Window *EvidenceAnalysisRolloutWindow `json:"window,omitempty"`
}

type EvidenceAnalysisValidationRun struct {
	RunID           string    `json:"run_id,omitempty"`
	Form            string    `json:"form"`
	GateClass       string    `json:"gate_class,omitempty"`
	Result          string    `json:"result"`
	Attempt         string    `json:"attempt,omitempty"`
	SnapshotID      string    `json:"snapshot_id,omitempty"`
	Log             string    `json:"log,omitempty"`
	TestTimings     string    `json:"test_timings,omitempty"`
	Tests           int       `json:"tests,omitempty"`
	SlowestTestMS   int64     `json:"slowest_test_ms,omitempty"`
	ResourceSamples string    `json:"resource_samples,omitempty"`
	Binding         string    `json:"binding,omitempty"`
	ExitCode        int       `json:"exit_code,omitempty"`
	DurationMS      int64     `json:"duration_ms,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
	Basis           string    `json:"basis"`
	Occurrences     int       `json:"occurrences,omitempty"`
}

type EvidenceAnalysisRetries struct {
	ValidationReruns  []EvidenceAnalysisValidationRun `json:"validation_reruns,omitempty"`
	ResumedModelCalls int                             `json:"resumed_model_calls"`
	Basis             string                          `json:"basis"`
}

type EvidenceAnalysisModelCalls struct {
	Status    string         `json:"status"`
	Total     int            `json:"total"`
	ByRole    map[string]int `json:"by_role,omitempty"`
	BySession map[string]int `json:"by_session,omitempty"`
	Basis     string         `json:"basis"`
}

type EvidenceAnalysisReviewRounds struct {
	Status string                  `json:"status"`
	Count  int                     `json:"count"`
	Rounds []EvidenceAnalysisRound `json:"rounds,omitempty"`
	Basis  string                  `json:"basis"`
}

type EvidenceAnalysisRound struct {
	Seq          int       `json:"seq"`
	ReviewNumber int       `json:"review_number"`
	AutoFixes    int       `json:"auto_fixes"`
	WorkerPhase  string    `json:"worker_phase,omitempty"`
	CapturedAt   time.Time `json:"captured_at"`
}

type EvidenceAnalysisGit struct {
	Status         string   `json:"status"`
	Archives       int      `json:"archives"`
	ArchiveDigests []string `json:"archive_digests,omitempty"`
	TaskDiff       string   `json:"task_diff,omitempty"`
	HeadOID        string   `json:"head_oid,omitempty"`
	Omission       string   `json:"omission,omitempty"`
}

type EvidenceAnalysisStates struct {
	Missing          []string `json:"missing,omitempty"`
	Unreadable       []string `json:"unreadable,omitempty"`
	Unattributed     []string `json:"unattributed,omitempty"`
	Changing         []string `json:"changing,omitempty"`
	TrailingFragment []string `json:"trailing_fragment,omitempty"`
	InProgress       []string `json:"in_progress,omitempty"`
}

type EvidenceAnalysisPredecessor struct {
	AttemptID     string                  `json:"attempt_id"`
	Status        string                  `json:"status"`
	Problem       string                  `json:"problem,omitempty"`
	RuntimeTaskID string                  `json:"runtime_task_id,omitempty"`
	SessionIDs    []string                `json:"session_ids,omitempty"`
	EntriesPrefix string                  `json:"entries_prefix"`
	SealDigest    string                  `json:"seal_digest,omitempty"`
	Window        *EvidenceAnalysisWindow `json:"window,omitempty"`
}

type EvidenceAnalysisSnapshotFile struct {
	Entry   string `json:"entry,omitempty"`
	SHA256  string `json:"sha256,omitempty"`
	Basis   string `json:"basis,omitempty"`
	Problem string `json:"problem,omitempty"`
}

type EvidenceAnalysisInstructions struct {
	Status  string                        `json:"status"`
	Basis   string                        `json:"basis"`
	Task    *EvidenceAnalysisSnapshotFile `json:"task,omitempty"`
	Plan    *EvidenceAnalysisSnapshotFile `json:"plan,omitempty"`
	Rules   *EvidenceAnalysisSnapshotFile `json:"rules,omitempty"`
	History *EvidenceAnalysisSnapshotFile `json:"history,omitempty"`
}

type EvidenceAnalysisTestTimingRegression struct {
	Status            string                                 `json:"status"`
	Basis             string                                 `json:"basis"`
	Runs              int                                    `json:"runs"`
	Conditions        EvidenceAnalysisTestTimingConditions   `json:"conditions"`
	Comparisons       []EvidenceAnalysisTestTimingComparison `json:"comparisons,omitempty"`
	IdentityConflicts []string                               `json:"identity_conflicts,omitempty"`
}

type EvidenceAnalysisTestTimingConditions struct {
	RatioThreshold float64 `json:"ratio_threshold"`
	DeltaFloorMS   int64   `json:"delta_floor_ms"`
	Candidates     int     `json:"candidates_bound"`
	ComparedPairs  string  `json:"compared_pairs"`
	Uncaptured     string  `json:"uncaptured"`
}

type EvidenceAnalysisTestTimingComparison struct {
	Form       string                                `json:"form"`
	WorkingDir string                                `json:"working_dir"`
	Baseline   EvidenceAnalysisTestTimingRunSide     `json:"baseline"`
	Latest     EvidenceAnalysisTestTimingRunSide     `json:"latest"`
	Candidates []EvidenceAnalysisTestTimingCandidate `json:"candidates,omitempty"`
	Unknown    string                                `json:"unknown,omitempty"`
}

type EvidenceAnalysisTestTimingRunSide struct {
	RunID      string                                    `json:"run_id"`
	Repository string                                    `json:"repository"`
	WorkingDir string                                    `json:"working_dir"`
	Head       string                                    `json:"head,omitempty"`
	TaskID     string                                    `json:"task_id,omitempty"`
	Result     string                                    `json:"result"`
	StartedAt  time.Time                                 `json:"started_at"`
	Conditions *EvidenceAnalysisTestTimingConditionsSide `json:"conditions,omitempty"`
}

type EvidenceAnalysisTestTimingConditionsSide = qualitygate.RunToolchain

type EvidenceAnalysisTestTimingCandidate struct {
	Package    string  `json:"package"`
	Test       string  `json:"test"`
	BaselineMS int64   `json:"baseline_ms"`
	LatestMS   int64   `json:"latest_ms"`
	DeltaMS    int64   `json:"delta_ms"`
	Ratio      float64 `json:"ratio,omitempty"`
}

type testTimingGroupKey struct {
	form       string
	workingDir string
}

type testTimingKey struct {
	pkg  string
	test string
}

const evidenceAnalysisIndexSchemaVersion = 1

const (
	evidenceAnalysisStatusCollected = "collected"
	evidenceAnalysisStatusPartial   = "partial"
	evidenceAnalysisStatusAbsent    = "absent"

	evidenceAnalysisBasisTelemetryRoles   = "telemetry-role-and-session"
	evidenceAnalysisBasisEvents           = "task-events"
	evidenceAnalysisBasisTelemetry        = "telemetry"
	evidenceAnalysisBasisRounds           = "review-rounds"
	evidenceAnalysisBasisAssociation      = "session-association"
	evidenceAnalysisBasisBlockObservation = "task-event-block-observations"

	evidenceAnalysisBasisTestTimings = "validation-test-timings"
)

const (
	testTimingRegressionRatioThreshold  = 1.5
	testTimingRegressionDeltaFloorMS    = int64(500)
	testTimingRegressionCandidatesBound = 20
)

const evidenceAnalysisIndexEntryPath = "analysis-index.json"

const taskEventKindValidation = "validation"

func (b *evidenceExportBuilder) buildAnalysisIndex(live EvidenceExportRuntimeSection, git *EvidenceExportGitAudit) error {
	canonicalRuns, observedActions := b.analysisValidationRuns()
	index := EvidenceAnalysisIndex{
		SchemaVersion:             evidenceAnalysisIndexSchemaVersion,
		GeneratedAt:               b.observedAt,
		Window:                    b.analysisWindow(live),
		Sessions:                  b.analysisSessions(live),
		Instructions:              b.analysisInstructions(),
		CanonicalValidationRuns:   canonicalRuns,
		ObservedValidationActions: observedActions,
		ModelCalls:                b.analysisModelCalls(),
		ReviewRounds:              b.analysisReviewRounds(),
		Git:                       analysisGit(git),
	}
	index.Parent = b.analysisParent(live)
	index.Guardian = b.analysisGuardian()
	index.Predecessors = b.analysisPredecessors()
	index.Retries = b.analysisRetries(canonicalRuns, observedActions)
	index.Workflow = b.analysisWorkflow(live)
	index.TestTimingRegression = b.analysisTestTimingRegression()
	index.EvidenceStates = b.analysisStates()
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	entry := EvidenceExportEntry{
		Path: evidenceAnalysisIndexEntryPath, Source: "analysis-index",
		SHA256: digestBytes(data), Bytes: int64(len(data)),
		CollectedAt: b.observedAt, Basis: "export-derivation",
	}
	b.commit(entry, data)
	b.analysisRef = EvidenceExportAnalysisRef{Path: entry.Path, SHA256: entry.SHA256, Bytes: entry.Bytes}
	return nil
}

func (b *evidenceExportBuilder) analysisWindow(live EvidenceExportRuntimeSection) EvidenceAnalysisWindow {
	switch {
	case b.attemptSection.Window != nil:
		return EvidenceAnalysisWindow{Start: b.attemptSection.Window.Start, End: b.attemptSection.Window.End, EndBasis: b.attemptSection.Window.EndBasis}
	case live.Window != nil:
		return EvidenceAnalysisWindow{Start: live.Window.Start, End: live.Window.End, EndBasis: live.Window.EndBasis}
	default:
		return EvidenceAnalysisWindow{EndBasis: "unbounded"}
	}
}

func (b *evidenceExportBuilder) analysisSessions(live EvidenceExportRuntimeSection) EvidenceAnalysisSessions {
	sessions := EvidenceAnalysisSessions{Status: evidenceAnalysisStatusAbsent, Basis: evidenceAnalysisBasisTelemetryRoles}
	if b.association != nil {
		sessions.Basis = evidenceAnalysisBasisAssociation
	}
	ids, roles := b.analysisSessionIdentities(live)
	if len(ids) == 0 {
		if b.attemptSection.Status == evidenceExportAttemptPresent && !b.attemptSection.RuntimeEvidence {
			sessions.Detail = "attempt seal captured no runtime evidence (session association missing)"
		}
		return sessions
	}
	sessions.Status = evidenceAnalysisStatusCollected
	for id := range ids {
		sessions.Sessions = append(sessions.Sessions, b.analysisSession(id, roles[id], sessions.Basis))
	}
	sort.Slice(sessions.Sessions, func(i, j int) bool { return sessions.Sessions[i].SessionID < sessions.Sessions[j].SessionID })
	sessions.Unattributed = unattributedSessions(ids, roles)
	return sessions
}

func (b *evidenceExportBuilder) analysisSessionIdentities(live EvidenceExportRuntimeSection) (map[string]struct{}, map[string]string) {
	ids := map[string]struct{}{}
	roles := map[string]string{}
	if b.association != nil {
		for _, id := range b.association.SessionIDs {
			ids[id] = struct{}{}
		}
	}
	for _, id := range live.SessionIDs {
		ids[id] = struct{}{}
	}
	b.collectTelemetrySessions(ids, roles)
	b.collectStateFileSessions(ids, roles)
	return ids, roles
}

func (b *evidenceExportBuilder) collectTelemetrySessions(ids map[string]struct{}, roles map[string]string) {
	for _, log := range b.telemetryCalls() {
		if log.SessionID == "" {
			continue
		}
		ids[log.SessionID] = struct{}{}
		if log.Role != "" {
			roles[log.SessionID] = string(log.Role)
		}
	}
}

func (b *evidenceExportBuilder) collectStateFileSessions(ids map[string]struct{}, roles map[string]string) {
	for _, stateFile := range []struct {
		name string
		role string
	}{
		{"state/worker.id", string(state.WorkerRole)},
		{"state/reviewer.id", string(state.ReviewerRole)},
		{"state/failure-path-reviewer.id", string(state.FailurePathReviewerRole)},
	} {
		data, ok := b.runtimeFileBytes(stateFile.name)
		if !ok {
			data, ok = b.fileBytes("attempt/" + stateFile.name)
		}
		if !ok {
			continue
		}
		id := strings.TrimSpace(string(data))
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
		if _, attributed := roles[id]; !attributed {
			roles[id] = stateFile.role
		}
	}
}

func (b *evidenceExportBuilder) analysisSession(id, role, basis string) EvidenceAnalysisSession {
	session := EvidenceAnalysisSession{SessionID: id, Role: role, Basis: basis}
	if role == "" {
		session.Basis = "discovered-session-without-model-call"
	}
	session.Entries = b.sessionTranscriptEntries(id)
	return session
}

func unattributedSessions(ids map[string]struct{}, roles map[string]string) []string {
	unattributed := map[string]struct{}{}
	for id := range ids {
		if _, called := roles[id]; !called {
			unattributed[id] = struct{}{}
		}
	}
	return sortedStringSet(unattributed)
}

func (b *evidenceExportBuilder) sessionTranscriptEntries(sessionID string) []string {
	var entries []string
	prefixes := []string{
		"live/transcripts/claude/" + sessionID + "/",
		"bound/transcripts/claude/" + sessionID + "/",
		"attempt/transcripts/model/" + sessionID + "/",
	}
	for _, entry := range b.entries {
		for _, prefix := range prefixes {
			if strings.HasPrefix(entry.Path, prefix) {
				entries = append(entries, entry.Path)
			}
		}
	}
	sort.Strings(entries)
	return entries
}

func (b *evidenceExportBuilder) analysisParent(live EvidenceExportRuntimeSection) *EvidenceAnalysisParent {
	parent := &EvidenceAnalysisParent{Status: evidenceAnalysisStatusAbsent}
	switch {
	case b.association != nil && b.association.Parent != nil:
		parent.ThreadID = b.association.Parent.ThreadID
		parent.AssociationBasis = evidenceAnalysisBasisAssociation
		for _, window := range b.association.ParentRollouts {
			parent.Rollouts = append(parent.Rollouts, EvidenceAnalysisRolloutWindow{
				ID: window.RolloutID, TotalBytes: window.TotalBytes,
				StartOffset: window.StartOffset, EndOffset: window.EndOffset,
				RecordsBefore: window.RecordsBefore, RecordsAfter: window.RecordsAfter,
				Basis: window.Basis,
			})
		}
	case live.ParentThreadID != "":
		parent.ThreadID = live.ParentThreadID
		parent.AssociationBasis = evidenceExportBasisLiveRuntime
	default:
		return parent
	}
	parent.Status = evidenceAnalysisStatusCollected
	if len(parent.Rollouts) == 0 {
		for _, prefix := range []string{"live/transcripts/parent/", "bound/transcripts/parent/"} {
			parent.Rollouts = append(parent.Rollouts, b.transcriptWindows(prefix)...)
		}
	}
	return parent
}

func (b *evidenceExportBuilder) analysisPredecessors() []EvidenceAnalysisPredecessor {
	var predecessors []EvidenceAnalysisPredecessor
	for _, section := range b.attemptSection.Predecessors {
		predecessor := EvidenceAnalysisPredecessor{
			AttemptID: section.AttemptID, Status: section.Status, Problem: section.Problem,
			RuntimeTaskID: section.RuntimeTaskID, SessionIDs: section.SessionIDs,
			EntriesPrefix: section.EntriesPrefix, SealDigest: section.SealDigest,
		}
		if section.Window != nil {
			predecessor.Window = &EvidenceAnalysisWindow{Start: section.Window.Start, End: section.Window.End, EndBasis: section.Window.EndBasis}
		}
		predecessors = append(predecessors, predecessor)
	}
	return predecessors
}

func (b *evidenceExportBuilder) analysisGuardian() []EvidenceAnalysisGuardian {
	var guardians []EvidenceAnalysisGuardian
	for _, entry := range b.entries {
		if !strings.HasPrefix(entry.Path, "live/transcripts/guardian/") && !strings.HasPrefix(entry.Path, "bound/transcripts/guardian/") && !strings.HasPrefix(entry.Path, "attempt/transcripts/guardian/") {
			continue
		}
		id := entry.Path
		for _, prefix := range []string{"live/transcripts/guardian/", "bound/transcripts/guardian/", "attempt/transcripts/guardian/"} {
			id = strings.TrimPrefix(id, prefix)
		}
		guardian := EvidenceAnalysisGuardian{
			ID:     strings.TrimSuffix(id, "/"),
			Status: evidenceAnalysisStatusCollected,
		}
		if !entry.Missing && entry.Unreadable == "" {
			guardian.Entry = entry.Path
		}
		if entry.Window != nil {
			guardian.Window = &EvidenceAnalysisRolloutWindow{
				TotalBytes: entry.Window.TotalBytes, StartOffset: entry.Window.StartOffset,
				EndOffset: entry.Window.EndOffset, RecordsBefore: entry.Window.RecordsBefore,
				RecordsAfter: entry.Window.RecordsAfter, Basis: entry.Window.Basis,
			}
		}
		guardians = append(guardians, guardian)
	}
	if b.association != nil {
		for _, window := range b.association.GuardianRollouts {
			guardians = append(guardians, EvidenceAnalysisGuardian{
				ID: window.RolloutID, Status: evidenceAnalysisStatusCollected,
				Window: &EvidenceAnalysisRolloutWindow{
					TotalBytes: window.TotalBytes, StartOffset: window.StartOffset,
					EndOffset: window.EndOffset, RecordsBefore: window.RecordsBefore,
					RecordsAfter: window.RecordsAfter, Basis: window.Basis,
				},
			})
		}
	}
	return guardians
}

func (b *evidenceExportBuilder) analysisInstructions() EvidenceAnalysisInstructions {
	instructions := EvidenceAnalysisInstructions{Status: evidenceAnalysisStatusPartial, Basis: "instruction-snapshot-entries"}
	instructions.Task = b.analysisSnapshotFile([]string{"attempt/authority.md", "live/task/authority.md", "bound/task/authority.md"})
	instructions.Plan = b.analysisSnapshotFile(snapshotEntryCandidates(evidenceExportInstructionPlanPath))
	instructions.Rules = b.analysisSnapshotFile(snapshotEntryCandidates(evidenceExportInstructionRulesPath))
	instructions.History = b.analysisSnapshotFile(snapshotEntryCandidates(evidenceExportInstructionHistoryPath))
	if completeSnapshotFile(instructions.Task) && completeSnapshotFile(instructions.Plan) &&
		completeSnapshotFile(instructions.Rules) && completeSnapshotFile(instructions.History) {
		instructions.Status = evidenceAnalysisStatusCollected
	}
	return instructions
}

func snapshotEntryCandidates(name string) []string {
	return []string{
		"attempt/" + evidenceExportSnapshotEntryDir + "/" + name,
		"live/" + evidenceExportSnapshotEntryDir + "/" + name,
		"bound/" + evidenceExportSnapshotEntryDir + "/" + name,
	}
}

func (b *evidenceExportBuilder) analysisSnapshotFile(candidates []string) *EvidenceAnalysisSnapshotFile {
	for _, entry := range b.entries {
		for _, candidate := range candidates {
			if entry.Path != candidate {
				continue
			}
			switch {
			case entry.Missing:
				return &EvidenceAnalysisSnapshotFile{Entry: entry.Path, Problem: "missing"}
			case entry.Unreadable != "":
				return &EvidenceAnalysisSnapshotFile{Entry: entry.Path, Problem: entry.Unreadable}
			default:
				return &EvidenceAnalysisSnapshotFile{Entry: entry.Path, SHA256: entry.SHA256, Basis: entry.Basis}
			}
		}
	}
	return &EvidenceAnalysisSnapshotFile{Problem: "absent from export"}
}

func completeSnapshotFile(file *EvidenceAnalysisSnapshotFile) bool {
	return file != nil && file.Problem == "" && file.SHA256 != ""
}

func (b *evidenceExportBuilder) analysisValidationRuns() (canonical []EvidenceAnalysisValidationRun, observed []EvidenceAnalysisValidationRun) {
	for _, collected := range b.validationRuns {
		run := EvidenceAnalysisValidationRun{
			RunID: collected.run.ValidationRunID, Form: collected.run.Form,
			GateClass: state.ValidationGateClass(collected.run.Form), Result: collected.run.Status,
			SnapshotID: state.ValidationSnapshotID(collected.run.Head, collected.run.IndexDigest, collected.run.WorktreeDigest),
			Log:        collected.logEntry, Binding: collected.binding, ExitCode: collected.run.ExitCode,
			DurationMS: collected.run.DurationMS, Timestamp: collected.run.StartedAt,
			Basis: collected.basis,
		}
		if collected.timings != nil {
			run.TestTimings = collected.timingsEntry
			run.Tests = collected.timings.CountTests()
			run.SlowestTestMS = collected.timings.SlowestTestMS()
		}
		if collected.resources != nil {
			run.ResourceSamples = collected.resourcesEntry
		}
		canonical = append(canonical, run)
	}
	for _, record := range b.taskEvents() {
		if record.Kind == taskEventKindValidation && record.Validation != nil {
			observed = append(observed, EvidenceAnalysisValidationRun{
				RunID: record.Validation.ValidationRunID, Form: record.Validation.Form,
				GateClass: record.Validation.GateClass, Result: record.Validation.Result,
				Attempt: record.Validation.Attempt, SnapshotID: record.Validation.SnapshotID,
				ExitCode: record.Validation.ExitCode, DurationMS: record.Validation.DurationMS,
				Timestamp: record.Timestamp, Basis: evidenceAnalysisBasisEvents,
			})
		}
		observed = append(observed, blockObservationValidationRuns(record)...)
	}
	return finalizeAnalysisValidationRuns(canonical), finalizeAnalysisValidationRuns(observed)
}

func finalizeAnalysisValidationRuns(runs []EvidenceAnalysisValidationRun) []EvidenceAnalysisValidationRun {
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].Timestamp.Equal(runs[j].Timestamp) {
			return runs[i].Timestamp.Before(runs[j].Timestamp)
		}
		return runs[i].RunID+runs[i].Form < runs[j].RunID+runs[j].Form
	})
	var merged []EvidenceAnalysisValidationRun
	for _, run := range runs {
		if last := len(merged) - 1; last >= 0 && sameAnalysisValidationRun(merged[last], run) {
			merged[last].Occurrences++
			continue
		}
		run.Occurrences = 1
		merged = append(merged, run)
	}
	return merged
}

func sameAnalysisValidationRun(left, right EvidenceAnalysisValidationRun) bool {
	left.Occurrences, right.Occurrences = 0, 0
	return left == right
}

func blockObservationValidationRuns(record state.TaskEventRecord) []EvidenceAnalysisValidationRun {
	var runs []EvidenceAnalysisValidationRun
	for _, block := range record.Blocks {
		for _, observation := range block.Validation {
			if observation.Result == "" {
				continue
			}
			runs = append(runs, EvidenceAnalysisValidationRun{
				Form: observation.Form, GateClass: observation.GateClass,
				Result: observation.Result, Attempt: observation.Attempt,
				SnapshotID: observation.SnapshotID, Timestamp: record.Timestamp,
				Basis: evidenceAnalysisBasisBlockObservation,
			})
		}
	}
	return runs
}

func (b *evidenceExportBuilder) analysisRetries(canonical, observed []EvidenceAnalysisValidationRun) EvidenceAnalysisRetries {
	retries := EvidenceAnalysisRetries{Basis: evidenceAnalysisBasisEvents}
	for _, run := range append(append([]EvidenceAnalysisValidationRun(nil), canonical...), observed...) {
		if run.Attempt == state.ValidationAttemptRetry {
			retries.ValidationReruns = append(retries.ValidationReruns, run)
		}
	}
	for _, call := range b.telemetryCalls() {
		if call.Resumed {
			retries.ResumedModelCalls++
		}
	}
	return retries
}

func (b *evidenceExportBuilder) analysisModelCalls() EvidenceAnalysisModelCalls {
	calls := EvidenceAnalysisModelCalls{Status: evidenceAnalysisStatusAbsent, Basis: evidenceAnalysisBasisTelemetry}
	logs := b.telemetryCalls()
	if len(logs) == 0 {
		return calls
	}
	calls.Status = evidenceAnalysisStatusCollected
	calls.Total = len(logs)
	calls.ByRole = map[string]int{}
	calls.BySession = map[string]int{}
	for _, log := range logs {
		calls.ByRole[string(log.Role)]++
		if log.SessionID != "" {
			calls.BySession[log.SessionID]++
		}
	}
	return calls
}

func (b *evidenceExportBuilder) analysisReviewRounds() EvidenceAnalysisReviewRounds {
	rounds := EvidenceAnalysisReviewRounds{Status: evidenceAnalysisStatusAbsent, Basis: evidenceAnalysisBasisRounds}
	data, ok := b.runtimeFileBytes("task/rounds.jsonl")
	if !ok {
		data, ok = b.fileBytes("attempt/rounds.jsonl")
	}
	if !ok || len(data) == 0 {
		return rounds
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var record state.RoundRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		rounds.Rounds = append(rounds.Rounds, EvidenceAnalysisRound{
			Seq: record.Seq, ReviewNumber: record.ReviewNumber, AutoFixes: record.AutoFixes,
			WorkerPhase: record.WorkerPhase, CapturedAt: record.CapturedAt,
		})
	}
	if len(rounds.Rounds) == 0 {
		return rounds
	}
	rounds.Status = evidenceAnalysisStatusCollected
	rounds.Count = len(rounds.Rounds)
	return rounds
}

func analysisGit(git *EvidenceExportGitAudit) *EvidenceAnalysisGit {
	if git == nil {
		return nil
	}
	analysis := EvidenceAnalysisGit{Status: evidenceAnalysisStatusCollected, Archives: len(git.Archives), HeadOID: git.HeadOID}
	for _, archive := range git.Archives {
		analysis.ArchiveDigests = append(analysis.ArchiveDigests, archive.Digest)
	}
	if git.TaskDiff != nil {
		analysis.TaskDiff = git.TaskDiff.Basis
	}
	if len(git.Archives) > 0 {
		analysis.Omission = evidenceExportPayloadOmission
	}
	return &analysis
}

func (b *evidenceExportBuilder) analysisStates() EvidenceAnalysisStates {
	states := EvidenceAnalysisStates{}
	for _, entry := range b.entries {
		switch {
		case entry.Missing:
			states.Missing = append(states.Missing, entry.Path)
		case entry.Unreadable != "":
			states.Unreadable = append(states.Unreadable, entry.Path)
		case entry.Unattributed != "":
			states.Unattributed = append(states.Unattributed, entry.Path)
		}
		if entry.Changing {
			states.Changing = append(states.Changing, entry.Path)
		}
		if entry.TrailingFragment {
			states.TrailingFragment = append(states.TrailingFragment, entry.Path)
		}
		if entry.InProgress {
			states.InProgress = append(states.InProgress, entry.Path)
		}
	}
	return states
}

func (b *evidenceExportBuilder) telemetryCalls() []state.ModelCallLog {
	return parseWorkflowTelemetry(b.runtimeTelemetryBytes())
}

func (b *evidenceExportBuilder) taskEvents() []state.TaskEventRecord {
	data, ok := b.runtimeFileBytes("task/events.jsonl")
	if !ok {
		data, ok = b.fileBytes("attempt/events.jsonl")
	}
	if !ok || len(data) == 0 {
		return nil
	}
	var events []state.TaskEventRecord
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := state.ParseTaskEventLine([]byte(line))
		if err != nil {
			continue
		}
		events = append(events, record)
	}
	return events
}

func (b *evidenceExportBuilder) fileBytes(path string) ([]byte, bool) {
	for _, file := range b.files {
		if file.Path == path {
			return file.Data, true
		}
	}
	return nil, false
}

func (b *evidenceExportBuilder) runtimeFileBytes(name string) ([]byte, bool) {
	for _, prefix := range []string{"live/", "bound/"} {
		if data, ok := b.fileBytes(prefix + name); ok {
			return data, true
		}
	}
	return nil, false
}

func (b *evidenceExportBuilder) transcriptWindows(prefix string) []EvidenceAnalysisRolloutWindow {
	var windows []EvidenceAnalysisRolloutWindow
	for _, entry := range b.entries {
		if !strings.HasPrefix(entry.Path, prefix) || entry.Window == nil {
			continue
		}
		windows = append(windows, EvidenceAnalysisRolloutWindow{
			Entry: entry.Path, TotalBytes: entry.Window.TotalBytes,
			StartOffset: entry.Window.StartOffset, EndOffset: entry.Window.EndOffset,
			RecordsBefore: entry.Window.RecordsBefore, RecordsAfter: entry.Window.RecordsAfter,
			Basis: entry.Window.Basis,
		})
	}
	return windows
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (b *evidenceExportBuilder) analysisTestTimingRegression() EvidenceAnalysisTestTimingRegression {
	regression := EvidenceAnalysisTestTimingRegression{
		Status: evidenceAnalysisStatusAbsent,
		Basis:  evidenceAnalysisBasisTestTimings,
		Conditions: EvidenceAnalysisTestTimingConditions{
			RatioThreshold: testTimingRegressionRatioThreshold,
			DeltaFloorMS:   testTimingRegressionDeltaFloorMS,
			Candidates:     testTimingRegressionCandidatesBound,
			ComparedPairs:  "latest run vs nearest older run with equal recorded toolchain conditions per form and repository-relative module; pass-to-pass tests only",
			Uncaptured:     "machine load, CPU model, and cache warmth are not captured; runs without recorded toolchain conditions never form pairs",
		},
	}
	groups, conflicts := collectTestTimingGroups(b.validationRuns)
	regression.IdentityConflicts = conflicts
	for _, runs := range groups {
		regression.Runs += len(runs)
	}
	if regression.Runs == 0 && len(conflicts) == 0 {
		return regression
	}
	regression.Status = evidenceAnalysisStatusCollected
	if len(conflicts) > 0 {
		regression.Status = evidenceAnalysisStatusPartial
	}
	for _, key := range sortedTestTimingGroupKeys(groups) {
		regression.Comparisons = append(regression.Comparisons, testTimingComparison(key.form, key.workingDir, groups[key]))
	}
	return regression
}

func collectTestTimingGroups(validationRuns []collectedValidationRun) (map[testTimingGroupKey][]collectedValidationRun, []string) {
	converged, conflicts := convergeValidationRunIdentities(validationRuns)
	groups := map[testTimingGroupKey][]collectedValidationRun{}
	for _, collected := range converged {
		if collected.timings == nil {
			continue
		}
		module := testTimingModule(collected.run)
		key := testTimingGroupKey{form: collected.run.Form, workingDir: module}
		groups[key] = append(groups[key], collected)
	}
	return groups, sortedConflictedRunIDs(conflicts)
}

func convergeValidationRunIdentities(runs []collectedValidationRun) ([]collectedValidationRun, map[string]struct{}) {
	conflicts := map[string]struct{}{}
	byID := map[string][]collectedValidationRun{}
	var order []string
	unidentified := []collectedValidationRun{}
	for _, run := range runs {
		id := run.run.ValidationRunID
		if id == "" {
			unidentified = append(unidentified, run)
			continue
		}
		if _, seen := byID[id]; !seen {
			order = append(order, id)
		}
		byID[id] = append(byID[id], run)
	}
	converged := make([]collectedValidationRun, 0, len(runs))
	for _, id := range order {
		copies := byID[id]
		merged, ok := mergeValidationRunCopies(copies)
		if !ok {
			conflicts[id] = struct{}{}
			continue
		}
		converged = append(converged, merged)
	}
	return append(converged, unidentified...), conflicts
}

func mergeValidationRunCopies(copies []collectedValidationRun) (collectedValidationRun, bool) {
	merged := copies[0]
	for _, copy := range copies[1:] {
		if !mergeValidationRunCopy(&merged, copy) {
			return collectedValidationRun{}, false
		}
	}
	return merged, true
}

func mergeValidationRunCopy(merged *collectedValidationRun, copy collectedValidationRun) bool {
	if !sameRunRecordEncoding(merged.run, copy.run) {
		return false
	}
	if copy.timings != nil {
		if merged.timings == nil {
			merged.timings = copy.timings
			merged.timingsEntry = copy.timingsEntry
		} else if !sameTestTimingsEncoding(*merged.timings, *copy.timings) {
			return false
		}
	}
	if copy.resources == nil {
		return true
	}
	if merged.resources == nil {
		merged.resources = copy.resources
		merged.resourcesEntry = copy.resourcesEntry
		return true
	}
	return sameResourceSamplesEncoding(merged.resources, copy.resources)
}

func sameResourceSamplesEncoding(left, right *qualitygate.ResourceSamples) bool {
	encodedLeft, err := qualitygate.EncodeResourceSamples(*left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.EncodeResourceSamples(*right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sameRunRecordEncoding(left, right qualitygate.RunRecord) bool {
	encodedLeft, err := qualitygate.Encode(left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.Encode(right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sameTestTimingsEncoding(left, right qualitygate.TestTimings) bool {
	encodedLeft, err := qualitygate.EncodeTestTimings(left)
	if err != nil {
		return false
	}
	encodedRight, err := qualitygate.EncodeTestTimings(right)
	if err != nil {
		return false
	}
	return bytes.Equal(encodedLeft, encodedRight)
}

func sortedConflictedRunIDs(conflicts map[string]struct{}) []string {
	ids := make([]string, 0, len(conflicts))
	for id := range conflicts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func testTimingModule(run qualitygate.RunRecord) string {
	if !filepath.IsAbs(run.Repository) || !filepath.IsAbs(run.WorkingDir) {
		return ""
	}
	module, err := filepath.Rel(run.Repository, run.WorkingDir)
	if err != nil || !validFinalValidationWorkingDir(module) {
		return ""
	}
	return filepath.ToSlash(module)
}

func sortedTestTimingGroupKeys(groups map[testTimingGroupKey][]collectedValidationRun) []testTimingGroupKey {
	keys := make([]testTimingGroupKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].form != keys[j].form {
			return keys[i].form < keys[j].form
		}
		return keys[i].workingDir < keys[j].workingDir
	})
	return keys
}

func testTimingComparison(form, workingDir string, runs []collectedValidationRun) EvidenceAnalysisTestTimingComparison {
	runs = append([]collectedValidationRun(nil), runs...)
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].run.StartedAt.Equal(runs[j].run.StartedAt) {
			return runs[i].run.StartedAt.Before(runs[j].run.StartedAt)
		}
		return runs[i].run.ValidationRunID < runs[j].run.ValidationRunID
	})
	latest := runs[len(runs)-1]
	comparison := EvidenceAnalysisTestTimingComparison{
		Form:       form,
		WorkingDir: workingDir,
		Latest:     testTimingRunSide(latest),
	}
	if workingDir == "" {
		comparison.Unknown = "run does not record a module within its repository checkout"
		return comparison
	}
	latestConditions := testTimingConditions(latest)
	if latestConditions == nil {
		comparison.Unknown = "latest run does not record its execution conditions"
		return comparison
	}
	if latest.timings == nil || !latest.timings.Coverage.Captured || latest.timings.Coverage.UntimedTerminalEvents > 0 || latest.run.Status != qualitygate.StatusPass {
		comparison.Unknown = "latest run captured no machine-readable test events"
		return comparison
	}
	baseline, ok := nearestComparableTimingRun(runs, len(runs)-1, latestConditions)
	if !ok {
		comparison.Unknown = "no older comparable run with recorded conditions and captured test events in this export"
		return comparison
	}
	comparison.Baseline = testTimingRunSide(baseline)
	comparison.Candidates = testTimingCandidates(baseline.timings, latest.timings)
	return comparison
}

func nearestComparableTimingRun(runs []collectedValidationRun, latest int, latestConditions *qualitygate.RunToolchain) (collectedValidationRun, bool) {
	for index := latest - 1; index >= 0; index-- {
		if runs[index].run.ValidationRunID == runs[latest].run.ValidationRunID {
			continue
		}
		if conditions := testTimingConditions(runs[index]); conditions == nil || *conditions != *latestConditions {
			continue
		}
		if runs[index].timings == nil || !runs[index].timings.Coverage.Captured || runs[index].timings.Coverage.UntimedTerminalEvents > 0 || runs[index].run.Status != qualitygate.StatusPass {
			continue
		}
		return runs[index], true
	}
	return collectedValidationRun{}, false
}

func testTimingRunSide(collected collectedValidationRun) EvidenceAnalysisTestTimingRunSide {
	return EvidenceAnalysisTestTimingRunSide{
		RunID:      collected.run.ValidationRunID,
		Repository: collected.run.Repository,
		WorkingDir: collected.run.WorkingDir,
		Head:       collected.run.Head,
		TaskID:     collected.run.TaskID,
		Result:     collected.run.Status,
		StartedAt:  collected.run.StartedAt,
		Conditions: testTimingConditionsSide(collected),
	}
}

func testTimingConditionsSide(collected collectedValidationRun) *EvidenceAnalysisTestTimingConditionsSide {
	toolchain := testTimingConditions(collected)
	if toolchain == nil {
		return nil
	}
	copy := *toolchain
	return &copy
}

func testTimingConditions(collected collectedValidationRun) *qualitygate.RunToolchain {
	if collected.run.Toolchain == nil || !collected.run.Toolchain.Recorded() {
		return nil
	}
	return collected.run.Toolchain
}

func testTimingCandidates(baseline, latest *qualitygate.TestTimings) []EvidenceAnalysisTestTimingCandidate {
	base := baselineTestTimings(baseline)
	var candidates []EvidenceAnalysisTestTimingCandidate
	for _, timing := range latest.Tests {
		candidate, ok := testTimingCandidate(timing, base)
		if ok {
			candidates = append(candidates, candidate)
		}
	}
	return boundTestTimingCandidates(candidates)
}

func baselineTestTimings(baseline *qualitygate.TestTimings) map[testTimingKey]qualitygate.TestTiming {
	base := map[testTimingKey]qualitygate.TestTiming{}
	for _, timing := range baseline.Tests {
		if timing.IsPackageTiming() {
			continue
		}
		base[testTimingKey{pkg: timing.Package, test: timing.Test}] = timing
	}
	return base
}

func testTimingCandidate(timing qualitygate.TestTiming, base map[testTimingKey]qualitygate.TestTiming) (EvidenceAnalysisTestTimingCandidate, bool) {
	if timing.IsPackageTiming() || timing.Action != qualitygate.TestTimingActionPass {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	compared, ok := base[testTimingKey{pkg: timing.Package, test: timing.Test}]
	if !ok || compared.Action != qualitygate.TestTimingActionPass {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	delta := timing.ElapsedMS - compared.ElapsedMS
	if delta < testTimingRegressionDeltaFloorMS {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	if compared.ElapsedMS > 0 && float64(timing.ElapsedMS) < float64(compared.ElapsedMS)*testTimingRegressionRatioThreshold {
		return EvidenceAnalysisTestTimingCandidate{}, false
	}
	return newTestTimingCandidate(timing, compared, delta), true
}

func newTestTimingCandidate(timing, compared qualitygate.TestTiming, delta int64) EvidenceAnalysisTestTimingCandidate {
	candidate := EvidenceAnalysisTestTimingCandidate{
		Package: timing.Package, Test: timing.Test,
		BaselineMS: compared.ElapsedMS, LatestMS: timing.ElapsedMS, DeltaMS: delta,
	}
	if compared.ElapsedMS > 0 {
		candidate.Ratio = math.Round(float64(timing.ElapsedMS)/float64(compared.ElapsedMS)*100) / 100
	}
	return candidate
}

func boundTestTimingCandidates(candidates []EvidenceAnalysisTestTimingCandidate) []EvidenceAnalysisTestTimingCandidate {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].DeltaMS != candidates[j].DeltaMS {
			return candidates[i].DeltaMS > candidates[j].DeltaMS
		}
		if candidates[i].Package != candidates[j].Package {
			return candidates[i].Package < candidates[j].Package
		}
		return candidates[i].Test < candidates[j].Test
	})
	if len(candidates) > testTimingRegressionCandidatesBound {
		candidates = candidates[:testTimingRegressionCandidatesBound]
	}
	return candidates
}
