package app

import "encoding/json"

func (value bundleAnalysisInterval) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status   string  `json:"status"`
		Start    *string `json:"start"`
		End      *string `json:"end"`
		EndBasis string  `json:"end_basis,omitempty"`
	}{
		Status: value.Status, Start: value.Start, End: value.End, EndBasis: value.EndBasis,
	})
}

func (value bundleAnalysisSubsequents) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status      string                         `json:"status"`
		Attribution string                         `json:"attribution"`
		Turns       []bundleAnalysisSubsequentTurn `json:"turns,omitempty"`
	}{
		Status: value.Status, Attribution: value.Attribution, Turns: value.Turns,
	})
}

func (value bundleAnalysisRollout) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status            string `json:"status"`
		TotalBytes        int64  `json:"total_bytes,omitempty"`
		WindowStartOffset int64  `json:"window_start_offset,omitempty"`
		WindowEndOffset   int64  `json:"window_end_offset,omitempty"`
		WindowBytes       int64  `json:"window_bytes,omitempty"`
		BaselineOffset    int64  `json:"baseline_offset,omitempty"`
	}{
		Status: value.Status, TotalBytes: value.TotalBytes,
		WindowStartOffset: value.WindowStartOffset, WindowEndOffset: value.WindowEndOffset,
		WindowBytes: value.WindowBytes, BaselineOffset: value.BaselineOffset,
	})
}

func (value bundleAnalysisCount) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status string `json:"status"`
		Count  int    `json:"count,omitempty"`
	}{Status: value.Status, Count: value.Count})
}

func (value bundleAnalysisTokenDelta) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status            string `json:"status"`
		InputTokens       int64  `json:"input_tokens,omitempty"`
		CachedInputTokens int64  `json:"cached_input_tokens,omitempty"`
		BaselineAt        string `json:"baseline_at,omitempty"`
		EndAt             string `json:"end_at,omitempty"`
	}{
		Status: value.Status, InputTokens: value.InputTokens, CachedInputTokens: value.CachedInputTokens,
		BaselineAt: value.BaselineAt, EndAt: value.EndAt,
	})
}

func (value bundleAnalysisValidations) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Status string              `json:"status"`
		Runs   []bundleAnalysisRun `json:"runs,omitempty"`
	}{Status: value.Status, Runs: value.Runs})
}

func (value bundleAnalysisRetries) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ValidationReruns  []bundleAnalysisRerun `json:"validation_reruns,omitempty"`
		WorkerCounters    map[string]int        `json:"worker_counters,omitempty"`
		ResumedModelCalls bundleAnalysisCount   `json:"resumed_model_calls"`
	}{
		ValidationReruns: value.ValidationReruns,
		WorkerCounters: value.WorkerCounters,
		ResumedModelCalls: value.ResumedModelCalls,
	})
}
