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
	path   string
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
	if len(transcript) == 0 || len(transcript) > reportOnlyTranscriptMaxBytes {
		return out
	}
	scanner := bufio.NewScanner(bytes.NewReader(transcript))
	scanner.Buffer(make([]byte, 64*1024), reportOnlyTranscriptMaxBytes)
	reads := int64(0)
	readIDs := make(map[string]struct{})
	pathReadCounts := make(map[string]int64)
	ranges := make(map[string]map[reportOnlyReadRange]struct{})
	rangesComplete := true
	idsComplete := true
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var record reportOnlyTranscriptRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return out
		}
		if record.Type != "assistant" {
			continue
		}
		for _, block := range record.Message.Content {
			if block.Type != "tool_use" || block.Name != "Read" {
				continue
			}
			reads++
			if block.ID == "" {
				idsComplete = false
			} else {
				if _, duplicated := readIDs[block.ID]; duplicated {
					idsComplete = false
				}
				readIDs[block.ID] = struct{}{}
			}
			if block.Input.FilePath == "" || block.Input.Offset == nil || block.Input.Limit == nil ||
				*block.Input.Offset < 0 || *block.Input.Limit < 1 {
				rangesComplete = false
				continue
			}
			path := block.Input.FilePath
			pathReadCounts[path]++
			if ranges[path] == nil {
				ranges[path] = make(map[reportOnlyReadRange]struct{})
			}
			ranges[path][reportOnlyReadRange{path: path, offset: *block.Input.Offset, limit: *block.Input.Limit}] = struct{}{}
		}
	}
	if scanner.Err() != nil {
		return out
	}
	out.ReadCalls = reportOnlyKnownMetric(reads)
	if rangesComplete {
		split := int64(0)
		for path, variants := range ranges {
			if len(variants) >= 2 {
				split += pathReadCounts[path]
			}
		}
		out.SamePathSplitReads = reportOnlyKnownMetric(split)
	}
	if reads == 0 {
		out.ToolDurationMS = reportOnlyKnownMetric(0)
		return out
	}
	if !idsComplete {
		return out
	}
	durations := make(map[string]int64)
	for _, event := range events {
		if event.CallID != call.CallID {
			continue
		}
		for _, block := range event.Blocks {
			if block.Type != "tool_result" || block.ToolID == "" {
				continue
			}
			if _, expected := readIDs[block.ToolID]; !expected || block.DurationMS <= 0 {
				continue
			}
			if _, duplicate := durations[block.ToolID]; duplicate {
				return out
			}
			durations[block.ToolID] = block.DurationMS
		}
	}
	if len(durations) == len(readIDs) {
		total := int64(0)
		for _, duration := range durations {
			total += duration
		}
		out.ToolDurationMS = reportOnlyKnownMetric(total)
	}
	return out
}
