package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type bundleCodexSource struct {
	Class            string   `json:"class"`
	Status           string   `json:"status"`
	Sources          []string `json:"sources,omitempty"`
	ArchivePaths     []string `json:"archive_paths,omitempty"`
	ThreadIDs        []string `json:"thread_ids,omitempty"`
	SpansTasks       bool     `json:"spans_tasks,omitempty"`
	AssociationBasis string   `json:"association_basis,omitempty"`
	Detail           string   `json:"detail,omitempty"`
}

type codexRollout struct {
	AbsolutePath   string
	HomeRelative   string
	ID             string
	ParentThreadID string
	GuardianSource bool
	FirstTimestamp time.Time
}

type codexSessionMeta struct {
	Timestamp string                  `json:"timestamp"`
	Type      string                  `json:"type"`
	Payload   codexSessionMetaPayload `json:"payload"`
}

type codexSessionMetaPayload struct {
	ID             string          `json:"id"`
	ParentThreadID string          `json:"parent_thread_id"`
	Source         json.RawMessage `json:"source"`
}

type codexRolloutSource struct {
	Subagent *codexRolloutSubagent `json:"subagent"`
}

type codexRolloutSubagent struct {
	Other string `json:"other"`
}

type codexLogRow struct {
	TS              int64   `json:"ts"`
	TSNanos         int64   `json:"ts_nanos"`
	Level           string  `json:"level"`
	Target          string  `json:"target"`
	ThreadID        string  `json:"thread_id"`
	ProcessUUID     *string `json:"process_uuid"`
	EstimatedBytes  int64   `json:"estimated_bytes"`
	FeedbackLogBody *string `json:"feedback_log_body"`
}

type codexAssociation struct {
	ParentStatus   string
	ParentPath     string
	ParentSource   string
	ParentThreadID string
	GuardianStatus string
	GuardianDetail string
	Guardians      []codexRollout
	Basis          string
	Detail         string
}

type codexLogExtraction struct {
	ThreadID string
	Rows     []codexLogRow
}

const (
	codexStatusIncluded    = "included"
	codexStatusMissing     = "missing"
	codexStatusUnavailable = "unavailable"
	codexStatusAmbiguous   = "ambiguous"

	codexClassParentSession     = "parent-session"
	codexClassGuardianChild     = "guardian-child"
	codexClassAppServerLogs     = "app-server-logs"
	codexClassProcessProjection = "process-projection"
	codexClassRuntimeSettings   = "runtime-settings"
	codexClassAttachments       = "attachments"

	codexAssociationBasis         = "stored-parent-identity"
	codexExplicitAssociationBasis = "explicit-bundle-parent-thread-id"
	bundleParentThreadIDEnv       = "GLM_WORKER_BUNDLE_PARENT_THREAD_ID"

	codexBackgroundTerminalMaxTimeoutKey = "background_terminal_max_timeout"
)

func codexSourceIsGuardian(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var source codexRolloutSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return false
	}
	return source.Subagent != nil && source.Subagent.Other == "guardian"
}

func (c *bundleCollector) collectCodexEvidence(cfg config.AppConfig, task bundleTask) (codexAssociation, []bundleCodexSource) {
	association := resolveCodexAssociation(cfg.CodexConfigDir, task)
	threads := c.addCodexRolloutEvidence(association)
	logs := c.addCodexLogEvidence(cfg.CodexConfigDir, task, threads, association.ParentStatus, association.Basis)
	process := c.addCodexProcessEvidence(cfg.CodexConfigDir, task, threads, association.ParentStatus, association.Basis)
	runtime := c.addCodexRuntimeSettingsEvidence(cfg.CodexConfigDir)
	attachments := bundleCodexSource{
		Class:   codexClassAttachments,
		Status:  codexStatusUnavailable,
		Sources: []string{"attachments"},
		Detail:  "schema-different: no deterministic structured association from rollouts to attachment storage",
	}
	return association, []bundleCodexSource{codexParentSource(association), codexGuardianSource(association), logs, process, runtime, attachments}
}

func resolveCodexAssociation(codexHome string, task bundleTask) codexAssociation {
	threadID, basis, failure := selectCodexParentIdentity(task)
	if failure != nil {
		return *failure
	}
	if threadID == "" {
		return codexAssociation{ParentStatus: codexStatusMissing, Detail: "parent Codex identity is not recorded for this task"}
	}
	if !codexDirExists(codexHome) {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: basis, Detail: "codex home is not present"}
	}
	rollouts, err := scanCodexRollouts(codexHome)
	if err != nil {
		return codexAssociation{ParentStatus: codexStatusUnavailable, Basis: basis, Detail: "codex rollout enumeration failed: " + err.Error()}
	}
	return buildCodexAssociation(matchingCodexRollouts(rollouts, threadID), rollouts, basis, task)
}

func selectCodexParentIdentity(task bundleTask) (string, string, *codexAssociation) {
	threadID := task.Stats.ParentCodexThreadID
	explicitThreadID := strings.TrimSpace(os.Getenv(bundleParentThreadIDEnv))
	if explicitThreadID == "" {
		return threadID, codexAssociationBasis, nil
	}
	if !state.ValidUUIDFormat(explicitThreadID) {
		failure := codexAssociation{ParentStatus: codexStatusUnavailable, Detail: bundleParentThreadIDEnv + " is not a canonical UUID"}
		return "", "", &failure
	}
	if threadID != "" && threadID != explicitThreadID {
		failure := codexAssociation{ParentStatus: codexStatusAmbiguous, Detail: "explicit bundle parent thread ID conflicts with the stored parent identity"}
		return "", "", &failure
	}
	if threadID != "" {
		return threadID, codexAssociationBasis, nil
	}
	return explicitThreadID, codexExplicitAssociationBasis, nil
}

func matchingCodexRollouts(rollouts []codexRollout, threadID string) []codexRollout {
	matches := make([]codexRollout, 0, 1)
	for _, rollout := range rollouts {
		if rollout.ID == threadID {
			matches = append(matches, rollout)
		}
	}
	return matches
}

func buildCodexAssociation(matches, rollouts []codexRollout, basis string, task bundleTask) codexAssociation {
	switch len(matches) {
	case 0:
		detail := "no rollout has session_meta.id equal to the stored parent thread ID"
		if basis == codexExplicitAssociationBasis {
			detail = "no rollout has session_meta.id equal to the explicit bundle parent thread ID"
		}
		return codexAssociation{ParentStatus: codexStatusMissing, Basis: basis, Detail: detail}
	case 1:
		return includedCodexAssociation(matches[0], rollouts, basis, task)
	default:
		detail := fmt.Sprintf("%d rollouts share the stored parent thread ID", len(matches))
		if basis == codexExplicitAssociationBasis {
			detail = fmt.Sprintf("%d rollouts share the explicit bundle parent thread ID", len(matches))
		}
		return codexAssociation{ParentStatus: codexStatusAmbiguous, Basis: basis, Detail: detail}
	}
}

func includedCodexAssociation(parent codexRollout, rollouts []codexRollout, basis string, task bundleTask) codexAssociation {
	start, end := taskWindow(task)
	guardians, qualifying := selectCodexGuardianChildren(rollouts, parent, start, end)
	detail := ""
	if basis == codexExplicitAssociationBasis {
		detail = "parent identity supplied explicitly for this bundle; task state was not modified"
	}
	association := codexAssociation{
		ParentStatus:   codexStatusIncluded,
		ParentPath:     parent.AbsolutePath,
		ParentSource:   parent.HomeRelative,
		ParentThreadID: parent.ID,
		GuardianStatus: codexStatusIncluded,
		Guardians:      guardians,
		Basis:          basis,
		Detail:         detail,
	}
	if qualifying > len(guardians) {
		association.GuardianStatus = codexStatusAmbiguous
		association.Guardians = nil
		association.GuardianDetail = fmt.Sprintf("%d rollouts share a direct guardian thread ID", qualifying)
	}
	return association
}

func taskWindow(task bundleTask) (time.Time, time.Time) {
	start := task.Stats.StartedAt.UTC()
	end := time.Now().UTC()
	if task.Stats.ArchivedAt != nil && task.Stats.ArchivedAt.Before(end) {
		end = task.Stats.ArchivedAt.UTC()
	}
	return start, end
}

func selectCodexGuardianChildren(rollouts []codexRollout, parent codexRollout, start, end time.Time) ([]codexRollout, int) {
	unique := make(map[string]codexRollout)
	qualifying := 0
	for _, rollout := range rollouts {
		if rollout.ParentThreadID != parent.ID || !rollout.GuardianSource {
			continue
		}
		last, ok := codexRolloutLastTimestamp(rollout.AbsolutePath)
		if !ok || rollout.FirstTimestamp.After(end) || last.Before(start) {
			continue
		}
		qualifying++
		unique[rollout.ID] = rollout
	}
	children := make([]codexRollout, 0, len(unique))
	for _, rollout := range unique {
		children = append(children, rollout)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
	return children, qualifying
}

func scanCodexRollouts(codexHome string) ([]codexRollout, error) {
	rollouts := make([]codexRollout, 0)
	for _, root := range []string{filepath.Join(codexHome, "sessions"), filepath.Join(codexHome, "archived_sessions")} {
		collected, err := scanCodexRolloutRoot(codexHome, root)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, collected...)
	}
	return rollouts, nil
}

func scanCodexRolloutRoot(codexHome, root string) ([]codexRollout, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, nil
	}
	rollouts := make([]codexRollout, 0)
	err = filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		if rollout, ok := readCodexRolloutMeta(codexHome, filePath); ok {
			rollouts = append(rollouts, rollout)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rollouts, nil
}

func readCodexRolloutMeta(codexHome, filePath string) (codexRollout, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		return codexRollout{}, false
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if !scanner.Scan() {
		return codexRollout{}, false
	}
	var meta codexSessionMeta
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil || meta.Type != "session_meta" {
		return codexRollout{}, false
	}
	first, err := time.Parse(time.RFC3339Nano, meta.Timestamp)
	if err != nil {
		first = time.Time{}
	}
	rel, relErr := filepath.Rel(codexHome, filePath)
	if relErr != nil {
		return codexRollout{}, false
	}
	return codexRollout{
		AbsolutePath:   filePath,
		HomeRelative:   filepath.ToSlash(rel),
		ID:             meta.Payload.ID,
		ParentThreadID: meta.Payload.ParentThreadID,
		GuardianSource: codexSourceIsGuardian(meta.Payload.Source),
		FirstTimestamp: first,
	}, true
}

func codexRolloutLastTimestamp(filePath string) (time.Time, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		return time.Time{}, false
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return time.Time{}, false
	}
	const tailSize = 16 * 1024
	buffer := make([]byte, tailSize)
	offset := info.Size() - tailSize
	if offset < 0 {
		offset = 0
	}
	read, err := file.ReadAt(buffer, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return time.Time{}, false
	}
	return lastTimestampFromTail(buffer[:read])
}

func lastTimestampFromTail(tail []byte) (time.Time, bool) {
	lines := bytes.Split(tail, []byte("\n"))
	for index := len(lines) - 1; index >= 0; index-- {
		line := bytes.TrimSpace(lines[index])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var record struct {
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &record); err != nil || record.Timestamp == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339Nano, record.Timestamp)
		if err != nil {
			continue
		}
		return parsed, true
	}
	return time.Time{}, false
}

func codexDirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func (c *bundleCollector) addCodexRolloutEvidence(association codexAssociation) []string {
	threads := make([]string, 0, 1+len(association.Guardians))
	if association.ParentStatus != codexStatusIncluded {
		return threads
	}
	c.addFile(association.ParentPath, codexRolloutArchivePath(association.ParentThreadID))
	threads = append(threads, association.ParentThreadID)
	for _, guardian := range association.Guardians {
		c.addFile(guardian.AbsolutePath, codexGuardianArchivePath(guardian.ID))
		threads = append(threads, guardian.ID)
	}
	return threads
}

func codexRolloutArchivePath(threadID string) string {
	return path.Join("codex-parent", "rollouts", safeArchiveComponent(threadID)+".jsonl")
}

func codexGuardianArchivePath(threadID string) string {
	return path.Join("codex-parent", "guardians", safeArchiveComponent(threadID)+".jsonl")
}

func codexParentSource(association codexAssociation) bundleCodexSource {
	source := bundleCodexSource{
		Class:  codexClassParentSession,
		Status: association.ParentStatus,
		Detail: association.Detail,
	}
	if association.ParentStatus != codexStatusIncluded {
		return source
	}
	source.Sources = []string{association.ParentSource}
	source.ArchivePaths = []string{codexRolloutArchivePath(association.ParentThreadID)}
	source.ThreadIDs = []string{association.ParentThreadID}
	source.SpansTasks = true
	source.AssociationBasis = association.Basis
	return source
}

func codexGuardianSource(association codexAssociation) bundleCodexSource {
	source := bundleCodexSource{Class: codexClassGuardianChild, Status: association.ParentStatus}
	if association.ParentStatus != codexStatusIncluded {
		source.Detail = "parent session is not associated: " + association.Detail
		return source
	}
	source.Status = association.GuardianStatus
	source.Detail = association.GuardianDetail
	if association.GuardianStatus == codexStatusAmbiguous {
		return source
	}
	source.AssociationBasis = association.Basis
	source.Detail = fmt.Sprintf("%d direct guardian children overlap the task window", len(association.Guardians))
	for _, guardian := range association.Guardians {
		source.Sources = append(source.Sources, guardian.HomeRelative)
		source.ArchivePaths = append(source.ArchivePaths, codexGuardianArchivePath(guardian.ID))
		source.ThreadIDs = append(source.ThreadIDs, guardian.ID)
	}
	return source
}

func (c *bundleCollector) addCodexLogEvidence(codexHome string, task bundleTask, threads []string, parentStatus, associationBasis string) bundleCodexSource {
	source := bundleCodexSource{Class: codexClassAppServerLogs, Sources: []string{"logs_2.sqlite"}}
	if parentStatus != codexStatusIncluded {
		source.Status = parentStatus
		source.Detail = "parent session is not associated: no associated Codex thread for bounded extraction"
		return source
	}
	if len(threads) == 0 {
		source.Status = codexStatusMissing
		source.Detail = "no associated Codex thread for bounded extraction"
		return source
	}
	source.Status = codexStatusIncluded
	source.AssociationBasis = associationBasis
	dbPath := filepath.Join(codexHome, "logs_2.sqlite")
	start, end := taskWindow(task)
	extractions := make([]codexLogExtraction, 0, len(threads))
	totalRows := 0
	for _, threadID := range threads {
		rows, err := extractCodexLogRows(dbPath, threadID, start, end)
		if err != nil {
			return bundleCodexSource{
				Class:            codexClassAppServerLogs,
				Status:           codexStatusUnavailable,
				Sources:          []string{"logs_2.sqlite"},
				AssociationBasis: associationBasis,
				Detail:           "bounded log extraction failed: " + err.Error(),
			}
		}
		extractions = append(extractions, codexLogExtraction{ThreadID: threadID, Rows: rows})
		totalRows += len(rows)
	}
	for _, extraction := range extractions {
		source.ArchivePaths = append(source.ArchivePaths, path.Join("codex-parent", "logs", safeArchiveComponent(extraction.ThreadID)+".jsonl"))
		source.ThreadIDs = append(source.ThreadIDs, extraction.ThreadID)
		c.addCodexLogEntry(extraction.ThreadID, extraction.Rows)
	}
	source.Detail = fmt.Sprintf("extracted %d rows bounded by the associated threads and the task time range", totalRows)
	return source
}

func (c *bundleCollector) addCodexLogEntry(threadID string, rows []codexLogRow) {
	var buffer bytes.Buffer
	for _, row := range rows {
		encoded, err := json.Marshal(row)
		if err != nil {
			return
		}
		buffer.Write(encoded)
		buffer.WriteByte('\n')
	}
	c.addData(path.Join("codex-parent", "logs", safeArchiveComponent(threadID)+".jsonl"), buffer.Bytes())
}

func extractCodexLogRows(dbPath, threadID string, start, end time.Time) ([]codexLogRow, error) {
	if !state.ValidUUIDFormat(threadID) {
		return nil, fmt.Errorf("thread ID is not a canonical UUID: %q", threadID)
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		return nil, fmt.Errorf("logs_2.sqlite is not present")
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("logs_2.sqlite is not a regular file")
	}
	if _, err := exec.LookPath("sqlite3"); err != nil {
		return nil, fmt.Errorf("sqlite3 binary not found")
	}
	query := fmt.Sprintf(
		"SELECT ts, ts_nanos, level, target, thread_id, process_uuid, estimated_bytes, feedback_log_body FROM logs WHERE thread_id = '%s' AND ts >= %d AND ts <= %d ORDER BY ts, ts_nanos, id;",
		threadID, start.Unix(), end.Unix(),
	)
	var stderr bytes.Buffer
	cmd := exec.Command("sqlite3", "-readonly", "-json", dbPath, query)
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 query failed: %s", strings.TrimSpace(stderr.String()))
	}
	if len(bytes.TrimSpace(output)) == 0 {
		return nil, nil
	}
	var rows []codexLogRow
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("sqlite3 -json decode failed: %w", err)
	}
	return rows, nil
}

func (c *bundleCollector) addCodexProcessEvidence(codexHome string, task bundleTask, threads []string, parentStatus, associationBasis string) bundleCodexSource {
	source := bundleCodexSource{Class: codexClassProcessProjection, Sources: []string{"process_manager/chat_processes.json"}}
	if !task.Current {
		source.Status = codexStatusUnavailable
		source.Detail = "process projection is volatile bundle-time evidence for the current task only"
		return source
	}
	if parentStatus != codexStatusIncluded {
		source.Status = parentStatus
		source.Detail = "parent session is not associated: matching process rows cannot be selected"
		return source
	}
	source.AssociationBasis = associationBasis
	matched, threadIDs, err := readCodexChatProcesses(filepath.Join(codexHome, "process_manager", "chat_processes.json"), threads)
	if err != nil {
		source.Status = codexStatusUnavailable
		source.Detail = "process projection unavailable: " + err.Error()
		return source
	}
	if len(matched) == 0 {
		source.Status = codexStatusMissing
		source.Detail = "no chat process rows match the associated threads at bundle time"
		return source
	}
	source.Status = codexStatusIncluded
	source.ThreadIDs = threadIDs
	source.ArchivePaths = []string{path.Join("codex-parent", "process-manager", "chat_processes.json")}
	encoded, err := json.MarshalIndent(matched, "", "  ")
	if err != nil {
		source.Status = codexStatusUnavailable
		source.Detail = "process projection encode failed: " + err.Error()
		source.ArchivePaths = nil
		return source
	}
	c.addData(source.ArchivePaths[0], append(encoded, '\n'))
	return source
}

func readCodexChatProcesses(filePath string, threads []string) ([]map[string]any, []string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("chat_processes.json is not readable")
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, fmt.Errorf("chat_processes.json is not a JSON array")
	}
	targets := make(map[string]struct{}, len(threads))
	for _, threadID := range threads {
		targets[threadID] = struct{}{}
	}
	matched := make([]map[string]any, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		conversationID, _ := row["conversationId"].(string)
		if _, ok := targets[conversationID]; !ok {
			continue
		}
		matched = append(matched, row)
		seen[conversationID] = struct{}{}
	}
	return matched, sortedSet(seen), nil
}

func (c *bundleCollector) addCodexRuntimeSettingsEvidence(codexHome string) bundleCodexSource {
	source := bundleCodexSource{Class: codexClassRuntimeSettings, Sources: []string{"config.toml"}}
	value, err := readCodexBackgroundTerminalMaxTimeout(filepath.Join(codexHome, "config.toml"))
	switch {
	case err != nil:
		source.Status = codexStatusUnavailable
		source.Detail = "runtime settings unavailable: " + err.Error()
	case value == nil:
		source.Status = codexStatusMissing
		source.Detail = codexBackgroundTerminalMaxTimeoutKey + " is not present in config.toml"
	default:
		source.Status = codexStatusIncluded
		source.ArchivePaths = []string{path.Join("codex-parent", "runtime-settings.json")}
		encoded, encodeErr := json.MarshalIndent(map[string]int64{codexBackgroundTerminalMaxTimeoutKey: *value}, "", "  ")
		if encodeErr != nil {
			source.Status = codexStatusUnavailable
			source.Detail = "runtime settings encode failed: " + encodeErr.Error()
			source.ArchivePaths = nil
			return source
		}
		c.addData(source.ArchivePaths[0], append(encoded, '\n'))
	}
	return source
}

func readCodexBackgroundTerminalMaxTimeout(configPath string) (*int64, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config.toml is not readable")
	}
	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "[") {
			return nil, nil
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(key) != codexBackgroundTerminalMaxTimeoutKey {
			continue
		}
		parsed, parseErr := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(value), "_", ""), 10, 64)
		if parseErr != nil {
			continue
		}
		return &parsed, nil
	}
	return nil, nil
}
