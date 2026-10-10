package controller

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceLiveCollector struct {
	store             *Store
	runtime           *state.StateStore
	taskID            string
	attempt           AttemptRecord
	builder           *evidenceExportBuilder
	observedAt        time.Time
	prefix            string
	inProgress        bool
	workspace         WorkspaceIdentity
	workspaceResolved bool
	windowEnd         time.Time
	windowEndBasis    string
	missing           []string
	unreadable        []string
	unattributed      []string
	sessionIDs        []string
	parentThreadID    string
}

const (
	evidenceExportSourceState      = "live-state"
	evidenceExportSourceTask       = "live-task"
	evidenceExportSourceArtifact   = "live-artifact"
	evidenceExportSourceTranscript = "live-transcript"
	evidenceExportSourceGitState   = "git-observation"
	evidenceExportSourceGitDiff    = "git-diff"
	evidenceExportSourceUntracked  = "git-untracked"
)

const (
	evidenceExportRuntimeStateDir    = "live/state"
	evidenceExportBoundStateDir      = "bound/state"
	evidenceExportLiveEndBasis       = "observation-time"
	evidenceExportBoundNoEndBoundary = "no-attempt-end-boundary"
)

var errEvidenceExportRuntimeAbsent = errors.New("live attempt has no runtime evidence yet")

func (c *evidenceLiveCollector) attemptWorkspaceRoot() (string, bool) {
	if !c.workspaceResolved {
		return "", false
	}
	return c.workspace.Root, true
}

func (s *Store) collectRuntimeSection(
	builder *evidenceExportBuilder,
	target EvidenceExportTarget,
	attempt AttemptRecord,
) (EvidenceExportRuntimeSection, *EvidenceExportGitAudit, error) {
	if !target.Live {
		return s.collectBoundRuntimeSection(builder, attempt)
	}
	workspace, err := s.liveAttemptWorkspace(builder.head, attempt)
	if err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	runtime, taskID, err := s.resolveExportRuntime(attempt)
	if errors.Is(err, errEvidenceExportRuntimeAbsent) {
		return EvidenceExportRuntimeSection{Status: evidenceExportRuntimeAbsent, AbsenceReason: err.Error()}, nil, nil
	}
	if err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return EvidenceExportRuntimeSection{}, nil, fmt.Errorf("evidence export live runtime source is not bound: %w", err)
	}
	collector := &evidenceLiveCollector{
		store: s, runtime: runtime, taskID: taskID,
		attempt: attempt, builder: builder, observedAt: builder.observedAt,
		prefix: evidenceExportRuntimeModeLive, inProgress: true,
		workspace: workspace, workspaceResolved: true,
		windowEnd: builder.observedAt, windowEndBasis: evidenceExportLiveEndBasis,
	}
	if err := collector.collectRuntime(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := collector.collectTranscripts(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := collector.collectValidationEvidence(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	collector.collectInstructionSnapshots()
	git, err := collector.observeGit()
	if err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := verifyExportRuntimeBindingStable(runtime, binding); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	section := collector.section(evidenceExportRuntimeModeLive, attempt, workspace.ID)
	return section, git, nil
}

func (s *Store) collectBoundRuntimeSection(
	builder *evidenceExportBuilder,
	attempt AttemptRecord,
) (EvidenceExportRuntimeSection, *EvidenceExportGitAudit, error) {
	runtime, taskID, err := s.resolveExportRuntime(attempt)
	if errors.Is(err, errEvidenceExportRuntimeAbsent) {
		return EvidenceExportRuntimeSection{
			Status:        evidenceExportRuntimeAbsent,
			AbsenceReason: "target attempt is not live and its runtime session has no bound evidence",
		}, nil, nil
	}
	if err != nil {
		return EvidenceExportRuntimeSection{
			Status:        evidenceExportRuntimeAbsent,
			AbsenceReason: fmt.Sprintf("target attempt runtime session is unavailable: %v", err),
		}, nil, nil
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return EvidenceExportRuntimeSection{
			Status:        evidenceExportRuntimeAbsent,
			AbsenceReason: fmt.Sprintf("target attempt runtime session is unavailable: %v", err),
		}, nil, nil
	}
	end, endBasis := boundRuntimeWindowEnd(builder)
	collector := &evidenceLiveCollector{
		store: s, runtime: runtime, taskID: taskID,
		attempt: attempt, builder: builder, observedAt: builder.observedAt,
		prefix: evidenceExportRuntimeModeBound, inProgress: false,
		windowEnd: end, windowEndBasis: endBasis,
	}
	if err := collector.resolveBoundAttemptWorkspace(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := collector.collectRuntime(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := collector.collectTranscripts(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	if err := collector.collectValidationEvidence(); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	collector.collectInstructionSnapshots()
	if err := verifyExportRuntimeBindingStable(runtime, binding); err != nil {
		return EvidenceExportRuntimeSection{}, nil, err
	}
	return collector.section(evidenceExportRuntimeModeBound, attempt, ""), nil, nil
}

func boundRuntimeWindowEnd(builder *evidenceExportBuilder) (time.Time, string) {
	if builder.attemptSection.Status == evidenceExportAttemptPresent && builder.attemptSection.Window != nil &&
		!builder.attemptSection.Window.End.IsZero() {
		return builder.attemptSection.Window.End, builder.attemptSection.Window.EndBasis
	}
	return time.Time{}, evidenceExportBoundNoEndBoundary
}

func (c *evidenceLiveCollector) resolveBoundAttemptWorkspace() error {
	workspace, resolved, err := c.store.attemptWorkspaceForExport(c.builder.head, c.attempt)
	if err != nil {
		return err
	}
	if resolved {
		c.workspace, c.workspaceResolved = workspace, true
	}
	return nil
}

func (c *evidenceLiveCollector) section(mode string, attempt AttemptRecord, workspaceID string) EvidenceExportRuntimeSection {
	basis := evidenceExportBasisLiveRuntime
	if mode == evidenceExportRuntimeModeBound {
		basis = evidenceExportBasisRuntimeBinding
	}
	return EvidenceExportRuntimeSection{
		Status:         evidenceExportRuntimeCollected,
		Mode:           mode,
		RuntimeTaskID:  c.taskID,
		Basis:          basis,
		WorkspaceID:    workspaceID,
		SessionIDs:     c.sessionIDs,
		ParentThreadID: c.parentThreadID,
		Window: &EvidenceExportWindow{
			Start: attempt.CreatedAt, End: c.windowEnd, EndBasis: c.windowEndBasis,
		},
		Missing:      c.missing,
		Unreadable:   c.unreadable,
		Unattributed: c.unattributed,
	}
}

func (s *Store) resolveExportRuntime(attempt AttemptRecord) (*state.StateStore, string, error) {
	runtimeCfg := s.config
	runtimeCfg.RepoHash = digestStrings(s.identity.LineageID, attempt.SemanticTaskRef.TaskPath)
	runtime := state.AttachStateStore(runtimeCfg)
	info, err := os.Lstat(runtime.Path("."))
	if errors.Is(err, os.ErrNotExist) {
		if verifyErr := s.requireNoModelRuntimeEvidence(attempt.AttemptID); verifyErr != nil {
			return nil, "", fmt.Errorf("evidence export live runtime source is unavailable: %w", verifyErr)
		}
		return nil, "", errEvidenceExportRuntimeAbsent
	}
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("evidence export live runtime source is not a directory")
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return nil, "", fmt.Errorf("evidence export live runtime source is not bound: %w", err)
	}
	if binding.AttemptID != attempt.AttemptID ||
		binding.TaskPath != attempt.SemanticTaskRef.TaskPath ||
		binding.TaskContractDigest != attempt.SemanticTaskRef.ContractDigest {
		return nil, "", fmt.Errorf("evidence export live runtime source is bound to %s (%s), not the target attempt", binding.AttemptID, binding.TaskPath)
	}
	taskID, err := validateRuntimeEvidenceTask(runtime)
	return runtime, taskID, err
}

func verifyExportRuntimeBindingStable(runtime *state.StateStore, expected state.ControllerRuntimeBinding) error {
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return fmt.Errorf("evidence export runtime source binding became unreadable: %w", err)
	}
	if binding != expected {
		return fmt.Errorf("evidence export runtime source was rebound during export: before attempt %s (%s), after attempt %s (%s)",
			expected.AttemptID, expected.TaskPath, binding.AttemptID, binding.TaskPath)
	}
	return nil
}

func (c *evidenceLiveCollector) collectRuntime() error {
	if c.runtime == nil {
		return nil
	}
	entries, err := os.ReadDir(c.runtime.Path("."))
	if err != nil {
		return err
	}
	stateDir := c.runtimeStateDir()
	for _, entry := range entries {
		if entry.IsDir() || evidenceExportEphemeralStateFile(entry.Name()) {
			continue
		}
		if entry.Name() == evidenceExportParentEvidenceFile {
			c.collectParentEvidenceAggregate(c.runtime.Path(entry.Name()), stateDir+"/"+entry.Name())
			continue
		}
		c.addFile(c.runtime.Path(entry.Name()), stateDir+"/"+entry.Name(), evidenceExportSourceState, false)
	}
	c.collectTaskFiles()
	return c.collectArtifactTree()
}

func (c *evidenceLiveCollector) runtimeStateDir() string {
	if c.prefix == evidenceExportRuntimeModeBound {
		return evidenceExportBoundStateDir
	}
	return evidenceExportRuntimeStateDir
}

func (c *evidenceLiveCollector) collectParentEvidenceAggregate(path, rawEntryPath string) {
	aggregatePath := strings.TrimSuffix(rawEntryPath, ".jsonl") + ".aggregate.json"
	data, err := os.ReadFile(path)
	if err != nil {
		c.recordUnreadable(aggregatePath, err.Error())
		return
	}
	aggregate, err := aggregateParentEvidence(data, rawEntryPath)
	if err != nil {
		c.recordUnreadable(aggregatePath, err.Error())
		return
	}
	c.commit(EvidenceExportEntry{
		Path: aggregatePath, Source: evidenceExportSourceState,
		SHA256: digestBytes(aggregate), Bytes: int64(len(aggregate)),
		CollectedAt: c.observedAt, Basis: evidenceExportBasisLiveRuntime,
		OmittedPayload: evidenceExportParentEvidenceOmission,
	}, aggregate)
}

func (c *evidenceLiveCollector) collectTaskFiles() {
	files := []struct {
		path         string
		name         string
		appendTarget bool
	}{
		{c.runtime.ModelCallLogPath(c.taskID), "telemetry.jsonl", true},
		{c.runtime.TaskEventLogPath(c.taskID), "events.jsonl", true},
		{c.runtime.TaskLiveStatusPath(c.taskID), "live.json", false},
		{c.runtime.RoundLogPath(c.taskID), "rounds.jsonl", true},
		{c.runtime.TaskLifecycleLogPath(c.taskID), "lifecycle.jsonl", true},
		{c.runtime.TaskAuthorityPathPath(c.taskID), "authority.path", false},
		{c.runtime.TaskAuthorityContentPath(c.taskID), "authority.md", false},
	}
	for _, file := range files {
		if _, err := os.Lstat(file.path); err != nil {
			continue
		}
		c.addFile(file.path, c.taskEntry(file.name), evidenceExportSourceTask, file.appendTarget)
	}
}

func (c *evidenceLiveCollector) taskEntry(name string) string {
	if c.prefix == evidenceExportRuntimeModeBound {
		return "bound/task/" + name
	}
	return "live/task/" + name
}

func (c *evidenceLiveCollector) transcriptEntry(prefix string) string {
	if c.prefix == evidenceExportRuntimeModeBound {
		return "bound/transcripts/" + prefix
	}
	return "live/transcripts/" + prefix
}

func (c *evidenceLiveCollector) collectArtifactTree() error {
	root := c.runtime.ArtifactDir(c.taskID)
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("evidence export runtime artifact source is not a directory")
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		c.addFile(path, c.taskEntry("artifacts/"+filepath.ToSlash(relative)), evidenceExportSourceArtifact, false)
		return nil
	})
}

func (c *evidenceLiveCollector) collectTranscripts() error {
	if c.runtime == nil {
		return nil
	}
	ids := c.discoverSessionIDs()
	c.sessionIDs = ids
	if err := c.collectClaudeTranscripts(ids); err != nil {
		return err
	}
	return c.collectParentTranscripts()
}

func (c *evidenceLiveCollector) discoverSessionIDs() []string {
	sessions := map[string]struct{}{}
	c.addTelemetrySessions(sessions)
	c.addEventSessions(sessions)
	for _, name := range []string{"worker.id", "reviewer.id", "failure-path-reviewer.id"} {
		value, err := c.runtime.Read(name)
		if err != nil || value == "" {
			continue
		}
		sessions[value] = struct{}{}
	}
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (c *evidenceLiveCollector) addTelemetrySessions(sessions map[string]struct{}) {
	logs, err := c.runtime.ReadModelCallLogs(c.taskID)
	if err != nil {
		return
	}
	for _, log := range logs {
		if log.TaskID != c.taskID || !runtimeTranscriptSession(log) {
			continue
		}
		sessions[log.SessionID] = struct{}{}
	}
}

func (c *evidenceLiveCollector) addEventSessions(sessions map[string]struct{}) {
	data, err := os.ReadFile(c.runtime.TaskEventLogPath(c.taskID))
	if err != nil {
		return
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		record, err := state.ParseTaskEventLine(line)
		if err != nil || record.SessionID == "" {
			continue
		}
		sessions[record.SessionID] = struct{}{}
	}
}

func (c *evidenceLiveCollector) collectClaudeTranscripts(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	safe := make([]string, 0, len(ids))
	for _, id := range ids {
		if !evidenceExportSegmentSafe(id) {
			c.recordUnreadable(c.transcriptEntry("claude/"+id), "session id is not a safe archive segment")
			continue
		}
		safe = append(safe, id)
	}
	matches, err := runner.FindClaudeTranscriptPaths(c.store.config.ClaudeConfigDir, safe)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("evidence export transcript discovery: %w", err)
		}
		for _, id := range safe {
			c.recordMissing(c.transcriptEntry("claude/" + id))
		}
		return nil
	}
	for _, id := range safe {
		paths := matches[id]
		sort.Strings(paths)
		if len(paths) == 0 {
			c.recordMissing(c.transcriptEntry("claude/" + id))
			continue
		}
		for index, path := range paths {
			capture := claudeCaptureRecord(c.builder.association, id, index, len(paths))
			c.addWindowedFile(path, c.transcriptEntry(fmt.Sprintf("claude/%s/%d", id, index)), evidenceExportSourceTranscript, true, capture)
		}
	}
	return nil
}

func (c *evidenceLiveCollector) collectParentTranscripts() error {
	parent, err := c.runtime.CurrentParentCodexIdentity()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("evidence export parent identity discovery: %w", err)
	}
	rollouts, err := codexrollout.Scan(c.store.config.CodexConfigDir)
	if err != nil {
		return fmt.Errorf("evidence export parent rollout discovery: %w", err)
	}
	if !evidenceExportSegmentSafe(parent.ThreadID) {
		c.recordUnreadable("live/transcripts/parent/"+parent.ThreadID, "parent thread id is not a safe archive segment")
	} else {
		c.parentThreadID = parent.ThreadID
		c.collectParentRolloutChain(rollouts, parent.ThreadID)
	}
	return c.collectGuardianTranscripts(rollouts, parent.ThreadID)
}

func (c *evidenceLiveCollector) collectParentRolloutChain(rollouts []codexrollout.Rollout, parentThreadID string) {
	chain, chainErr := runtimeParentRolloutChain(rollouts, parentThreadID)
	if chainErr != nil {
		c.recordMissing(c.transcriptEntry("parent/" + parentThreadID))
		return
	}
	for index, rollout := range chain {
		c.addRawTranscriptFile(rollout.AbsolutePath, c.transcriptEntry(fmt.Sprintf("parent/%s/%d", parentThreadID, index)), evidenceExportSourceTranscript, true)
	}
}

func (c *evidenceLiveCollector) collectGuardianTranscripts(rollouts []codexrollout.Rollout, parentThreadID string) error {
	for _, rollout := range rollouts {
		if rollout.ParentThreadID != parentThreadID || !rollout.GuardianSource {
			continue
		}
		if !evidenceExportSegmentSafe(rollout.ID) {
			c.recordUnreadable(c.transcriptEntry("guardian/"+rollout.ID), "guardian rollout id is not a safe archive segment")
			continue
		}
		c.addRawTranscriptFile(rollout.AbsolutePath, c.transcriptEntry("guardian/"+rollout.ID), evidenceExportSourceTranscript, true)
	}
	return nil
}

func claudeCaptureRecord(association *runtimeSessionAssociation, sessionID string, index, currentFiles int) *runtimeTranscriptWindowRecord {
	if association == nil {
		return nil
	}
	count := 0
	var match *runtimeTranscriptWindowRecord
	for i := range association.ModelTranscripts {
		if association.ModelTranscripts[i].SessionID != sessionID {
			continue
		}
		if count == index {
			match = &association.ModelTranscripts[i]
		}
		count++
	}
	if count == 0 || count != currentFiles {
		return nil
	}
	return match
}

func evidenceExportSegmentSafe(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	return !strings.ContainsAny(segment, `/\`)
}

func (c *evidenceLiveCollector) observeGit() (*EvidenceExportGitAudit, error) {
	snapshot, err := CaptureWorkspaceSnapshot(c.workspace.Root)
	if err != nil {
		return nil, fmt.Errorf("evidence export workspace observation: %w", err)
	}
	audit := &EvidenceExportGitAudit{
		ExecutionBaseOID: c.attempt.ExecutionBaseOID,
		HeadOID:          snapshot.Head,
		WorkspaceID:      c.workspace.ID,
		Snapshot:         &snapshot,
	}
	snapshotData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	c.addBytes("live/git/snapshot.json", evidenceExportSourceGitState, append(snapshotData, '\n'), false)
	if err := c.collectGitDiffs(); err != nil {
		return nil, err
	}
	return audit, c.collectUntrackedFiles()
}

func (c *evidenceLiveCollector) collectGitDiffs() error {
	staged, err := runGitBinary(c.workspace.Root, nil, "diff", "--cached", "--binary")
	if err != nil {
		return fmt.Errorf("evidence export staged diff: %w", err)
	}
	c.addBytes("live/git/diff-staged.patch", evidenceExportSourceGitDiff, staged, false)
	unstaged, err := runGitBinary(c.workspace.Root, nil, "diff", "--binary")
	if err != nil {
		return fmt.Errorf("evidence export unstaged diff: %w", err)
	}
	c.addBytes("live/git/diff-unstaged.patch", evidenceExportSourceGitDiff, unstaged, false)
	return nil
}

func (c *evidenceLiveCollector) collectUntrackedFiles() error {
	output, err := runGitBinary(c.workspace.Root, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("evidence export untracked discovery: %w", err)
	}
	for _, relative := range strings.Split(string(output), "\x00") {
		if relative == "" {
			continue
		}
		entryPath, ok := evidenceExportUntrackedEntryPath(relative)
		if !ok {
			c.recordUnreadable("live/git/untracked/"+filepath.ToSlash(relative), "path escapes repository")
			continue
		}
		c.addFile(filepath.Join(c.workspace.Root, filepath.FromSlash(relative)), entryPath, evidenceExportSourceUntracked, false)
	}
	return nil
}

func evidenceExportUntrackedEntryPath(relative string) (string, bool) {
	slash := path.Clean(filepath.ToSlash(relative))
	if slash == ".." || strings.HasPrefix(slash, "../") || strings.HasPrefix(slash, "/") {
		return "", false
	}
	return "live/git/untracked/" + slash, true
}

func (c *evidenceLiveCollector) addFile(sourcePath, entryPath, source string, appendTarget bool) {
	c.addFileBasis(sourcePath, entryPath, source, evidenceExportBasisLiveRuntime, appendTarget)
}

func (c *evidenceLiveCollector) addFileBasis(sourcePath, entryPath, source, basis string, appendTarget bool) {
	before, err := os.Lstat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		c.recordMissing(entryPath)
		return
	}
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return
	}
	if before.Mode()&os.ModeSymlink != 0 {
		c.addSymlink(sourcePath, entryPath, source)
		return
	}
	if !before.Mode().IsRegular() {
		c.recordUnreadable(entryPath, "source is not a regular file")
		return
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return
	}
	after, err := os.Lstat(sourcePath)
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return
	}
	entry := EvidenceExportEntry{
		Path:        entryPath,
		Source:      source,
		SHA256:      digestBytes(data),
		Bytes:       int64(len(data)),
		CollectedAt: c.observedAt,
		InProgress:  appendTarget && c.inProgress,
		Changing:    evidenceExportFileChanged(before, after) || (!appendTarget && after.ModTime().After(c.observedAt)),
		Basis:       basis,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	c.commit(entry, data)
}

func (c *evidenceLiveCollector) readTranscriptSource(sourcePath, entryPath string) ([]byte, os.FileInfo, os.FileInfo, bool) {
	before, err := os.Lstat(sourcePath)
	if errors.Is(err, os.ErrNotExist) {
		c.recordMissing(entryPath)
		return nil, nil, nil, false
	}
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return nil, nil, nil, false
	}
	if !before.Mode().IsRegular() {
		c.recordUnreadable(entryPath, "source is not a regular file")
		return nil, nil, nil, false
	}
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return nil, nil, nil, false
	}
	after, err := os.Lstat(sourcePath)
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return nil, nil, nil, false
	}
	return data, before, after, true
}

func (c *evidenceLiveCollector) addWindowedFile(sourcePath, entryPath, source string, appendTarget bool, capture *runtimeTranscriptWindowRecord) {
	data, before, after, ok := c.readTranscriptSource(sourcePath, entryPath)
	if !ok {
		return
	}
	window, windowed := scanTranscriptWindow(data, c.attempt.CreatedAt, c.windowEnd, capture)
	if window.Unattributed != "" {
		c.recordUnattributed(entryPath, window.Unattributed, window)
		return
	}
	entry := EvidenceExportEntry{
		Path:        entryPath,
		Source:      source,
		SHA256:      digestBytes(windowed),
		Bytes:       int64(len(windowed)),
		CollectedAt: c.observedAt,
		InProgress:  appendTarget && c.inProgress,
		Changing:    evidenceExportFileChanged(before, after) || after.ModTime().After(c.observedAt),
		Basis:       evidenceExportBasisLiveRuntime,
		Window:      &window,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, windowed)
	c.commit(entry, windowed)
}

func (c *evidenceLiveCollector) addRawTranscriptFile(sourcePath, entryPath, source string, appendTarget bool) {
	data, before, after, ok := c.readTranscriptSource(sourcePath, entryPath)
	if !ok {
		return
	}
	window := rawTranscriptWindow(data)
	entry := EvidenceExportEntry{
		Path:        entryPath,
		Source:      source,
		SHA256:      digestBytes(data),
		Bytes:       int64(len(data)),
		CollectedAt: c.observedAt,
		InProgress:  appendTarget && c.inProgress,
		Changing:    evidenceExportFileChanged(before, after) || after.ModTime().After(c.observedAt),
		Basis:       evidenceExportBasisLiveRuntime,
		Window:      &window,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	c.commit(entry, data)
}

func (c *evidenceLiveCollector) addSymlink(sourcePath, entryPath, source string) {
	target, err := os.Readlink(sourcePath)
	if err != nil {
		c.recordUnreadable(entryPath, err.Error())
		return
	}
	c.commit(EvidenceExportEntry{
		Path: entryPath, Source: source,
		SHA256: digestBytes([]byte(target)), Bytes: int64(len(target)), Symlink: target,
		Basis: evidenceExportBasisLiveRuntime,
	}, []byte(target))
}

func (c *evidenceLiveCollector) addBytes(entryPath, source string, data []byte, appendTarget bool) {
	entry := EvidenceExportEntry{
		Path:        entryPath,
		Source:      source,
		SHA256:      digestBytes(data),
		Bytes:       int64(len(data)),
		CollectedAt: c.observedAt,
		InProgress:  appendTarget && c.inProgress,
		Basis:       evidenceExportBasisLiveRuntime,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	c.commit(entry, data)
}

func (c *evidenceLiveCollector) commit(entry EvidenceExportEntry, data []byte) {
	c.builder.commit(entry, data)
}

func (c *evidenceLiveCollector) recordMissing(entryPath string) {
	c.builder.entries = append(c.builder.entries, EvidenceExportEntry{
		Path: entryPath, Source: evidenceExportSourceMissingFor(entryPath), Missing: true,
		Basis: evidenceExportBasisLiveRuntime,
	})
	c.missing = append(c.missing, entryPath)
}

func (c *evidenceLiveCollector) recordUnreadable(entryPath, reason string) {
	c.builder.entries = append(c.builder.entries, EvidenceExportEntry{Path: entryPath, Unreadable: reason, Basis: evidenceExportBasisLiveRuntime})
	c.unreadable = append(c.unreadable, entryPath)
}

func (c *evidenceLiveCollector) recordUnattributed(entryPath, reason string, window evidenceTranscriptWindow) {
	c.builder.entries = append(c.builder.entries, EvidenceExportEntry{
		Path: entryPath, Source: evidenceExportSourceTranscript,
		Unattributed: reason, Basis: evidenceExportBasisLiveRuntime, Window: &window,
	})
	c.unattributed = append(c.unattributed, entryPath)
}

func evidenceExportSourceMissingFor(entryPath string) string {
	switch {
	case strings.HasPrefix(entryPath, "live/transcripts/"), strings.HasPrefix(entryPath, "bound/transcripts/"):
		return evidenceExportSourceTranscript
	case strings.Contains(entryPath, "/validation/install-smoke/"):
		return evidenceExportSourceInstallSmoke
	case strings.Contains(entryPath, "/validation/"):
		return evidenceExportSourceGateRun
	default:
		return evidenceExportSourceTask
	}
}

func evidenceExportFileChanged(before, after os.FileInfo) bool {
	return !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime())
}

func evidenceExportJSONLStats(entryPath string, data []byte) (int, bool) {
	if !strings.HasSuffix(entryPath, ".jsonl") || len(data) == 0 {
		return 0, false
	}
	trailing := data[len(data)-1] != '\n'
	body := strings.TrimSuffix(string(data), "\n")
	if body == "" {
		return 0, trailing
	}
	records := 0
	for _, line := range strings.Split(body, "\n") {
		if json.Valid([]byte(line)) {
			records++
			continue
		}
		trailing = true
	}
	return records, trailing
}
