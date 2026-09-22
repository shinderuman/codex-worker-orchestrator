package codexrollout

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Rollout struct {
	AbsolutePath   string
	HomeRelative   string
	ID             string
	ParentThreadID string
	GuardianSource bool
	FirstTimestamp time.Time
	LastTimestamp  time.Time
	Cwd            string
	Originator     string
	SourceRaw      string
}

type sessionMeta struct {
	Timestamp string             `json:"timestamp"`
	Type      string             `json:"type"`
	Payload   sessionMetaPayload `json:"payload"`
}

type sessionMetaPayload struct {
	ID             string          `json:"id"`
	ParentThreadID string          `json:"parent_thread_id"`
	Cwd            string          `json:"cwd"`
	Originator     string          `json:"originator"`
	Source         json.RawMessage `json:"source"`
}

type rolloutSource struct {
	Subagent *rolloutSubagent `json:"subagent"`
}

type rolloutSubagent struct {
	Other string `json:"other"`
}

func Scan(codexHome string) ([]Rollout, error) {
	rollouts := make([]Rollout, 0)
	for _, root := range []string{filepath.Join(codexHome, "sessions"), filepath.Join(codexHome, "archived_sessions")} {
		collected, err := scanRoot(codexHome, root)
		if err != nil {
			return nil, err
		}
		rollouts = append(rollouts, collected...)
	}
	return rollouts, nil
}

func scanRoot(codexHome, root string) ([]Rollout, error) {
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
	rollouts := make([]Rollout, 0)
	err = filepath.WalkDir(root, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() || filepath.Ext(entry.Name()) != ".jsonl" {
			return nil
		}
		if rollout, ok := readMeta(codexHome, filePath); ok {
			rollouts = append(rollouts, rollout)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rollouts, nil
}

func readMeta(codexHome, filePath string) (Rollout, bool) {
	file, err := os.Open(filePath)
	if err != nil {
		return Rollout{}, false
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if !scanner.Scan() {
		return Rollout{}, false
	}
	var meta sessionMeta
	if err := json.Unmarshal(scanner.Bytes(), &meta); err != nil || meta.Type != "session_meta" {
		return Rollout{}, false
	}
	first, err := time.Parse(time.RFC3339Nano, meta.Timestamp)
	if err != nil {
		first = time.Time{}
	}
	rel, relErr := filepath.Rel(codexHome, filePath)
	if relErr != nil {
		return Rollout{}, false
	}
	return Rollout{
		AbsolutePath:   filePath,
		HomeRelative:   filepath.ToSlash(rel),
		ID:             meta.Payload.ID,
		ParentThreadID: meta.Payload.ParentThreadID,
		GuardianSource: sourceIsGuardian(meta.Payload.Source),
		FirstTimestamp: first,
		Cwd:            meta.Payload.Cwd,
		Originator:     meta.Payload.Originator,
		SourceRaw:      compactRawJSON(meta.Payload.Source),
	}, true
}

func Matching(rollouts []Rollout, threadID string) []Rollout {
	matches := make([]Rollout, 0, 1)
	for _, rollout := range rollouts {
		if rollout.ID == threadID {
			matches = append(matches, rollout)
		}
	}
	return matches
}

func DirExists(dir string) bool {
	info, err := os.Stat(dir)
	return err == nil && info.IsDir()
}

func SourceLabel(chain []Rollout) string {
	sources := make([]string, 0, len(chain))
	for _, member := range chain {
		sources = append(sources, member.HomeRelative)
	}
	return strings.Join(sources, ";")
}

func LastTimestamp(filePath string) (time.Time, bool) {
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

func sourceIsGuardian(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var source rolloutSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return false
	}
	return source.Subagent != nil && source.Subagent.Other == "guardian"
}

func compactRawJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var buffer bytes.Buffer
	if err := json.Compact(&buffer, raw); err != nil {
		return string(raw)
	}
	return buffer.String()
}
