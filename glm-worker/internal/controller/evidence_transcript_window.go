package controller

import (
	"bytes"
	"encoding/json"
	"time"
)

type evidenceTranscriptWindow struct {
	TotalBytes    int64  `json:"total_bytes"`
	StartOffset   int64  `json:"start_offset"`
	EndOffset     int64  `json:"end_offset"`
	RecordsBefore int    `json:"records_before,omitempty"`
	RecordsAfter  int    `json:"records_after,omitempty"`
	Basis         string `json:"basis"`
	Unattributed  string `json:"unattributed,omitempty"`
}

type transcriptWindowScan struct {
	startOffset   int64
	endOffset     int64
	recordsBefore int
	recordsAfter  int
	timestamped   bool
}

const (
	evidenceWindowBasisTimestamp      = "record-timestamp-window"
	evidenceWindowBasisUnbounded      = "no-record-timestamps"
	evidenceWindowBasisCaptureOffsets = "capture-offset-window"
)

func scanTranscriptWindow(data []byte, start, end time.Time, capture *runtimeTranscriptWindowRecord) (evidenceTranscriptWindow, []byte) {
	window := evidenceTranscriptWindow{TotalBytes: int64(len(data)), EndOffset: int64(len(data))}
	if len(data) == 0 {
		window.Basis = evidenceWindowBasisTimestamp
		return window, data
	}
	scan := scanTranscriptRegions(data, start, end)
	if scan.timestamped {
		window.StartOffset = scan.startOffset
		window.EndOffset = scan.endOffset
		window.RecordsBefore = scan.recordsBefore
		window.RecordsAfter = scan.recordsAfter
		window.Basis = evidenceWindowBasisTimestamp
		return window, data[scan.startOffset:scan.endOffset]
	}
	return captureOffsetWindow(data, capture)
}

func captureOffsetWindow(data []byte, capture *runtimeTranscriptWindowRecord) (evidenceTranscriptWindow, []byte) {
	window := evidenceTranscriptWindow{TotalBytes: int64(len(data))}
	if capture == nil {
		window.Basis = evidenceWindowBasisUnbounded
		window.Unattributed = "transcript has no record timestamps and no canonical capture offsets determine an attempt-dedicated range"
		return window, nil
	}
	window.Basis = evidenceWindowBasisCaptureOffsets
	if capture.StartOffset < 0 || capture.EndOffset < capture.StartOffset || capture.EndOffset > int64(len(data)) {
		window.Unattributed = "canonical capture offsets no longer fit the transcript"
		return window, nil
	}
	window.StartOffset = capture.StartOffset
	window.EndOffset = capture.EndOffset
	window.RecordsBefore = countTranscriptRecords(data[:capture.StartOffset])
	window.RecordsAfter = countTranscriptRecords(data[capture.EndOffset:])
	return window, data[capture.StartOffset:capture.EndOffset]
}

func scanTranscriptRegions(data []byte, start, end time.Time) transcriptWindowScan {
	scan := transcriptWindowScan{startOffset: -1, endOffset: -1}
	region := 0
	offset := int64(0)
	for offset < int64(len(data)) {
		lineEnd := bytes.IndexByte(data[offset:], '\n')
		next := int64(len(data))
		if lineEnd >= 0 {
			next = offset + int64(lineEnd) + 1
		}
		if timestamp, ok := transcriptLineTimestamp(data[offset:next]); ok {
			scan.timestamped = true
			region = transcriptRegion(timestamp, start, end)
			if region == 0 {
				if scan.startOffset < 0 {
					scan.startOffset = offset
				}
				scan.endOffset = next
			}
		}
		scan.countRegion(region)
		offset = next
	}
	scan.normalize(int64(len(data)))
	return scan
}

func transcriptRegion(timestamp, start, end time.Time) int {
	switch {
	case timestamp.Before(start):
		return -1
	case !end.IsZero() && timestamp.After(end):
		return 1
	default:
		return 0
	}
}

func (scan *transcriptWindowScan) countRegion(region int) {
	switch region {
	case -1:
		scan.recordsBefore++
	case 1:
		scan.recordsAfter++
	}
}

func (scan *transcriptWindowScan) normalize(total int64) {
	if scan.startOffset < 0 {
		scan.startOffset = total
		scan.endOffset = total
	}
	if scan.endOffset < scan.startOffset {
		scan.endOffset = scan.startOffset
	}
}

func countTranscriptRecords(data []byte) int {
	records := 0
	offset := 0
	for offset < len(data) {
		lineEnd := bytes.IndexByte(data[offset:], '\n')
		next := len(data)
		if lineEnd >= 0 {
			next = offset + lineEnd + 1
		}
		if json.Valid(bytes.TrimSpace(data[offset:next])) {
			records++
		}
		offset = next
	}
	return records
}

func transcriptLineTimestamp(line []byte) (time.Time, bool) {
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return time.Time{}, false
	}
	var record struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(trimmed, &record); err != nil || record.Timestamp == "" {
		return time.Time{}, false
	}
	timestamp, err := time.Parse(time.RFC3339Nano, record.Timestamp)
	if err != nil {
		return time.Time{}, false
	}
	return timestamp, true
}
