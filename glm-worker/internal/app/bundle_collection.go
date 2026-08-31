package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"path"
	"runtime/debug"
	"strings"
	"time"
)

type bundleEntryMeasure struct {
	hash             hash.Hash
	bytes            int64
	records          int
	invalidRecords   int
	droppedLines     int
	runtimeRecords   int
	lastEventAt      string
	observeJSON      bool
	observeRuntime   bool
	line             []byte
	lineDropped      bool
	trailingFragment bool
}

const bundleMeasureLineLimit = 4 * 1024 * 1024

const bundleTelemetryArchivePrefix = "task/telemetry/"

func newBundleEntryMeasure(archivePath string) *bundleEntryMeasure {
	return &bundleEntryMeasure{
		hash:           sha256.New(),
		observeJSON:    strings.EqualFold(path.Ext(archivePath), ".jsonl"),
		observeRuntime: strings.HasPrefix(archivePath, bundleTelemetryArchivePrefix),
	}
}

func (m *bundleEntryMeasure) WriteTo(writer io.Writer, data []byte) (int64, error) {
	if _, err := m.hash.Write(data); err != nil {
		return 0, err
	}
	m.bytes += int64(len(data))
	if m.observeJSON {
		m.observeLines(data)
	}
	written, err := writer.Write(data)
	return int64(written), err
}

func (m *bundleEntryMeasure) CopyFrom(writer io.Writer, sourcePath string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("bundle source %sを開けません: %w", sourcePath, err)
	}
	defer func() { _ = file.Close() }()
	if !m.observeJSON {
		written, err := io.Copy(io.MultiWriter(writer, m.hash), file)
		m.bytes += written
		return err
	}
	buffer := make([]byte, 64*1024)
	for {
		read, err := file.Read(buffer)
		if read > 0 {
			if _, writeErr := m.WriteTo(writer, buffer[:read]); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("bundle source %sを読めません: %w", sourcePath, err)
		}
	}
}

func (m *bundleEntryMeasure) observeLines(data []byte) {
	for _, b := range data {
		if b != '\n' {
			if len(m.line) < bundleMeasureLineLimit {
				m.line = append(m.line, b)
			} else {
				m.lineDropped = true
			}
			continue
		}
		m.observeLine(m.line, false)
		m.line = m.line[:0]
		m.lineDropped = false
	}
}

func (m *bundleEntryMeasure) observeLine(line []byte, atEOF bool) {
	if len(line) == 0 {
		return
	}
	if m.lineDropped {
		m.droppedLines++
		return
	}
	var record struct {
		Timestamp   string          `json:"timestamp"`
		CompletedAt string          `json:"completed_at"`
		CapturedAt  string          `json:"captured_at"`
		Runtime     json.RawMessage `json:"runtime"`
	}
	if err := json.Unmarshal(line, &record); err != nil {
		if atEOF {
			m.trailingFragment = true
			return
		}
		m.invalidRecords++
		return
	}
	m.records++
	if m.observeRuntime && runtimeFieldPresent(record.Runtime) {
		m.runtimeRecords++
	}
	for _, value := range []string{record.Timestamp, record.CompletedAt, record.CapturedAt} {
		if value == "" {
			continue
		}
		if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
			continue
		}
		m.lastEventAt = value
	}
}

func runtimeFieldPresent(raw json.RawMessage) bool {
	return len(raw) != 0 && string(raw) != "null"
}

func (m *bundleEntryMeasure) apply(collected *bundleCollectedEntry) {
	m.observeLine(m.line, true)
	collected.SHA256 = fmt.Sprintf("%x", m.hash.Sum(nil))
	collected.Bytes = m.bytes
	collected.Records = m.records
	collected.InvalidRecords = m.invalidRecords
	collected.DroppedLines = m.droppedLines
	collected.RuntimeRecords = m.runtimeRecords
	collected.TrailingFragment = m.trailingFragment
	collected.LastEventAt = m.lastEventAt
}

func bundleCollectorRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return strings.TrimSpace(setting.Value)
		}
	}
	return ""
}
