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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type bundleOutput struct {
	TaskID             string   `json:"task_id"`
	TaskStatus         string   `json:"task_status"`
	ArchivePath        string   `json:"archive_path"`
	EvidenceStatus     string   `json:"evidence_status"`
	ClaudeSessionIDs   []string `json:"claude_session_ids"`
	InFlightModelCalls int      `json:"in_flight_model_calls,omitempty"`
	Missing            []string `json:"missing"`
	Unattributed       []string `json:"unattributed,omitempty"`
}

type bundleManifest struct {
	Format             string   `json:"format"`
	TaskID             string   `json:"task_id"`
	TaskStatus         string   `json:"task_status"`
	CurrentTask        bool     `json:"current_task"`
	EvidenceStatus     string   `json:"evidence_status"`
	ClaudeSessionIDs   []string `json:"claude_session_ids"`
	InFlightModelCalls int      `json:"in_flight_model_calls,omitempty"`
	Included           []string `json:"included"`
	Missing            []string `json:"missing"`
	Unattributed       []string `json:"unattributed,omitempty"`
	CreatedAt          string   `json:"created_at"`
}

type bundleTask struct {
	ID      string
	Status  string
	Current bool
	Stats   state.TaskStats
}

type bundleEntry struct {
	SourcePath  string
	ArchivePath string
	Data        []byte
}

type bundleCollector struct {
	entries      map[string]bundleEntry
	missing      map[string]struct{}
	unattributed map[string]struct{}
}

const bundleFormat = "glm-worker-task-bundle-v2"

var bundleAggregateDirs = map[string]bool{
	"artifacts":      true,
	"events":         true,
	"rounds":         true,
	"stats":          true,
	"task-authority": true,
	"telemetry":      true,
}

func printBundle(cfg config.AppConfig, st *state.StateStore, requestedTaskID string, stdout io.Writer) error {
	task, err := selectBundleTask(st, requestedTaskID)
	if err != nil {
		return err
	}

	collector := newBundleCollector()
	collector.collectTaskEvidence(st, task)
	sessionIDs := collector.collectTaskSessions(st, task)
	collector.collectClaudeTranscripts(cfg, sessionIDs)
	if task.Current {
		collector.collectCurrentState(cfg, st)
	}

	archivePath, err := bundleArchivePath(cfg, task.ID)
	if err != nil {
		return err
	}
	missing := collector.missingList()
	unattributed := collector.unattributedList()
	evidenceStatus := "complete"
	if len(missing) > 0 {
		evidenceStatus = "incomplete"
	}
	inFlightModelCalls := bundleInFlightModelCalls(st, task)
	manifest := bundleManifest{
		Format:             bundleFormat,
		TaskID:             task.ID,
		TaskStatus:         task.Status,
		CurrentTask:        task.Current,
		EvidenceStatus:     evidenceStatus,
		ClaudeSessionIDs:   sessionIDs,
		InFlightModelCalls: inFlightModelCalls,
		Included:           collector.includedListWithManifest(),
		Missing:            missing,
		Unattributed:       unattributed,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
	}
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("bundle manifestをJSON化できません: %w", err)
	}
	collector.addData("manifest.json", append(manifestData, '\n'))
	if err := writeBundleArchiveAtomically(archivePath, collector.entryList()); err != nil {
		return err
	}

	return writeJSON(stdout, bundleOutput{
		TaskID:             task.ID,
		TaskStatus:         task.Status,
		ArchivePath:        archivePath,
		EvidenceStatus:     evidenceStatus,
		ClaudeSessionIDs:   sessionIDs,
		InFlightModelCalls: inFlightModelCalls,
		Missing:            missing,
		Unattributed:       unattributed,
	})
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
		unattributed: make(map[string]struct{}),
	}
}

func (c *bundleCollector) collectTaskEvidence(st *state.StateStore, task bundleTask) {
	c.addFileIfPresent(st.ModelCallLogPath(task.ID), path.Join("task", "telemetry", task.ID+".jsonl"))
	c.addFileIfPresent(st.TaskEventLogPath(task.ID), path.Join("task", "events", task.ID+".jsonl"))
	c.addFileIfPresent(st.TaskLiveStatusPath(task.ID), path.Join("task", "events", task.ID+".live.json"))
	c.addFileIfPresent(st.RoundLogPath(task.ID), path.Join("task", "rounds", task.ID+".jsonl"))
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
	matches, err := findClaudeTranscripts(cfg.ClaudeConfigDir, sessionIDs)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		c.addMissing("claude-transcripts/unavailable")
	}
	for _, sessionID := range sessionIDs {
		c.addClaudeTranscriptMatches(sessionID, matches[sessionID])
	}
}

func findClaudeTranscripts(configDir string, sessionIDs []string) (map[string][]string, error) {
	targets := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		targets[sessionID] = struct{}{}
	}
	matches := make(map[string][]string, len(sessionIDs))
	projectsRoot := filepath.Join(configDir, "projects")
	err := filepath.WalkDir(projectsRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		if _, ok := targets[sessionID]; ok {
			matches[sessionID] = append(matches[sessionID], filePath)
		}
		return nil
	})
	return matches, err
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
	if err != nil || !info.Mode().IsRegular() {
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

func (c *bundleCollector) missingList() []string {
	return sortedSet(c.missing)
}

func (c *bundleCollector) unattributedList() []string {
	return sortedSet(c.unattributed)
}

func (c *bundleCollector) includedListWithManifest() []string {
	result := make([]string, 0, len(c.entries)+1)
	for archivePath := range c.entries {
		result = append(result, archivePath)
	}
	result = append(result, "manifest.json")
	sort.Strings(result)
	return result
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

func writeBundleArchiveAtomically(archivePath string, entries []bundleEntry) error {
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

	zipWriter := zip.NewWriter(temp)
	for _, entry := range entries {
		if err := writeBundleEntry(zipWriter, entry); err != nil {
			_ = zipWriter.Close()
			_ = temp.Close()
			return err
		}
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

func writeBundleEntry(zipWriter *zip.Writer, entry bundleEntry) error {
	header := &zip.FileHeader{Name: entry.ArchivePath, Method: zip.Deflate}
	header.SetMode(0o600)
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("bundle entry %sを作成できません: %w", entry.ArchivePath, err)
	}
	if entry.SourcePath == "" {
		if _, err := writer.Write(entry.Data); err != nil {
			return fmt.Errorf("bundle entry %sを書き込めません: %w", entry.ArchivePath, err)
		}
		return nil
	}
	file, err := os.Open(entry.SourcePath)
	if err != nil {
		return fmt.Errorf("bundle source %sを開けません: %w", entry.SourcePath, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(writer, file); err != nil {
		return fmt.Errorf("bundle source %sをコピーできません: %w", entry.SourcePath, err)
	}
	return nil
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
