package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type bundleOutput struct {
	TaskID             string   `json:"task_id"`
	TaskStatus         string   `json:"task_status"`
	ArchivePath        string   `json:"archive_path"`
	EvidenceStatus     string   `json:"evidence_status"`
	Coverage           string   `json:"coverage"`
	CoverageReasons    []string `json:"coverage_reasons,omitempty"`
	CoverageScope      []string `json:"coverage_scope"`
	ClaudeSessionIDs   []string `json:"claude_session_ids"`
	InFlightModelCalls int      `json:"in_flight_model_calls,omitempty"`
	Missing            []string `json:"missing"`
	Unattributed       []string `json:"unattributed,omitempty"`
	Unreadable         []string `json:"unreadable,omitempty"`
}

type bundleManifest struct {
	Format             string              `json:"format"`
	TaskID             string              `json:"task_id"`
	TaskStatus         string              `json:"task_status"`
	CurrentTask        bool                `json:"current_task"`
	EvidenceStatus     string              `json:"evidence_status"`
	Coverage           string              `json:"coverage"`
	CoverageReasons    []string            `json:"coverage_reasons,omitempty"`
	CoverageScope      []string            `json:"coverage_scope"`
	ClaudeSessionIDs   []string            `json:"claude_session_ids"`
	InFlightModelCalls int                 `json:"in_flight_model_calls,omitempty"`
	Included           []string            `json:"included"`
	Missing            []string            `json:"missing"`
	Unattributed       []string            `json:"unattributed,omitempty"`
	Unreadable         []string            `json:"unreadable,omitempty"`
	CodexEvidence      []bundleCodexSource `json:"codex_evidence,omitempty"`
	CollectionIndex    string              `json:"collection_index,omitempty"`
	CreatedAt          string              `json:"created_at"`
}

type bundleCollectedEntry struct {
	Path             string `json:"path"`
	SHA256           string `json:"sha256"`
	Bytes            int64  `json:"bytes"`
	CollectedAt      string `json:"collected_at"`
	SourceModifiedAt string `json:"source_modified_at,omitempty"`
	Records          int    `json:"records,omitempty"`
	InvalidRecords   int    `json:"invalid_records,omitempty"`
	DroppedLines     int    `json:"dropped_lines,omitempty"`
	RuntimeRecords   int    `json:"runtime_records,omitempty"`
	TrailingFragment bool   `json:"trailing_fragment,omitempty"`
	LastEventAt      string `json:"last_event_at,omitempty"`
	InProgress       bool   `json:"in_progress,omitempty"`
}

type bundleCollectionIndex struct {
	CollectorRevision string                 `json:"collector_revision,omitempty"`
	Entries           []bundleCollectedEntry `json:"entries"`
}

type bundleTask struct {
	ID      string
	Status  string
	Current bool
	Stats   state.TaskStats
}

type bundleEvidenceSummary struct {
	missing            []string
	unattributed       []string
	unreadable         []string
	evidenceStatus     string
	coverage           string
	coverageReasons    []string
	inFlightModelCalls int
	lifecycleCollected bool
}

type bundleEntry struct {
	SourcePath  string
	ArchivePath string
	Data        []byte
	InProgress  bool
}

type bundleCollector struct {
	entries            map[string]bundleEntry
	missing            map[string]struct{}
	unreadable         map[string]struct{}
	unattributed       map[string]struct{}
	lifecycleCollected bool
}

const bundleFormat = "glm-worker-task-bundle-v3"

const bundleCollectionEntryPath = "collection.json"

const (
	bundleCoveragePartial = "partial"
	bundleCoverageOpen    = "open"
	bundleCoverageClosed  = "closed"
)

var bundleAggregateDirs = map[string]bool{
	"artifacts":      true,
	"events":         true,
	"lifecycle":      true,
	"rounds":         true,
	"stats":          true,
	"task-authority": true,
	"telemetry":      true,
}

var bundleInProgressPrefixes = []string{
	"task/telemetry/",
	"task/events/",
	"task/rounds/",
	"task/lifecycle/",
	"claude-transcripts/",
}

var bundleCoverageScope = []string{"task-status", "evidence-presence", "evidence-readability"}

func printBundle(cfg config.AppConfig, st *state.StateStore, requestedTaskID string, stdout io.Writer) error {
	task, err := selectBundleTask(st, requestedTaskID)
	if err != nil {
		return err
	}

	collector := newBundleCollector()
	collector.collectTaskEvidence(st, task)
	sessionIDs := collector.collectTaskSessions(st, task)
	collector.collectClaudeTranscripts(cfg, sessionIDs)
	codexEvidence := collector.collectCodexEvidence(cfg, task)
	if task.Current {
		collector.collectCurrentState(cfg, st)
	}
	collector.markInProgressEvidence(task)

	archivePath, err := bundleArchivePath(cfg, task.ID)
	if err != nil {
		return err
	}
	summary := collector.evidenceSummary(st, task)
	if err := writeBundleArchive(collector, archivePath, task, sessionIDs, &summary, codexEvidence); err != nil {
		return err
	}

	return writeJSON(stdout, bundleOutput{
		TaskID:             task.ID,
		TaskStatus:         task.Status,
		ArchivePath:        archivePath,
		EvidenceStatus:     summary.evidenceStatus,
		Coverage:           summary.coverage,
		CoverageReasons:    summary.coverageReasons,
		CoverageScope:      bundleCoverageScope,
		ClaudeSessionIDs:   sessionIDs,
		InFlightModelCalls: summary.inFlightModelCalls,
		Missing:            summary.missing,
		Unattributed:       summary.unattributed,
		Unreadable:         summary.unreadable,
	})
}

func (c *bundleCollector) evidenceSummary(st *state.StateStore, task bundleTask) bundleEvidenceSummary {
	missing := c.missingList()
	summary := bundleEvidenceSummary{
		missing:            missing,
		unattributed:       c.unattributedList(),
		unreadable:         c.unreadableList(),
		evidenceStatus:     "complete",
		inFlightModelCalls: bundleInFlightModelCalls(st, task),
		lifecycleCollected: c.lifecycleCollected,
	}
	if len(missing) > 0 {
		summary.evidenceStatus = "incomplete"
	}
	summary.coverage, summary.coverageReasons = bundleCoverage(task, summary.inFlightModelCalls, summary.missing, summary.unreadable, false)
	if !c.lifecycleCollected {
		summary.coverageReasons = append(summary.coverageReasons, "legacy-evidence:lifecycle")
	}
	return summary
}

func (s *bundleEvidenceSummary) mergeReadability(index bundleCollectionIndex, task bundleTask) {
	s.coverage, s.coverageReasons = bundleCoverage(task, s.inFlightModelCalls, s.missing, s.unreadable, bundleIndexReadabilityAnomaly(index))
	if !s.lifecycleCollected {
		s.coverageReasons = append(s.coverageReasons, "legacy-evidence:lifecycle")
	}
	if bundleIndexLegacyRuntime(index) {
		s.coverageReasons = append(s.coverageReasons, "legacy-evidence:runtime")
	}
}

func bundleIndexReadabilityAnomaly(index bundleCollectionIndex) bool {
	for _, entry := range index.Entries {
		if entry.InProgress {
			continue
		}
		if entry.TrailingFragment || entry.InvalidRecords > 0 || entry.DroppedLines > 0 {
			return true
		}
	}
	return false
}

func bundleIndexLegacyRuntime(index bundleCollectionIndex) bool {
	for _, entry := range index.Entries {
		if strings.HasPrefix(entry.Path, bundleTelemetryArchivePrefix) && entry.Records > 0 && entry.RuntimeRecords == 0 {
			return true
		}
	}
	return false
}

func buildBundleManifest(collector *bundleCollector, task bundleTask, sessionIDs []string, summary bundleEvidenceSummary, codexEvidence []bundleCodexSource) bundleManifest {
	return bundleManifest{
		Format:             bundleFormat,
		TaskID:             task.ID,
		TaskStatus:         task.Status,
		CurrentTask:        task.Current,
		EvidenceStatus:     summary.evidenceStatus,
		Coverage:           summary.coverage,
		CoverageReasons:    summary.coverageReasons,
		CoverageScope:      bundleCoverageScope,
		ClaudeSessionIDs:   sessionIDs,
		InFlightModelCalls: summary.inFlightModelCalls,
		Included:           collector.includedListWithManifest(),
		Missing:            summary.missing,
		Unattributed:       summary.unattributed,
		Unreadable:         summary.unreadable,
		CodexEvidence:      codexEvidence,
		CollectionIndex:    bundleCollectionEntryPath,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func writeBundleArchive(
	collector *bundleCollector,
	archivePath string,
	task bundleTask,
	sessionIDs []string,
	summary *bundleEvidenceSummary,
	codexEvidence []bundleCodexSource,
) error {
	buildManifest := func(index bundleCollectionIndex) bundleManifest {
		summary.mergeReadability(index, task)
		return buildBundleManifest(collector, task, sessionIDs, *summary, codexEvidence)
	}
	return writeBundleArchiveAtomically(archivePath, collector.entryList(), buildManifest)
}

func bundleCoverage(task bundleTask, inFlightModelCalls int, missing, unreadable []string, jsonlAnomaly bool) (string, []string) {
	reasons := bundleCoverageReasons(task, inFlightModelCalls, missing, unreadable, jsonlAnomaly)
	switch {
	case len(missing) > 0 || len(unreadable) > 0 || jsonlAnomaly:
		return bundleCoveragePartial, reasons
	case task.Current || inFlightModelCalls > 0 || task.Status != string(state.TaskStatusComplete):
		return bundleCoverageOpen, reasons
	default:
		return bundleCoverageClosed, reasons
	}
}

func bundleCoverageReasons(task bundleTask, inFlightModelCalls int, missing, unreadable []string, jsonlAnomaly bool) []string {
	reasons := make([]string, 0, 5)
	if task.Current {
		reasons = append(reasons, "task-current")
	}
	if inFlightModelCalls > 0 {
		reasons = append(reasons, "in-flight-model-calls")
	}
	if task.Status != string(state.TaskStatusComplete) {
		reasons = append(reasons, "task-status:"+task.Status)
	}
	if len(missing) > 0 {
		reasons = append(reasons, "missing-evidence")
	}
	if len(unreadable) > 0 {
		reasons = append(reasons, "unreadable-evidence")
	}
	if jsonlAnomaly {
		reasons = append(reasons, "jsonl-anomaly")
	}
	return reasons
}

func selectBundleTask(st *state.StateStore, requestedTaskID string) (bundleTask, error) {
	currentID := st.ReadOr("task.id", "")
	if requestedTaskID == "" && currentID != "" {
		return currentBundleTask(st, currentID), nil
	}
	if requestedTaskID != "" && requestedTaskID == currentID {
		return currentBundleTask(st, currentID), nil
	}

	allStats, err := st.AllTaskStats()
	if err != nil {
		return bundleTask{}, fmt.Errorf("task statsを読めません: %w", err)
	}
	if requestedTaskID != "" {
		for _, stats := range allStats {
			if stats.TaskID == requestedTaskID {
				return bundleTask{ID: stats.TaskID, Status: string(stats.Status), Stats: stats}, nil
			}
		}
		return bundleTask{}, &NotFoundError{Message: fmt.Sprintf("task %sのretained evidenceがありません", requestedTaskID)}
	}
	if len(allStats) == 0 {
		return bundleTask{}, &NotFoundError{Message: "bundle対象のtaskがありません"}
	}

	sort.Slice(allStats, func(i, j int) bool {
		if allStats[i].StartedAt.Equal(allStats[j].StartedAt) {
			return allStats[i].TaskID < allStats[j].TaskID
		}
		return allStats[i].StartedAt.Before(allStats[j].StartedAt)
	})
	latest := allStats[len(allStats)-1]
	return bundleTask{ID: latest.TaskID, Status: string(latest.Status), Stats: latest}, nil
}

func currentBundleTask(st *state.StateStore, taskID string) bundleTask {
	stats, _ := st.CurrentTaskStats()
	status := string(st.TaskStatus())
	if status == string(state.TaskStatusNone) && stats.TaskID == taskID {
		status = string(stats.Status)
	}
	return bundleTask{ID: taskID, Status: status, Current: true, Stats: stats}
}

func bundleInFlightModelCalls(st *state.StateStore, task bundleTask) int {
	if !task.Current || task.Stats.ModelCalls <= 0 {
		return 0
	}
	logs, err := st.ReadModelCallLogs(task.ID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0
	}
	finalized := 0
	for _, log := range logs {
		if log.CallType == "" || log.CallType == state.CallTypeTask {
			finalized++
		}
	}
	pending := task.Stats.ModelCalls - finalized
	if pending < 0 {
		return 0
	}
	return pending
}

func newBundleCollector() *bundleCollector {
	return &bundleCollector{
		entries:      make(map[string]bundleEntry),
		missing:      make(map[string]struct{}),
		unreadable:   make(map[string]struct{}),
		unattributed: make(map[string]struct{}),
	}
}

func (c *bundleCollector) collectTaskEvidence(st *state.StateStore, task bundleTask) {
	c.addFileIfPresent(st.ModelCallLogPath(task.ID), path.Join("task", "telemetry", task.ID+".jsonl"))
	c.addFileIfPresent(st.TaskEventLogPath(task.ID), path.Join("task", "events", task.ID+".jsonl"))
	c.addFileIfPresent(st.TaskLiveStatusPath(task.ID), path.Join("task", "events", task.ID+".live.json"))
	c.addFileIfPresent(st.RoundLogPath(task.ID), path.Join("task", "rounds", task.ID+".jsonl"))
	c.lifecycleCollected = c.addFileIfPresent(st.TaskLifecycleLogPath(task.ID), path.Join("task", "lifecycle", task.ID+".jsonl"))
	if c.addFileIfPresent(st.TaskAuthorityPathPath(task.ID), path.Join("task", "authority", "active-task.path")) {
		if !c.addFileIfPresent(st.TaskAuthorityContentPath(task.ID), path.Join("task", "authority", "active-task.md")) {
			c.addMissing(path.Join("task", "authority", "active-task.md"))
		}
	}
	if task.Current {
		if !c.addFileIfPresent(st.Path("task-stats.json"), path.Join("task", "task-stats.json")) {
			c.addMissing("task/task-stats.json")
		}
	} else {
		statsPath := st.Path(filepath.Join("stats", task.ID+".json"))
		if !c.addFileIfPresent(statsPath, path.Join("task", "stats", task.ID+".json")) {
			c.addMissing(path.Join("task", "stats", task.ID+".json"))
		}
	}
	_ = c.addTreeIfPresent(st.ArtifactDir(task.ID), path.Join("task", "artifacts", task.ID))

	if task.Stats.ModelCalls > 0 && !hasTaskAssociationEvidence(st, task.ID) {
		c.addMissing("task/session-association")
	}
}

func hasTaskAssociationEvidence(st *state.StateStore, taskID string) bool {
	return regularFileExists(st.ModelCallLogPath(taskID)) || regularFileExists(st.TaskEventLogPath(taskID))
}

func (c *bundleCollector) collectTaskSessions(st *state.StateStore, task bundleTask) []string {
	sessions := make(map[string]struct{})
	logs, err := st.ReadModelCallLogs(task.ID)
	switch {
	case err == nil:
		for _, log := range logs {
			if log.SessionID != "" {
				sessions[log.SessionID] = struct{}{}
			}
		}
	case !errors.Is(err, os.ErrNotExist):
		c.addMissing("task/telemetry/session-association-unreadable")
	}
	c.collectEventSessions(st.TaskEventLogPath(task.ID), sessions)

	if task.Current {
		for _, name := range []string{"worker.id", "reviewer.id"} {
			if value := st.ReadOr(name, ""); value != "" {
				sessions[value] = struct{}{}
			}
		}
	}
	if task.Stats.ModelCalls > 0 && len(sessions) == 0 {
		c.addMissing("task/claude-session-ids")
	}
	return sortedSet(sessions)
}

func (c *bundleCollector) collectEventSessions(eventPath string, sessions map[string]struct{}) {
	file, err := os.Open(eventPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		c.addMissing("task/events/session-association-unreadable")
		return
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			c.addMissing("task/events/session-association-unreadable")
			continue
		}
		if record.SessionID != "" {
			sessions[record.SessionID] = struct{}{}
		}
	}
	if scanner.Err() != nil {
		c.addMissing("task/events/session-association-unreadable")
	}
}

func (c *bundleCollector) collectClaudeTranscripts(cfg config.AppConfig, sessionIDs []string) {
	if len(sessionIDs) == 0 {
		return
	}
	matches, err := runner.FindClaudeTranscriptPaths(cfg.ClaudeConfigDir, sessionIDs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.addMissing("claude-transcripts/unavailable")
	}
	for _, sessionID := range sessionIDs {
		c.addClaudeTranscriptMatches(sessionID, matches[sessionID])
	}
}

func (c *bundleCollector) addClaudeTranscriptMatches(sessionID string, paths []string) {
	sort.Strings(paths)
	switch len(paths) {
	case 0:
		c.addMissing(path.Join("claude-transcripts", safeArchiveComponent(sessionID)+".jsonl"))
	case 1:
		c.addFile(paths[0], path.Join("claude-transcripts", safeArchiveComponent(sessionID)+".jsonl"))
	default:
		for index, transcriptPath := range paths {
			archivePath := path.Join("claude-transcripts", safeArchiveComponent(sessionID), fmt.Sprintf("%02d.jsonl", index+1))
			c.addFile(transcriptPath, archivePath)
		}
	}
}

func (c *bundleCollector) collectCurrentState(cfg config.AppConfig, st *state.StateStore) {
	stateRoot := filepath.Clean(st.Path("."))
	entries, err := os.ReadDir(stateRoot)
	if err != nil {
		c.addMissing("current-state/state-root")
	} else {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				if bundleAggregateDirs[name] {
					continue
				}
				_ = c.addUnattributedTreeIfPresent(
					filepath.Join(stateRoot, name),
					path.Join("current-state", "diagnostics", name),
				)
				continue
			}
			if entry.Type().IsRegular() {
				archivePath := path.Join("current-state", "state", name)
				c.addFile(filepath.Join(stateRoot, name), archivePath)
				if name == "review-current-task.patch" {
					c.addUnattributed(archivePath)
				}
			}
		}
	}

	c.collectCurrentTaskDiff(cfg, st)

	var status bytes.Buffer
	if err := printStatus(st, &status); err != nil {
		c.addMissing("current-state/status.json")
	} else {
		c.addData("current-state/status.json", status.Bytes())
	}
	c.collectRepositoryAuthority(cfg, st)
}

func (c *bundleCollector) collectCurrentTaskDiff(cfg config.AppConfig, st *state.StateStore) {
	diff, available, err := taskdiff.Capture(cfg.RepoRoot, st)
	switch {
	case err != nil:
		c.addMissing("current-state/snapshot/task-diff.patch")
	case available:
		c.addData("current-state/snapshot/task-diff.patch", diff)
	}
}

func (c *bundleCollector) collectRepositoryAuthority(cfg config.AppConfig, st *state.StateStore) {
	for _, rel := range []string{"IMPLEMENTATION_PLAN.local.md", "IMPLEMENTATION_RULES.md", "IMPLEMENTATION_HISTORY.md"} {
		archivePath := path.Join("current-state", "repository-authority", filepath.ToSlash(rel))
		if !c.addRepositoryFile(cfg.RepoRoot, rel, archivePath) {
			c.addMissing(archivePath)
		}
	}
	activeTask := st.ReadOr("active-task", "")
	if activeTask == "" {
		return
	}
	archivePath := path.Join("current-state", "repository-authority", filepath.ToSlash(activeTask))
	if c.addRepositoryFile(cfg.RepoRoot, activeTask, archivePath) {
		return
	}
	taskID := st.ReadOr("task.id", "")
	if taskID != "" && taskAuthoritySnapshotMatches(st, taskID, activeTask) &&
		c.addFileIfPresent(st.TaskAuthorityContentPath(taskID), archivePath) {
		return
	}
	c.addMissing(archivePath)
}

func taskAuthoritySnapshotMatches(st *state.StateStore, taskID, activeTask string) bool {
	data, err := os.ReadFile(st.TaskAuthorityPathPath(taskID))
	return err == nil && strings.TrimSpace(string(data)) == activeTask
}

func (c *bundleCollector) addRepositoryFile(repoRoot, rel, archivePath string) bool {
	cleanRel := filepath.Clean(filepath.FromSlash(rel))
	if cleanRel == "." || filepath.IsAbs(cleanRel) || cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(filepath.Separator)) {
		return false
	}
	return c.addFileIfPresent(filepath.Join(repoRoot, cleanRel), archivePath)
}

func (c *bundleCollector) addFileIfPresent(sourcePath, archivePath string) bool {
	info, err := os.Lstat(sourcePath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			c.addUnreadable(cleanArchivePath(archivePath))
		}
		return false
	}
	if !info.Mode().IsRegular() {
		return false
	}
	c.addFile(sourcePath, archivePath)
	return true
}

func (c *bundleCollector) addFile(sourcePath, archivePath string) {
	archivePath = cleanArchivePath(archivePath)
	if archivePath == "" {
		return
	}
	c.entries[archivePath] = bundleEntry{SourcePath: sourcePath, ArchivePath: archivePath}
}

func (c *bundleCollector) addData(archivePath string, data []byte) {
	archivePath = cleanArchivePath(archivePath)
	if archivePath == "" {
		return
	}
	copied := append([]byte(nil), data...)
	c.entries[archivePath] = bundleEntry{ArchivePath: archivePath, Data: copied}
}

func (c *bundleCollector) addTreeIfPresent(sourceRoot, archiveRoot string) bool {
	return c.addTree(sourceRoot, archiveRoot, false)
}

func (c *bundleCollector) addUnattributedTreeIfPresent(sourceRoot, archiveRoot string) bool {
	return c.addTree(sourceRoot, archiveRoot, true)
}

func (c *bundleCollector) addTree(sourceRoot, archiveRoot string, unattributed bool) bool {
	info, err := os.Lstat(sourceRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	_ = filepath.WalkDir(sourceRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(sourceRoot, filePath)
		if err != nil {
			return nil
		}
		archivePath := path.Join(archiveRoot, filepath.ToSlash(rel))
		c.addFile(filePath, archivePath)
		if unattributed {
			c.addUnattributed(archivePath)
		}
		return nil
	})
	return true
}

func (c *bundleCollector) addMissing(value string) {
	if value != "" {
		c.missing[value] = struct{}{}
	}
}

func (c *bundleCollector) addUnattributed(value string) {
	if value != "" {
		c.unattributed[value] = struct{}{}
	}
}

func (c *bundleCollector) addUnreadable(value string) {
	if value != "" {
		c.unreadable[value] = struct{}{}
	}
}

func (c *bundleCollector) missingList() []string {
	return sortedSet(c.missing)
}

func (c *bundleCollector) unreadableList() []string {
	return sortedSet(c.unreadable)
}

func (c *bundleCollector) unattributedList() []string {
	return sortedSet(c.unattributed)
}

func (c *bundleCollector) includedListWithManifest() []string {
	result := make([]string, 0, len(c.entries)+2)
	for archivePath := range c.entries {
		result = append(result, archivePath)
	}
	result = append(result, "manifest.json", bundleCollectionEntryPath)
	sort.Strings(result)
	return result
}

func (c *bundleCollector) markInProgressEvidence(task bundleTask) {
	if !task.Current {
		return
	}
	for archivePath, entry := range c.entries {
		if bundleEvidenceStillAppending(archivePath) {
			entry.InProgress = true
			c.entries[archivePath] = entry
		}
	}
}

func bundleEvidenceStillAppending(archivePath string) bool {
	if archivePath == "task/task-stats.json" {
		return true
	}
	for _, prefix := range bundleInProgressPrefixes {
		if strings.HasPrefix(archivePath, prefix) {
			return true
		}
	}
	return false
}

func (c *bundleCollector) entryList() []bundleEntry {
	result := make([]bundleEntry, 0, len(c.entries))
	for _, entry := range c.entries {
		result = append(result, entry)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ArchivePath < result[j].ArchivePath })
	return result
}

func bundleArchivePath(cfg config.AppConfig, taskID string) (string, error) {
	if safeArchiveComponent(taskID) != taskID {
		return "", fmt.Errorf("bundle用task IDが不正です: %q", taskID)
	}
	exportDir := filepath.Join(filepath.Dir(cfg.StateBase), "exports", cfg.RepoHash)
	absoluteDir, err := filepath.Abs(exportDir)
	if err != nil {
		return "", fmt.Errorf("bundle export pathを解決できません: %w", err)
	}
	return filepath.Join(absoluteDir, taskID+".zip"), nil
}

func writeBundleArchiveAtomically(archivePath string, entries []bundleEntry, buildManifest func(bundleCollectionIndex) bundleManifest) error {
	dir := filepath.Dir(archivePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("bundle export directoryを作成できません: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("bundle export directoryの権限を設定できません: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".bundle-*.tmp")
	if err != nil {
		return fmt.Errorf("bundle一時ファイルを作成できません: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}

	index := bundleCollectionIndex{CollectorRevision: bundleCollectorRevision()}
	zipWriter := zip.NewWriter(temp)
	if err := writeBundleArchiveContents(zipWriter, entries, index, buildManifest); err != nil {
		_ = zipWriter.Close()
		_ = temp.Close()
		return err
	}
	if err := zipWriter.Close(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("bundle ZIPを閉じられません: %w", err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("bundle ZIPを同期できません: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("bundle一時ファイルを閉じられません: %w", err)
	}
	if err := os.Rename(tempPath, archivePath); err != nil {
		return fmt.Errorf("bundle ZIPを配置できません: %w", err)
	}
	return nil
}

func writeBundleArchiveContents(
	zipWriter *zip.Writer,
	entries []bundleEntry,
	index bundleCollectionIndex,
	buildManifest func(bundleCollectionIndex) bundleManifest,
) error {
	for _, entry := range entries {
		collected, err := writeBundleEntry(zipWriter, entry)
		if err != nil {
			return err
		}
		index.Entries = append(index.Entries, collected)
	}
	if err := appendBundleManifestEntry(zipWriter, buildManifest(index)); err != nil {
		return err
	}
	return appendBundleCollectionIndex(zipWriter, index)
}

func appendBundleManifestEntry(zipWriter *zip.Writer, manifest bundleManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle manifestをJSON化できません: %w", err)
	}
	header := &zip.FileHeader{Name: "manifest.json", Method: zip.Deflate}
	header.SetMode(0o600)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("bundle entry manifest.jsonを作成できません: %w", err)
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("bundle entry manifest.jsonを書き込めません: %w", err)
	}
	return nil
}

func appendBundleCollectionIndex(zipWriter *zip.Writer, index bundleCollectionIndex) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle collection indexをJSON化できません: %w", err)
	}
	header := &zip.FileHeader{Name: bundleCollectionEntryPath, Method: zip.Deflate}
	header.SetMode(0o600)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("bundle entry %sを作成できません: %w", bundleCollectionEntryPath, err)
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("bundle entry %sを書き込めません: %w", bundleCollectionEntryPath, err)
	}
	return nil
}

func writeBundleEntry(zipWriter *zip.Writer, entry bundleEntry) (bundleCollectedEntry, error) {
	collected := bundleCollectedEntry{
		Path:        entry.ArchivePath,
		CollectedAt: time.Now().UTC().Format(time.RFC3339Nano),
		InProgress:  entry.InProgress,
	}
	if info, err := os.Lstat(entry.SourcePath); err == nil {
		collected.SourceModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
	}
	header := &zip.FileHeader{Name: entry.ArchivePath, Method: zip.Deflate}
	header.SetMode(0o600)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return collected, fmt.Errorf("bundle entry %sを作成できません: %w", entry.ArchivePath, err)
	}
	measure := newBundleEntryMeasure(entry.ArchivePath)
	if entry.SourcePath == "" {
		if _, err := measure.WriteTo(writer, entry.Data); err != nil {
			return collected, fmt.Errorf("bundle entry %sを書き込めません: %w", entry.ArchivePath, err)
		}
	} else {
		if err := measure.CopyFrom(writer, entry.SourcePath); err != nil {
			return collected, err
		}
	}
	measure.apply(&collected)
	return collected, nil
}

func regularFileExists(filePath string) bool {
	info, err := os.Lstat(filePath)
	return err == nil && info.Mode().IsRegular()
}

func cleanArchivePath(value string) string {
	cleaned := path.Clean(strings.TrimPrefix(filepath.ToSlash(value), "/"))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

func safeArchiveComponent(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func sortedSet[T ~string](values map[T]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, string(value))
	}
	sort.Strings(result)
	return result
}
