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
	store          *Store
	runtime        *state.StateStore
	taskID         string
	attempt        AttemptRecord
	builder        *evidenceExportBuilder
	observedAt     time.Time
	missing        []string
	unreadable     []string
	sessionIDs     []string
	parentThreadID string
}

const (
	evidenceExportSourceProjection = "sealed-projection"
	evidenceExportSourceState      = "live-state"
	evidenceExportSourceTask       = "live-task"
	evidenceExportSourceArtifact   = "live-artifact"
	evidenceExportSourceTranscript = "live-transcript"
	evidenceExportSourceGitState   = "git-observation"
	evidenceExportSourceGitDiff    = "git-diff"
	evidenceExportSourceUntracked  = "git-untracked"
	evidenceExportSourceGitObjects = "git-object-archive"
)

const evidenceExportRuntimeStateDir = "live/state"

var errEvidenceExportRuntimeAbsent = errors.New("live attempt has no runtime evidence yet")

func (s *Store) collectLiveSection(
	builder *evidenceExportBuilder,
	target EvidenceExportTarget,
	attempt AttemptRecord,
) (EvidenceExportLiveSection, *EvidenceExportGitObservation, error) {
	if !target.Live {
		return EvidenceExportLiveSection{Status: evidenceExportLiveAbsent, AbsenceReason: "target attempt is not live"}, nil, nil
	}
	lease, err := s.loadLease(builder.head.LiveLeaseID)
	if err != nil {
		return EvidenceExportLiveSection{}, nil, fmt.Errorf("evidence export live lease is unavailable: %w", err)
	}
	if lease.AttemptID != attempt.AttemptID {
		return EvidenceExportLiveSection{}, nil, fmt.Errorf("evidence export live lease is bound to another attempt")
	}
	runtime, taskID, err := s.resolveExportRuntime(attempt)
	if errors.Is(err, errEvidenceExportRuntimeAbsent) {
		return EvidenceExportLiveSection{Status: evidenceExportLiveAbsent, AbsenceReason: err.Error()}, nil, nil
	}
	if err != nil {
		return EvidenceExportLiveSection{}, nil, err
	}
	binding, err := runtime.LoadControllerRuntimeBinding()
	if err != nil {
		return EvidenceExportLiveSection{}, nil, fmt.Errorf("evidence export live runtime source is not bound: %w", err)
	}
	collector := &evidenceLiveCollector{
		store: s, runtime: runtime, taskID: taskID,
		attempt: attempt, builder: builder, observedAt: time.Now().UTC(),
	}
	if err := collector.collectRuntime(); err != nil {
		return EvidenceExportLiveSection{}, nil, err
	}
	if err := collector.collectTranscripts(); err != nil {
		return EvidenceExportLiveSection{}, nil, err
	}
	git, err := collector.observeGit(lease)
	if err != nil {
		return EvidenceExportLiveSection{}, nil, err
	}
	if err := verifyExportRuntimeBindingStable(runtime, binding); err != nil {
		return EvidenceExportLiveSection{}, nil, err
	}
	live := EvidenceExportLiveSection{
		Status:         evidenceExportLiveCollected,
		RuntimeTaskID:  taskID,
		WorkspaceID:    lease.WorkspaceID,
		SessionIDs:     collector.sessionIDs,
		ParentThreadID: collector.parentThreadID,
		Missing:        collector.missing,
		Unreadable:     collector.unreadable,
	}
	return live, git, nil
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
	for _, entry := range entries {
		if entry.Name() == "lock" || entry.IsDir() {
			continue
		}
		c.addFile(c.runtime.Path(entry.Name()), evidenceExportRuntimeStateDir+"/"+entry.Name(), evidenceExportSourceState, false)
	}
	c.collectTaskFiles()
	return c.collectArtifactTree()
}

func (c *evidenceLiveCollector) collectTaskFiles() {
	files := []struct {
		path         string
		entry        string
		appendTarget bool
	}{
		{c.runtime.ModelCallLogPath(c.taskID), "live/task/telemetry.jsonl", true},
		{c.runtime.TaskEventLogPath(c.taskID), "live/task/events.jsonl", true},
		{c.runtime.TaskLiveStatusPath(c.taskID), "live/task/live.json", false},
		{c.runtime.RoundLogPath(c.taskID), "live/task/rounds.jsonl", true},
		{c.runtime.TaskLifecycleLogPath(c.taskID), "live/task/lifecycle.jsonl", true},
		{c.runtime.TaskAuthorityPathPath(c.taskID), "live/task/authority.path", false},
		{c.runtime.TaskAuthorityContentPath(c.taskID), "live/task/authority.md", false},
	}
	for _, file := range files {
		if _, err := os.Lstat(file.path); err != nil {
			continue
		}
		c.addFile(file.path, file.entry, evidenceExportSourceTask, file.appendTarget)
	}
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
		c.addFile(path, "live/task/artifacts/"+filepath.ToSlash(relative), evidenceExportSourceArtifact, false)
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
			c.recordUnreadable("live/transcripts/claude/"+id, "session id is not a safe archive segment")
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
			c.recordMissing("live/transcripts/claude/" + id)
		}
		return nil
	}
	for _, id := range safe {
		paths := matches[id]
		sort.Strings(paths)
		if len(paths) == 0 {
			c.recordMissing("live/transcripts/claude/" + id)
			continue
		}
		for index, path := range paths {
			c.addFile(path, fmt.Sprintf("live/transcripts/claude/%s/%d", id, index), evidenceExportSourceTranscript, true)
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
		c.recordMissing("live/transcripts/parent/" + parentThreadID)
		return
	}
	for index, rollout := range chain {
		c.addFile(rollout.AbsolutePath, fmt.Sprintf("live/transcripts/parent/%s/%d", parentThreadID, index), evidenceExportSourceTranscript, true)
	}
}

func (c *evidenceLiveCollector) collectGuardianTranscripts(rollouts []codexrollout.Rollout, parentThreadID string) error {
	for _, rollout := range rollouts {
		if rollout.ParentThreadID != parentThreadID || !rollout.GuardianSource || rollout.FirstTimestamp.IsZero() || rollout.FirstTimestamp.After(c.observedAt) {
			continue
		}
		last, ok := codexrollout.LastTimestamp(rollout.AbsolutePath)
		if !ok || last.Before(c.attempt.CreatedAt) {
			continue
		}
		if !evidenceExportSegmentSafe(rollout.ID) {
			c.recordUnreadable("live/transcripts/guardian/"+rollout.ID, "guardian rollout id is not a safe archive segment")
			continue
		}
		c.addFile(rollout.AbsolutePath, "live/transcripts/guardian/"+rollout.ID, evidenceExportSourceTranscript, true)
	}
	return nil
}

func evidenceExportSegmentSafe(segment string) bool {
	if segment == "" || segment == "." || segment == ".." {
		return false
	}
	return !strings.ContainsAny(segment, `/\`)
}

func (c *evidenceLiveCollector) observeGit(lease ExecutionLease) (*EvidenceExportGitObservation, error) {
	workspace, err := ResolveWorkspaceIdentity(c.store.config.RepoRoot, c.store.identity)
	if err != nil {
		return nil, fmt.Errorf("evidence export workspace identity: %w", err)
	}
	if workspace.ID != lease.WorkspaceID {
		return nil, fmt.Errorf("evidence export workspace identity %s does not match attempt lease workspace %s", workspace.ID, lease.WorkspaceID)
	}
	snapshot, err := CaptureWorkspaceSnapshot(c.store.config.RepoRoot)
	if err != nil {
		return nil, fmt.Errorf("evidence export workspace observation: %w", err)
	}
	observation := &EvidenceExportGitObservation{Snapshot: snapshot, WorkspaceID: workspace.ID}
	snapshotData, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, err
	}
	c.addBytes("live/git/snapshot.json", evidenceExportSourceGitState, append(snapshotData, '\n'), false)
	if err := c.collectGitDiffs(); err != nil {
		return nil, err
	}
	if err := c.collectUntrackedFiles(); err != nil {
		return nil, err
	}
	archiveDigest, roots, err := c.collectGitObjectArchive(snapshot)
	if err != nil {
		return nil, err
	}
	observation.ObjectArchiveDigest = archiveDigest
	observation.ObjectArchiveRoots = roots
	return observation, nil
}

func (c *evidenceLiveCollector) collectGitDiffs() error {
	staged, err := runGitBinary(c.store.config.RepoRoot, nil, "diff", "--cached", "--binary")
	if err != nil {
		return fmt.Errorf("evidence export staged diff: %w", err)
	}
	c.addBytes("live/git/diff-staged.patch", evidenceExportSourceGitDiff, staged, false)
	unstaged, err := runGitBinary(c.store.config.RepoRoot, nil, "diff", "--binary")
	if err != nil {
		return fmt.Errorf("evidence export unstaged diff: %w", err)
	}
	c.addBytes("live/git/diff-unstaged.patch", evidenceExportSourceGitDiff, unstaged, false)
	return nil
}

func (c *evidenceLiveCollector) collectUntrackedFiles() error {
	output, err := runGitBinary(c.store.config.RepoRoot, nil, "ls-files", "--others", "--exclude-standard", "-z")
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
		c.addFile(filepath.Join(c.store.config.RepoRoot, filepath.FromSlash(relative)), entryPath, evidenceExportSourceUntracked, false)
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

func (c *evidenceLiveCollector) collectGitObjectArchive(snapshot WorkspaceSnapshot) (string, []GitObjectArchiveRoot, error) {
	rootOIDs := []string{c.attempt.ExecutionBaseOID}
	if snapshot.Head != "" && snapshot.Head != c.attempt.ExecutionBaseOID {
		rootOIDs = append(rootOIDs, snapshot.Head)
	}
	data, roots, err := buildGitObjectArchiveEnvelope(c.store.config.RepoRoot, rootOIDs)
	if err != nil {
		return "", nil, fmt.Errorf("evidence export git object observation: %w", err)
	}
	c.addBytes("live/git/object-archive.json", evidenceExportSourceGitObjects, data, false)
	return digestBytes(data), roots, nil
}

func (c *evidenceLiveCollector) addFile(sourcePath, entryPath, source string, appendTarget bool) {
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
		InProgress:  appendTarget,
		Changing:    evidenceExportFileChanged(before, after) || (!appendTarget && after.ModTime().After(c.observedAt)),
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
	}, []byte(target))
}

func (c *evidenceLiveCollector) addBytes(entryPath, source string, data []byte, appendTarget bool) {
	entry := EvidenceExportEntry{
		Path:        entryPath,
		Source:      source,
		SHA256:      digestBytes(data),
		Bytes:       int64(len(data)),
		CollectedAt: c.observedAt,
		InProgress:  appendTarget,
	}
	entry.Records, entry.TrailingFragment = evidenceExportJSONLStats(entryPath, data)
	c.commit(entry, data)
}

func (c *evidenceLiveCollector) commit(entry EvidenceExportEntry, data []byte) {
	c.builder.files = append(c.builder.files, EvidenceExportFile{Path: entry.Path, Data: data})
	c.builder.entries = append(c.builder.entries, entry)
}

func (c *evidenceLiveCollector) recordMissing(entryPath string) {
	c.builder.entries = append(c.builder.entries, EvidenceExportEntry{Path: entryPath, Source: evidenceExportSourceMissingFor(entryPath), Missing: true})
	c.missing = append(c.missing, entryPath)
}

func (c *evidenceLiveCollector) recordUnreadable(entryPath, reason string) {
	c.builder.entries = append(c.builder.entries, EvidenceExportEntry{Path: entryPath, Unreadable: reason})
	c.unreadable = append(c.unreadable, entryPath)
}

func evidenceExportSourceMissingFor(entryPath string) string {
	switch {
	case strings.HasPrefix(entryPath, "live/transcripts/"):
		return evidenceExportSourceTranscript
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
