package runner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type workerBuildIdentity struct {
	revision string
	modified *bool
}

type claudeVersionScope struct {
	sessionID    string
	absent       bool
	ambiguous    bool
	path         string
	identity     os.FileInfo
	size         int64
	prefixSHA256 string
	lineBounded  bool
}

const claudeTranscriptVersionTailBytes = 32 * 1024

const claudeVersionPrefixAnchorBytes = 4 * 1024

const claudeVersionMaxBytes = 128

const (
	claudeVersionSourceTranscript = "session-transcript"
	claudeVersionSourceUnknown    = "unknown"
)

var (
	workerBuildOnce  sync.Once
	workerBuildValue workerBuildIdentity
)

func (r *ClaudeRunner) callRuntimeEnvironment(isolationArgs string, settingEnv map[string]string, observedAt time.Time) *state.CallRuntime {
	environment := &state.CallRuntime{
		ClaudeBin:                r.config.ClaudeBin,
		InstructionSurfaceDigest: r.instructionSurfaceDigest,
		IsolationSettingsDigest:  digestOf([]byte(isolationArgs)),
		SettingEnvKeys:           sortedEnvKeys(settingEnv),
		EnvironmentObservedAt:    observedAt.UTC().Format(time.RFC3339Nano),
	}
	if resolved, err := exec.LookPath(r.config.ClaudeBin); err == nil {
		environment.ClaudeBinResolved = resolved
		if info, statErr := os.Stat(resolved); statErr == nil {
			environment.ClaudeBinBytes = info.Size()
			environment.ClaudeBinModifiedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
	}
	identity := workerBuildIdentityFromGoBuild()
	environment.WorkerRevision = identity.revision
	environment.WorkerModified = identity.modified
	return environment
}

func (r *ClaudeRunner) beginClaudeVersionScope(sessionID string) claudeVersionScope {
	scope := claudeVersionScope{sessionID: sessionID}
	if sessionID == "" {
		scope.ambiguous = true
		return scope
	}
	matches, err := FindClaudeTranscriptPaths(r.config.ClaudeConfigDir, []string{sessionID})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			scope.absent = true
			return scope
		}
		scope.ambiguous = true
		return scope
	}
	if len(matches[sessionID]) > 1 {
		scope.ambiguous = true
		return scope
	}
	if len(matches[sessionID]) == 0 {
		scope.absent = true
		return scope
	}
	scope.path = matches[sessionID][0]
	file, err := os.Open(scope.path)
	if err != nil {
		scope.ambiguous = true
		return scope
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		scope.ambiguous = true
		return scope
	}
	scope.identity = info
	scope.size = info.Size()
	prefix, bounded, err := transcriptPrefixIdentity(file, scope.size)
	if err != nil {
		scope.ambiguous = true
		return scope
	}
	scope.prefixSHA256 = prefix
	scope.lineBounded = bounded
	return scope
}

func observeClaudeVersion(claudeConfigDir string, scope claudeVersionScope) (string, string) {
	version, ok := claudeVersionFromAppendedRecords(claudeConfigDir, scope)
	if !ok {
		return "", claudeVersionSourceUnknown
	}
	if len(version) > claudeVersionMaxBytes {
		version = version[:claudeVersionMaxBytes]
	}
	return version, claudeVersionSourceTranscript
}

func claudeVersionFromAppendedRecords(claudeConfigDir string, scope claudeVersionScope) (string, bool) {
	if scope.ambiguous {
		return "", false
	}
	file, info, ok := openVersionTranscript(claudeConfigDir, scope)
	if !ok {
		return "", false
	}
	defer func() { _ = file.Close() }()
	if scope.absent {
		return versionFromAppendedRange(file, 0, info.Size(), true)
	}
	start, end, sameFile := sameInodeAppendedRange(file, scope, info)
	if !sameFile {
		return "", false
	}
	return versionFromAppendedRange(file, start, end, scope.lineBounded)
}

func openVersionTranscript(claudeConfigDir string, scope claudeVersionScope) (*os.File, os.FileInfo, bool) {
	matches, err := FindClaudeTranscriptPaths(claudeConfigDir, []string{scope.sessionID})
	if err != nil || len(matches[scope.sessionID]) != 1 {
		return nil, nil, false
	}
	if !scope.absent && matches[scope.sessionID][0] != scope.path {
		return nil, nil, false
	}
	file, err := os.Open(matches[scope.sessionID][0])
	if err != nil {
		return nil, nil, false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, nil, false
	}
	return file, info, true
}

func sameInodeAppendedRange(file *os.File, scope claudeVersionScope, info os.FileInfo) (int64, int64, bool) {
	if !os.SameFile(scope.identity, info) || info.Size() <= scope.size {
		return 0, 0, false
	}
	prefix, _, err := transcriptPrefixIdentity(file, scope.size)
	if err != nil || prefix != scope.prefixSHA256 {
		return 0, 0, false
	}
	return scope.size, info.Size(), true
}

func versionFromAppendedRange(file *os.File, start, end int64, startLineBounded bool) (string, bool) {
	windowStart := end - claudeTranscriptVersionTailBytes
	if windowStart < start {
		windowStart = start
	}
	data, err := readFileRange(file, windowStart, end)
	if err != nil {
		return "", false
	}
	segments := strings.Split(string(data), "\n")
	complete := segments[:len(segments)-1]
	skipFirst := !startLineBounded || windowStart > start
	if skipFirst && len(complete) > 0 {
		complete = complete[1:]
	}
	version := ""
	found := false
	for _, line := range complete {
		if value, ok := transcriptLineVersion(line); ok {
			version = value
			found = true
		}
	}
	return version, found
}

func readFileRange(file *os.File, start, end int64) ([]byte, error) {
	buffer := make([]byte, end-start)
	read, err := file.ReadAt(buffer, start)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buffer[:read], nil
}

func transcriptPrefixIdentity(file *os.File, size int64) (string, bool, error) {
	anchorLen := size
	if anchorLen > claudeVersionPrefixAnchorBytes {
		anchorLen = claudeVersionPrefixAnchorBytes
	}
	anchor := make([]byte, anchorLen)
	if _, err := file.ReadAt(anchor, 0); err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	endedWithNewline := false
	if size > 0 {
		last := make([]byte, 1)
		if _, err := file.ReadAt(last, size-1); err != nil {
			return "", false, err
		}
		endedWithNewline = last[0] == '\n'
	}
	return digestOf(anchor), endedWithNewline, nil
}

func FindClaudeTranscriptPaths(configDir string, sessionIDs []string) (map[string][]string, error) {
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

func transcriptLineVersion(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || trimmed[0] != '{' {
		return "", false
	}
	var record struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal([]byte(trimmed), &record); err != nil || record.Version == "" {
		return "", false
	}
	return record.Version, true
}

func workerBuildIdentityFromGoBuild() workerBuildIdentity {
	workerBuildOnce.Do(func() {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			return
		}
		identity := workerBuildIdentity{}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				identity.revision = strings.TrimSpace(setting.Value)
			case "vcs.modified":
				value := strings.TrimSpace(setting.Value)
				if value == "true" || value == "false" {
					modified := value == "true"
					identity.modified = &modified
				}
			}
		}
		workerBuildValue = identity
	})
	return workerBuildValue
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortedEnvKeys(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
