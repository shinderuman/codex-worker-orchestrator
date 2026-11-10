package bundleanalysis

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ReportOnlyReadMetric struct {
	Status string `json:"status"`
	Value  *int64 `json:"value,omitempty"`
}

type ReportOnlyReadEvidence struct {
	CallID             string               `json:"call_id"`
	Identity           string               `json:"identity"`
	Phase              string               `json:"phase"`
	ReadCalls          ReportOnlyReadMetric `json:"read_calls"`
	SamePathSplitReads ReportOnlyReadMetric `json:"same_path_split_reads"`
	ToolDurationMS     ReportOnlyReadMetric `json:"tool_duration_ms"`
	CLIApiDurationMS   ReportOnlyReadMetric `json:"cli_api_duration_ms"`
	Outcome            string               `json:"outcome"`
}

type reportOnlyReadRange struct {
	offset int
	limit  int
}

type reportOnlyTranscriptBlock struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	ID    string `json:"id"`
	Input struct {
		FilePath string `json:"file_path"`
		Offset   *int   `json:"offset"`
		Limit    *int   `json:"limit"`
	} `json:"input"`
}

type reportOnlyTranscriptRecord struct {
	Type    string `json:"type"`
	Message struct {
		Content []reportOnlyTranscriptBlock `json:"content"`
	} `json:"message"`
}

type reportOnlyReadScan struct {
	readCount      int64
	pathReads      map[string]int64
	pathRanges     map[string]map[reportOnlyReadRange]struct{}
	readIDs        map[string]struct{}
	rangesComplete bool
	idsComplete    bool
}

const reportOnlyTranscriptMaxBytes = 4 * 1024 * 1024

func reportOnlyUnknownMetric() ReportOnlyReadMetric {
	return ReportOnlyReadMetric{Status: "unknown"}
}

func reportOnlyKnownMetric(value int64) ReportOnlyReadMetric {
	return ReportOnlyReadMetric{Status: "known", Value: &value}
}

func AnalyzeReportOnlyReadReentry(transcriptCallID string, transcript []byte, call state.ModelCallLog, events []state.TaskEventRecord) ReportOnlyReadEvidence {
	out := ReportOnlyReadEvidence{
		CallID:             call.CallID,
		Identity:           "unknown",
		Phase:              "unknown",
		ReadCalls:          reportOnlyUnknownMetric(),
		SamePathSplitReads: reportOnlyUnknownMetric(),
		ToolDurationMS:     reportOnlyUnknownMetric(),
		CLIApiDurationMS:   reportOnlyUnknownMetric(),
		Outcome:            "unknown",
	}
	if transcriptCallID == "" || call.CallID == "" || transcriptCallID != call.CallID ||
		call.CallType != state.CallTypeTask || !strings.Contains(call.Phase, "report-only") {
		return out
	}
	out.Identity = "matched-call-id"
	out.Phase = call.Phase
	if call.ClaudeAPIDurationMS > 0 {
		out.CLIApiDurationMS = reportOnlyKnownMetric(call.ClaudeAPIDurationMS)
	}
	switch call.Outcome {
	case "success", "rate_limited", "invalid_packet", "error", "interrupted", "provider_unavailable":
		out.Outcome = call.Outcome
	}
	scan, valid := scanReportOnlyReadTranscript(transcript)
	if !valid {
		return out
	}
	out.ReadCalls = reportOnlyKnownMetric(scan.readCount)
	if scan.rangesComplete {
		out.SamePathSplitReads = reportOnlyKnownMetric(scan.splitReads())
	}
	out.ToolDurationMS = reportOnlyReadToolDuration(scan, call.CallID, events)
	return out
}

func scanReportOnlyReadTranscript(transcript []byte) (reportOnlyReadScan, bool) {
	scan := reportOnlyReadScan{
		pathReads:      make(map[string]int64),
		pathRanges:     make(map[string]map[reportOnlyReadRange]struct{}),
		readIDs:        make(map[string]struct{}),
		rangesComplete: true,
		idsComplete:    true,
	}
	if len(transcript) == 0 || len(transcript) > reportOnlyTranscriptMaxBytes {
		return scan, false
	}
	scanner := bufio.NewScanner(bytes.NewReader(transcript))
	scanner.Buffer(make([]byte, 64*1024), reportOnlyTranscriptMaxBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record reportOnlyTranscriptRecord
		if json.Unmarshal(line, &record) != nil {
			return scan, false
		}
		if record.Type != "assistant" {
			continue
		}
		for _, block := range record.Message.Content {
			scan.observe(block)
		}
	}
	return scan, scanner.Err() == nil
}

func (scan *reportOnlyReadScan) observe(block reportOnlyTranscriptBlock) {
	if block.Type != "tool_use" || block.Name != "Read" {
		return
	}
	scan.readCount++
	if block.ID == "" {
		scan.idsComplete = false
	} else {
		if _, exists := scan.readIDs[block.ID]; exists {
			scan.idsComplete = false
		}
		scan.readIDs[block.ID] = struct{}{}
	}
	if block.Input.FilePath == "" || block.Input.Offset == nil || block.Input.Limit == nil ||
		*block.Input.Offset < 0 || *block.Input.Limit < 1 {
		scan.rangesComplete = false
		return
	}
	path := block.Input.FilePath
	scan.pathReads[path]++
	if scan.pathRanges[path] == nil {
		scan.pathRanges[path] = make(map[reportOnlyReadRange]struct{})
	}
	scan.pathRanges[path][reportOnlyReadRange{offset: *block.Input.Offset, limit: *block.Input.Limit}] = struct{}{}
}

func (scan reportOnlyReadScan) splitReads() int64 {
	var split int64
	for path, ranges := range scan.pathRanges {
		if len(ranges) >= 2 {
			split += scan.pathReads[path]
		}
	}
	return split
}

func reportOnlyReadToolDuration(scan reportOnlyReadScan, callID string, events []state.TaskEventRecord) ReportOnlyReadMetric {
	if scan.readCount == 0 {
		return reportOnlyKnownMetric(0)
	}
	if !scan.idsComplete {
		return reportOnlyUnknownMetric()
	}
	durations := make(map[string]int64)
	for _, event := range events {
		if event.CallID != callID {
			continue
		}
		for _, block := range event.Blocks {
			if block.Type != "tool_result" || block.ToolID == "" || block.DurationMS <= 0 {
				continue
			}
			if _, relevant := scan.readIDs[block.ToolID]; !relevant {
				continue
			}
			if _, duplicate := durations[block.ToolID]; duplicate {
				return reportOnlyUnknownMetric()
			}
			durations[block.ToolID] = block.DurationMS
		}
	}
	if len(durations) != len(scan.readIDs) {
		return reportOnlyUnknownMetric()
	}
	var total int64
	for _, duration := range durations {
		total += duration
	}
	return reportOnlyKnownMetric(total)
}
