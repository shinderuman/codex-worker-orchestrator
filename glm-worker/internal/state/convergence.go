package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type RoundPathState struct {
	Path           string `json:"path"`
	Class          string `json:"class"`
	Deleted        bool   `json:"deleted,omitempty"`
	FullDigest     string `json:"full_digest,omitempty"`
	SemanticDigest string `json:"semantic_digest,omitempty"`
}

type RoundRecord struct {
	Version      int              `json:"version"`
	TaskID       string           `json:"task_id"`
	Seq          int              `json:"seq"`
	ReviewNumber int              `json:"review_number"`
	AutoFixes    int              `json:"auto_fixes"`
	WorkerPhase  string           `json:"worker_phase"`
	CapturedAt   time.Time        `json:"captured_at"`
	Snapshot     SnapshotDigest   `json:"snapshot"`
	Paths        []RoundPathState `json:"paths,omitempty"`
	CaptureError string           `json:"capture_error,omitempty"`
}

type RoundDelta struct {
	Class         string
	ChangedPaths  int
	SemanticPaths int
	DocPaths      int
}

const roundLogVersion = 1

const (
	extCC    = ".cc"
	extCS    = ".cs"
	extMM    = ".mm"
	extCPP   = ".cpp"
	extDart  = ".dart"
	extPHP   = ".php"
	extCXX   = ".cxx"
	extJava  = ".java"
	extKT    = ".kt"
	extKTS   = ".kts"
	extScala = ".scala"
	extSwift = ".swift"
	extRS    = ".rs"
	extHPP   = ".hpp"
	extHH    = ".hh"
	extHXX   = ".hxx"
	extH     = ".h"
)

const (
	RoundPathClassDoc   = "doc"
	RoundPathClassCode  = "code"
	RoundPathClassOther = "other"
)

const (
	RoundDeltaBaseline      = "baseline"
	RoundDeltaInitial       = "initial"
	RoundDeltaSameSnapshot  = "same-snapshot"
	RoundDeltaCommentFormat = "comment-format-only"
	RoundDeltaDocChange     = "doc-change"
	RoundDeltaSemantic      = "semantic-change"
	RoundDeltaUnknown       = "unknown"
)

const RoundWorkerPhaseBaseline = "baseline"

const (
	roundCommentSlash = "slash"
	roundCommentHash  = "hash"
	roundCommentNone  = "none"
)

func (s *StateStore) RoundLogPath(taskID string) string {
	return s.Path(filepath.Join("rounds", taskID+".jsonl"))
}

func (s *StateStore) AppendRoundRecord(record RoundRecord) error {
	if record.Version == 0 {
		record.Version = roundLogVersion
	}
	seq := 1
	records, err := s.ReadRoundRecords(record.TaskID)
	if err == nil && len(records) > 0 {
		seq = records[len(records)-1].Seq + 1
	}
	record.Seq = seq
	data, err := json.Marshal(record)
	if err != nil {
		warnRoundRecordFailure("JSON化", err)
		return err
	}
	path := s.RoundLogPath(record.TaskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		warnRoundRecordFailure("log dir作成", err)
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		warnRoundRecordFailure("log open", err)
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		warnRoundRecordFailure("log chmod", err)
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		_ = file.Close()
		warnRoundRecordFailure("追記", err)
		return err
	}
	return file.Close()
}

func ParseRoundLine(data []byte) (RoundRecord, error) {
	var record RoundRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RoundRecord{}, fmt.Errorf("round recordを読めません: %w", err)
	}
	if record.Version != roundLogVersion {
		return RoundRecord{}, fmt.Errorf("unsupported round record version: %d", record.Version)
	}
	return record, nil
}

func (s *StateStore) ReadRoundRecords(taskID string) ([]RoundRecord, error) {
	data, err := os.ReadFile(s.RoundLogPath(taskID))
	if err != nil {
		return nil, err
	}
	var records []RoundRecord
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		record, err := ParseRoundLine([]byte(line))
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func warnRoundRecordFailure(operation string, err error) {
	writeStatsWarningEvent("round_log", fmt.Sprintf("round記録の%sに失敗しました（観測用のためtask本体へ影響しません）", operation), err)
}

func ClassifyRoundPaths(repoRoot string, paths []string) []RoundPathState {
	sorted := append([]string(nil), paths...)
	sort.Strings(sorted)
	result := make([]RoundPathState, 0, len(sorted))
	for _, path := range sorted {
		result = append(result, ClassifyRoundPath(repoRoot, path))
	}
	return result
}

func ClassifyRoundPath(repoRoot string, relPath string) RoundPathState {
	entry := RoundPathState{Path: relPath, Class: RoundPathClass(relPath)}
	absPath, err := joinWithinRoot(repoRoot, relPath)
	if err != nil {
		return entry
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			entry.Deleted = true
		}
		return entry
	}
	switch {
	case info.Mode().IsRegular():
		content, err := os.ReadFile(absPath)
		if err != nil {
			return entry
		}
		entry.FullDigest = roundDigest(content)
		entry.SemanticDigest = RoundSemanticDigest(content, entry.Class, relPath)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(absPath)
		if err != nil {
			return entry
		}
		entry.FullDigest = roundDigest([]byte(target))
	}
	return entry
}

func RoundPathClass(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".rst", ".adoc", ".txt":
		return RoundPathClassDoc
	case ".go", ".c", extH, extCPP, extHPP, extCC, extHH, extCXX, extHXX,
		extJava, extKT, extKTS, extCS, extSwift, extRS, extDart, extScala,
		".m", extMM, extPHP,
		".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs",
		".py", ".toml", ".ini", ".cfg",
		".json", ".css", ".scss", ".less":
		return RoundPathClassCode
	}
	switch filepath.Base(path) {
	case "LICENSE", "NOTICE", "CHANGELOG", "CHANGELOG.md":
		return RoundPathClassDoc
	}
	return RoundPathClassOther
}

func roundCommentKind(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go", ".c", extH, extCPP, extHPP, extCC, extHH, extCXX, extHXX,
		extJava, extKT, extKTS, extCS, extSwift, extRS, extDart, extScala,
		".m", extMM, extPHP,
		".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs":
		return roundCommentSlash
	case ".py", ".toml", ".ini", ".cfg":
		return roundCommentHash
	}
	return roundCommentNone
}

func RoundSemanticDigest(content []byte, class string, path string) string {
	if len(content) == 0 || class == RoundPathClassDoc {
		return roundDigest(content)
	}
	if class != RoundPathClassCode {
		return ""
	}
	kind := roundCommentKind(path)
	if roundSemanticUnsafe(content, path, kind) {
		return ""
	}
	return roundDigest(normalizeRoundContent(content, kind))
}

func roundSemanticUnsafe(content []byte, path, kind string) bool {
	if bytes.Contains(content, []byte("\\\n")) {
		return true
	}
	switch kind {
	case roundCommentSlash:
		return roundSlashSemanticUnsafe(content, path)
	case roundCommentHash:
		return bytes.Contains(content, []byte(`"""`)) || bytes.Contains(content, []byte(`'''`))
	default:
		return false
	}
}

func roundSlashSemanticUnsafe(content []byte, path string) bool {
	if bytes.IndexByte(content, '`') >= 0 {
		return true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == extPHP && bytes.Contains(content, []byte("<<<")) {
		return true
	}
	for _, marker := range roundSlashStringGuardMarkers(ext) {
		if bytes.Contains(content, marker) {
			return true
		}
	}
	return false
}

func roundSlashStringGuardMarkers(ext string) [][]byte {
	switch ext {
	case extJava, extKT, extKTS, extScala:
		return [][]byte{[]byte(`"""`)}
	case extSwift, extDart:
		return [][]byte{[]byte(`"""`), []byte(`'''`)}
	case extCS:
		return [][]byte{[]byte(`"""`), []byte(`@"`)}
	case extRS:
		return [][]byte{[]byte(`r"`), []byte(`#"`)}
	case extCPP, extHPP, extCC, extHH, extCXX, extHXX, extH, extMM:
		return [][]byte{[]byte(`R"`)}
	}
	return nil
}

func normalizeRoundContent(content []byte, kind string) []byte {
	var kept [][]byte
	for _, line := range bytes.Split(content, []byte("\n")) {
		trimmed := bytes.TrimRight(line, " \t\r")
		if len(trimmed) == 0 {
			continue
		}
		if kind != roundCommentNone && roundCommentLine(trimmed, kind) {
			continue
		}
		kept = append(kept, trimmed)
	}
	return bytes.Join(kept, []byte("\n"))
}

func roundCommentLine(line []byte, kind string) bool {
	trimmed := bytes.TrimLeft(line, " \t")
	var marker []byte
	switch kind {
	case roundCommentSlash:
		marker = []byte("//")
	case roundCommentHash:
		marker = []byte("#")
	default:
		return false
	}
	if !bytes.HasPrefix(trimmed, marker) {
		return false
	}
	rest := trimmed[len(marker):]
	if kind == roundCommentHash {
		if bytes.HasPrefix(rest, []byte("!")) || bytes.HasPrefix(rest, []byte("-*-")) {
			return false
		}
	}
	if roundDirectiveComment(rest) {
		return false
	}
	return true
}

func roundDirectiveComment(rest []byte) bool {
	if len(rest) == 0 || !isDirectiveIdentChar(rest[0], true) {
		return false
	}
	for i := 1; i < len(rest); i++ {
		if rest[i] == ':' {
			return true
		}
		if !isDirectiveIdentChar(rest[i], false) {
			return false
		}
	}
	return false
}

func isDirectiveIdentChar(char byte, first bool) bool {
	switch {
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z':
		return true
	case char >= '0' && char <= '9':
		return !first
	}
	return false
}

func roundDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func CompareRoundRecords(prev, curr *RoundRecord) RoundDelta {
	if initial, done := initialRoundDelta(prev, curr); done {
		return initial
	}
	previous := roundPathIndex(prev.Paths)
	current := roundPathIndex(curr.Paths)
	delta := RoundDelta{Class: RoundDeltaCommentFormat}
	accumulateCurrentRoundPaths(&delta, previous, current)
	accumulateRemovedRoundPaths(&delta, previous, current)
	if delta.ChangedPaths == 0 {
		return RoundDelta{Class: RoundDeltaUnknown}
	}
	return delta
}

func initialRoundDelta(prev, curr *RoundRecord) (RoundDelta, bool) {
	switch {
	case curr == nil:
		return RoundDelta{Class: RoundDeltaUnknown}, true
	case curr.WorkerPhase == RoundWorkerPhaseBaseline:
		return RoundDelta{Class: RoundDeltaBaseline}, true
	case prev == nil:
		return RoundDelta{Class: RoundDeltaInitial}, true
	case roundSnapshotsEqual(prev.Snapshot, curr.Snapshot):
		return RoundDelta{Class: RoundDeltaSameSnapshot}, true
	case prev.CaptureError != "" || curr.CaptureError != "":
		return RoundDelta{Class: RoundDeltaUnknown}, true
	default:
		return RoundDelta{}, false
	}
}

func accumulateCurrentRoundPaths(delta *RoundDelta, previous, current map[string]RoundPathState) {
	for path, currEntry := range current {
		prevEntry, existed := previous[path]
		if existed && prevEntry.FullDigest == currEntry.FullDigest && currEntry.FullDigest != "" {
			continue
		}
		delta.ChangedPaths++
		if currEntry.Class == RoundPathClassDoc || (existed && prevEntry.Class == RoundPathClassDoc) {
			delta.addDocPath()
			continue
		}
		if roundPathDeltaNonSemantic(prevEntry, existed, currEntry) {
			continue
		}
		delta.SemanticPaths++
		delta.Class = RoundDeltaSemantic
	}
}

func accumulateRemovedRoundPaths(delta *RoundDelta, previous, current map[string]RoundPathState) {
	for path, prevEntry := range previous {
		if _, ok := current[path]; ok {
			continue
		}
		delta.ChangedPaths++
		if prevEntry.Class == RoundPathClassDoc {
			delta.addDocPath()
			continue
		}
		delta.SemanticPaths++
		delta.Class = RoundDeltaSemantic
	}
}

func (d *RoundDelta) addDocPath() {
	d.DocPaths++
	if d.Class != RoundDeltaSemantic {
		d.Class = RoundDeltaDocChange
	}
}

func roundPathDeltaNonSemantic(prev RoundPathState, existed bool, curr RoundPathState) bool {
	if !existed || curr.Deleted || prev.Deleted {
		return false
	}
	return prev.SemanticDigest != "" && curr.SemanticDigest != "" &&
		prev.SemanticDigest == curr.SemanticDigest
}

func roundPathIndex(paths []RoundPathState) map[string]RoundPathState {
	index := make(map[string]RoundPathState, len(paths))
	for _, entry := range paths {
		index[entry.Path] = entry
	}
	return index
}

func roundSnapshotsEqual(prev, curr SnapshotDigest) bool {
	return prev.IndexDigest != "" && prev.WorktreeDigest != "" &&
		curr.IndexDigest != "" && curr.WorktreeDigest != "" &&
		prev.Head == curr.Head &&
		prev.IndexDigest == curr.IndexDigest &&
		prev.WorktreeDigest == curr.WorktreeDigest
}
