package state

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
)

type TelemetryTaskError struct {
	TaskID string `json:"task_id"`
	Error  string `json:"error"`
}

type TelemetryCurrentScan struct {
	Dir                    string
	Files                  int
	FilesConsidered        int
	RecordsOutsidePeriod   int
	RecordsUndatedExcluded int
	IgnoredFiles           []string
	UnreadableTasks        []TelemetryTaskError
	Logs                   []TaskCallLogs
}

type telemetryCorpusScan struct {
	dir                    string
	filesConsidered        int
	recordsOutsidePeriod   int
	recordsUndatedExcluded int
	ignoredFiles           []string
	unreadableFiles        []TelemetryFileError
	files                  []telemetryCorpusFile
}

type telemetryCorpusFile struct {
	name             string
	taskID           string
	currentReadError string
	records          []telemetryCorpusRecord
}

type telemetryCorpusRecord struct {
	current         bool
	log             ModelCallLog
	usagePresent    bool
	malformedReason string
	currentReadError string
}

type telemetryCorpusHeader struct {
	Version        *int            `json:"version"`
	SchemaRevision int             `json:"schema_revision"`
	TreeUsage      json.RawMessage `json:"tree_usage"`
}

func (s *StateStore) scanTelemetryCorpus(filter TelemetryQueryFilter) (*telemetryCorpusScan, error) {
	dir := s.Path("telemetry")
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("telemetry dirを読めません: %w", err)
	}

	scan := &telemetryCorpusScan{dir: dir}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		taskID := strings.TrimSuffix(name, ".jsonl")
		if !filter.MatchesTask(taskID) {
			continue
		}
		scan.filesConsidered++
		if !ValidGeneratedUUID(taskID) {
			scan.ignoredFiles = append(scan.ignoredFiles, name)
			continue
		}

		fileScan := telemetryCorpusFile{name: name, taskID: taskID}
		if err := s.scanTelemetryCorpusFile(&fileScan, filter, scan); err != nil {
			fileScan.currentReadError = err.Error()
			scan.unreadableFiles = append(scan.unreadableFiles, TelemetryFileError{File: name, Error: err.Error()})
		}
		scan.files = append(scan.files, fileScan)
	}
	return scan, nil
}

func (s *StateStore) scanTelemetryCorpusFile(fileScan *telemetryCorpusFile, filter TelemetryQueryFilter, scan *telemetryCorpusScan) error {
	file, err := os.Open(s.ModelCallLogPath(fileScan.taskID))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, include := decodeTelemetryCorpusLine(scanner.Bytes(), filter, scan)
		if !include {
			continue
		}
		fileScan.records = append(fileScan.records, record)
		if record.currentReadError != "" && fileScan.currentReadError == "" {
			fileScan.currentReadError = record.currentReadError
		}
	}
	return scanner.Err()
}

func decodeTelemetryCorpusLine(line []byte, filter TelemetryQueryFilter, scan *telemetryCorpusScan) (telemetryCorpusRecord, bool) {
	if len(line) == 0 {
		return telemetryCorpusRecord{currentReadError: "telemetryを読めません: unexpected end of JSON input"}, true
	}

	var header telemetryCorpusHeader
	if err := json.Unmarshal(line, &header); err != nil {
		return telemetryCorpusRecord{
			malformedReason: telemetryMalformedReasonDecode,
			currentReadError: fmt.Sprintf("telemetryを読めません: %v", err),
		}, true
	}
	if header.Version == nil {
		return telemetryCorpusRecord{malformedReason: telemetryMalformedReasonHeader}, true
	}
	if *header.Version != ModelCallLogVersion || header.SchemaRevision != ModelCallLogSchemaRevision {
		return telemetryCorpusRecord{malformedReason: telemetryMalformedReasonUnsupportedSchema}, true
	}

	var record ModelCallLog
	if err := json.Unmarshal(line, &record); err != nil {
		return telemetryCorpusRecord{
			malformedReason: telemetryMalformedReasonDecode,
			currentReadError: fmt.Sprintf("telemetryを読めません: %v", err),
		}, true
	}
	if filter.ExcludesUndated(record.StartedAt) {
		scan.recordsUndatedExcluded++
		return telemetryCorpusRecord{}, false
	}
	if !filter.CoversTime(record.StartedAt) {
		scan.recordsOutsidePeriod++
		return telemetryCorpusRecord{}, false
	}

	return telemetryCorpusRecord{
		current:      true,
		log:          record,
		usagePresent: len(header.TreeUsage) > 0 && string(header.TreeUsage) != "null",
	}, true
}

func (s *StateStore) ScanTelemetryCurrent(filter TelemetryQueryFilter) (*TelemetryCurrentScan, error) {
	corpus, err := s.scanTelemetryCorpus(filter)
	if err != nil {
		return nil, err
	}

	scan := &TelemetryCurrentScan{
		Dir:                    corpus.dir,
		FilesConsidered:        corpus.filesConsidered,
		RecordsOutsidePeriod:   corpus.recordsOutsidePeriod,
		RecordsUndatedExcluded: corpus.recordsUndatedExcluded,
		IgnoredFiles:           corpus.ignoredFiles,
		Logs:                   make([]TaskCallLogs, 0, len(corpus.files)),
	}
	for _, file := range corpus.files {
		if file.currentReadError != "" {
			scan.UnreadableTasks = append(scan.UnreadableTasks, TelemetryTaskError{TaskID: file.taskID, Error: file.currentReadError})
			continue
		}
		logs := make([]ModelCallLog, 0, len(file.records))
		for _, record := range file.records {
			if record.current {
				logs = append(logs, record.log)
			}
		}
		scan.Files++
		scan.Logs = append(scan.Logs, TaskCallLogs{TaskID: file.taskID, Logs: logs})
	}
	return scan, nil
}
