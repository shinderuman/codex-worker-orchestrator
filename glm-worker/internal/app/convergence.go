package app

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// convergenceDeltaVerificationOnlyはsame-snapshot roundのうち、当該roundを生成した
// worker呼出がevent logでtool利用を観測され・file変更toolを一度も使っていないもの。
// treeはsnapshotで同一と確定しているため再review対象状態は同一のまま扱う。
const convergenceDeltaVerificationOnly = "verification-only"

// convergenceMutatingToolsはworktree内容を変更しうるtool名。これらが観測された
// same-snapshot roundはverification-onlyへ細分化しない(編集後に戻した可能性)。
var convergenceMutatingTools = map[string]bool{
	"Edit":         true,
	"Write":        true,
	"NotebookEdit": true,
}

var convergenceReviewerPhase = regexp.MustCompile(`^reviewer-(\d+)(-risk-floor)?$`)

// convergenceRoundは集計1行分のround観測。recordはround log、reviewer/workerは時間窓で
// 対応付けたtelemetry呼出。gapはreviewer番号不一致かseq不連続でrecord欠落が疑われるround。
type convergenceRound struct {
	record   state.RoundRecord
	delta    state.RoundDelta
	gap      bool
	mismatch bool
	reviewer []state.ModelCallLog
	worker   []state.ModelCallLog
}

type convergenceOutput struct {
	TaskID        string                `json:"task_id"`
	TaskStatus    string                `json:"task_status"`
	RoundsLog     convergenceLog        `json:"rounds_log"`
	SkippedRounds int                   `json:"skipped_rounds,omitempty"`
	Telemetry     string                `json:"telemetry"`
	EventLog      string                `json:"event_log"`
	Baseline      *convergenceBaseline  `json:"baseline"`
	Rounds        []convergenceRoundOut `json:"rounds"`
	Summary       convergenceSummaryOut `json:"summary"`
}

// convergenceLogはround logの所在と読み取り状態。statusはok・none(まだ無い)・
// unreadable(読み取り失敗)で、okのときだけpathが載る。
type convergenceLog struct {
	Status string  `json:"status"`
	Path   *string `json:"path,omitempty"`
}

type convergenceBaseline struct {
	CapturedAt    *time.Time           `json:"captured_at"`
	Paths         int                  `json:"paths"`
	Snapshot      state.SnapshotDigest `json:"snapshot"`
	SnapshotKnown bool                 `json:"snapshot_known"`
	CaptureError  string               `json:"capture_error,omitempty"`
}

type convergenceRoundOut struct {
	Number       int                  `json:"number"`
	Seq          int                  `json:"seq"`
	ReviewNumber int                  `json:"review_number"`
	AutoFixes    int                  `json:"autofixes"`
	WorkerPhase  string               `json:"worker_phase"`
	Delta        convergenceDeltaOut  `json:"delta"`
	Snapshot     state.SnapshotDigest `json:"snapshot"`
	Review       convergenceReviewOut `json:"review"`
	ReviewerCost *convergenceCost     `json:"reviewer_cost"`
	WorkerCost   *convergenceCost     `json:"worker_cost"`
}

type convergenceDeltaOut struct {
	Class              string `json:"class"`
	ChangedPaths       int    `json:"changed_paths"`
	NonSemanticPaths   int    `json:"nonsemantic_paths"`
	DocPaths           int    `json:"doc_paths"`
	Gap                bool   `json:"gap,omitempty"`
	MismatchedReviewer bool   `json:"mismatched_reviewer,omitempty"`
	CaptureError       string `json:"capture_error,omitempty"`
}

type convergenceReviewOut struct {
	Calls           int     `json:"calls"`
	Outcome         *string `json:"outcome"`
	Risk            *string `json:"risk"`
	ReportedRisk    *string `json:"reported_risk"`
	RiskFloorReemit bool    `json:"risk_floor_reemit"`
	Unresolved      bool    `json:"unresolved"`
	Snapshot        string  `json:"snapshot"`
}

type convergenceCost struct {
	Calls        int     `json:"calls"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	Turns        int     `json:"turns"`
	DurationMS   int64   `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
}

type convergenceSummaryOut struct {
	ByClass               []convergenceClassSummary `json:"by_class"`
	UnresolvedIssueRounds int                       `json:"unresolved_issue_rounds"`
	HighRounds            int                       `json:"high_rounds"`
}

type convergenceClassSummary struct {
	Class                string `json:"class"`
	Rounds               int    `json:"rounds"`
	ReviewerCalls        int    `json:"reviewer_calls"`
	ReviewerInputTokens  int64  `json:"reviewer_input_tokens"`
	ReviewerOutputTokens int64  `json:"reviewer_output_tokens"`
	ReviewerDurationMS   int64  `json:"reviewer_duration_ms"`
}

// printConvergenceはround log・telemetry・event logだけからreview/fix convergenceを
// machine JSONで出す。state書換・repo lock・AI call・provider/workerへの問い合わせを
// 行わない。telemetryの呼出結果・token・durationとround logのsnapshot・path分類をrecordの
// CapturedAt時間窓とWorkerPhaseで対応付け、対応付け不能な値はnullとして推測しない。
// taskIDArgが空なら現在task、指定されればそのtaskの保存済みlogを読む。task ID検証は
// --timelineと同じUUID v4境界を使う。明示指定taskのround log不在はnot_found error、
// 読込失敗はinternal errorとし、現在taskの不在は正常終了する。
func printConvergence(st *state.StateStore, taskIDArg string, stdout io.Writer) error {
	explicit := taskIDArg != ""
	taskID := taskIDArg
	if taskID == "" {
		taskID = st.ReadOr("task.id", "")
	}
	if !validTimelineTaskID(taskID, explicit) {
		return &UsageError{Message: fmt.Sprintf("task IDが生成されるUUID v4形式と一致しません: %q", taskID)}
	}

	records, skipped, recordsErr := readRoundRecords(st, taskID)
	var logStatus convergenceLog
	switch {
	case errors.Is(recordsErr, os.ErrNotExist):
		if explicit {
			return &NotFoundError{Message: fmt.Sprintf("task %sのround logがありません: %v", taskID, recordsErr)}
		}
		logStatus = convergenceLog{Status: "none"}
	case recordsErr != nil:
		if explicit {
			return fmt.Errorf("task %sのround logを読めません: %w", taskID, recordsErr)
		}
		logStatus = convergenceLog{Status: "unreadable"}
	default:
		logStatus = convergenceLog{Status: "ok", Path: stringPtr(st.RoundLogPath(taskID))}
	}

	output := convergenceOutput{
		TaskID:        taskID,
		TaskStatus:    timelineTaskStatus(st, taskID, explicit),
		RoundsLog:     logStatus,
		SkippedRounds: skipped,
	}
	if logStatus.Status != "ok" {
		return writeJSON(stdout, output)
	}

	logs, logErr := readStatusTelemetry(st, taskID)
	if logErr != nil {
		output.Telemetry = "unreadable"
	} else {
		output.Telemetry = "ok"
	}
	events, _, eventsErr := readTaskEventRecords(st, taskID)
	output.EventLog = taskRecordsStatus(taskID, eventsErr)

	rounds, baseline := buildConvergenceRounds(records, logs)
	refineConvergenceDeltas(rounds, events)
	output.Baseline = convergenceBaselineOut(baseline)
	output.Rounds = convergenceRoundOuts(rounds)
	output.Summary = buildConvergenceSummary(rounds)
	return writeJSON(stdout, output)
}

// taskRecordsStatusはevent logの読み取り結果をok・none・unreadableへ分類する。
func taskRecordsStatus(taskID string, err error) string {
	if taskID == "" {
		return "none"
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "none"
	case err != nil:
		return "unreadable"
	default:
		return "ok"
	}
}

// refineConvergenceDeltasはsame-snapshot roundをevent logのtool観測で
// verification-onlyへ細分化する。delta.Class自体を書き替える。
func refineConvergenceDeltas(rounds []convergenceRound, events []state.TaskEventRecord) {
	for i := range rounds {
		if rounds[i].delta.Class != state.RoundDeltaSameSnapshot {
			continue
		}
		uses, mutating := convergenceWorkerToolUse(events, rounds[i].record.WorkerPhase)
		if uses > 0 && !mutating {
			rounds[i].delta.Class = convergenceDeltaVerificationOnly
		}
	}
}

// readRoundRecordsはround logを行ごとに読む。破損行・旧version行はskipしてその件数を
// 返し、log全体の読込失敗(不在含む)だけをerrorとする。
func readRoundRecords(st *state.StateStore, taskID string) ([]state.RoundRecord, int, error) {
	file, err := os.Open(st.RoundLogPath(taskID))
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var records []state.RoundRecord
	skipped := 0
	for scanner.Scan() {
		record, err := state.ParseRoundLine(scanner.Bytes())
		if err != nil {
			skipped++
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return records, skipped, err
	}
	return records, skipped, nil
}

// buildConvergenceRoundsはround record列を集計単位へ組み立てる。baseline recordは
// 先頭に高々1つだけ分離し、以降をreview roundとして前recordとの差分を分類する。
// telemetry呼出は各recordのCapturedAtを境界とする時間窓へ配る。round iのreviewer呼出は
// [CapturedAt_i, CapturedAt_{i+1})へ、round iを生成したworker呼出は直前の境界からの
// 窓でWorkerPhaseと一致するものを対応付ける。reviewer phase番号がrecordのReviewNumber
// と一致しない呼出が同じ窓に現れたとき、record欠落が疑われるため次roundの分類をunknown
// へ倒す。
func buildConvergenceRounds(records []state.RoundRecord, logs []state.ModelCallLog) ([]convergenceRound, *state.RoundRecord) {
	if len(records) == 0 {
		return nil, nil
	}
	buckets := bucketTaskCallsByRound(records, logs)

	var baseline *state.RoundRecord
	rest := records
	if records[0].WorkerPhase == state.RoundWorkerPhaseBaseline {
		b := records[0]
		baseline = &b
		rest = records[1:]
	}
	rounds := make([]convergenceRound, 0, len(rest))
	for index, record := range rest {
		round := convergenceRound{record: record}
		recordIndex := index + len(records) - len(rest)
		round.reviewer = reviewerCallsInBucket(buckets[recordIndex], record.ReviewNumber, &round.mismatch)
		round.worker = producingWorkerCalls(buckets[recordIndex-1], record.WorkerPhase)
		round.gap = roundHasRecordGap(records, recordIndex)
		round.delta = state.CompareRoundRecords(prevRoundRecord(records, recordIndex), &rest[index])
		rounds = append(rounds, round)
	}
	for i := 1; i < len(rounds); i++ {
		if rounds[i-1].mismatch {
			rounds[i].gap = true
		}
	}
	for i := range rounds {
		if rounds[i].gap {
			rounds[i].delta = state.RoundDelta{Class: state.RoundDeltaUnknown}
		}
	}
	return rounds, baseline
}

// bucketTaskCallsByRoundはtask呼出をrecord境界の時間窓へ割り当てる。窓kは
// [records[k].CapturedAt, records[k+1].CapturedAt)で、最後の窓は以降全て。最初の境界
// より前の呼出は窓-1へ置き、先頭recordがround 1のときその生成worker呼出として扱える
// ようにする(先頭がbaselineのとき通常は存在しない)。
func bucketTaskCallsByRound(records []state.RoundRecord, logs []state.ModelCallLog) map[int][]state.ModelCallLog {
	buckets := make(map[int][]state.ModelCallLog, len(records)+1)
	boundaryIndex := func(startedAt time.Time) int {
		index := -1
		for i := len(records) - 1; i >= 0; i-- {
			if !records[i].CapturedAt.After(startedAt) {
				index = i
				break
			}
		}
		return index
	}
	for _, entry := range logs {
		if entry.CallType != state.CallTypeTask {
			continue
		}
		index := boundaryIndex(entry.StartedAt)
		buckets[index] = append(buckets[index], entry)
	}
	return buckets
}

func reviewerCallsInBucket(entries []state.ModelCallLog, reviewNumber int, mismatch *bool) []state.ModelCallLog {
	result := make([]state.ModelCallLog, 0, len(entries))
	for _, entry := range entries {
		if entry.Role != state.ReviewerRole {
			continue
		}
		match := convergenceReviewerPhase.FindStringSubmatch(entry.Phase)
		if match == nil || match[1] != fmt.Sprint(reviewNumber) {
			*mismatch = true
			continue
		}
		result = append(result, entry)
	}
	return result
}

// producingWorkerCallsはround生成呼出のtelemetry記録を取り出す。WorkerPhaseと一致
// (結果修正再依頼suffix付きを含む)するworker呼出のうち、IMPLEMENTED以外の結果を返した
// 呼出(Sol decisionへ向かった試行)は対象から外す。
// 結果status空欄の呼出はtransient再試行・invalid resultなど当該呼出の消費なので残す。
func producingWorkerCalls(entries []state.ModelCallLog, workerPhase string) []state.ModelCallLog {
	result := make([]state.ModelCallLog, 0, len(entries))
	for _, entry := range entries {
		if entry.Role != state.WorkerRole {
			continue
		}
		if entry.Phase != workerPhase && entry.Phase != workerPhase+"-result-correct" {
			continue
		}
		if entry.PacketStatus != "" && entry.PacketStatus != "IMPLEMENTED" {
			continue
		}
		result = append(result, entry)
	}
	return result
}

// roundHasRecordGapは直前recordとのseq不連続を検出する。先頭round(またはbaseline直後)
// は比較対象が無いためfalse。
func roundHasRecordGap(records []state.RoundRecord, recordIndex int) bool {
	if recordIndex <= 0 {
		return false
	}
	return records[recordIndex].Seq != records[recordIndex-1].Seq+1
}

func prevRoundRecord(records []state.RoundRecord, recordIndex int) *state.RoundRecord {
	if recordIndex <= 0 {
		return nil
	}
	prev := records[recordIndex-1]
	return &prev
}

func convergenceBaselineOut(baseline *state.RoundRecord) *convergenceBaseline {
	if baseline == nil {
		return nil
	}
	out := convergenceBaseline{
		Paths:         len(baseline.Paths),
		Snapshot:      baseline.Snapshot,
		SnapshotKnown: baseline.Snapshot.IndexDigest != "" && baseline.Snapshot.WorktreeDigest != "",
		CaptureError:  baseline.CaptureError,
	}
	if !baseline.CapturedAt.IsZero() {
		capturedAt := baseline.CapturedAt
		out.CapturedAt = &capturedAt
	}
	return &out
}

func convergenceRoundOuts(rounds []convergenceRound) []convergenceRoundOut {
	result := make([]convergenceRoundOut, 0, len(rounds))
	for index, round := range rounds {
		result = append(result, convergenceRoundOutDetail(index+1, round))
	}
	return result
}

func convergenceRoundOutDetail(number int, round convergenceRound) convergenceRoundOut {
	record := round.record
	out := convergenceRoundOut{
		Number:       number,
		Seq:          record.Seq,
		ReviewNumber: record.ReviewNumber,
		AutoFixes:    record.AutoFixes,
		WorkerPhase:  record.WorkerPhase,
		Delta: convergenceDeltaOut{
			Class:              round.delta.Class,
			ChangedPaths:       round.delta.ChangedPaths,
			NonSemanticPaths:   round.delta.ChangedPaths - round.delta.SemanticPaths - round.delta.DocPaths,
			DocPaths:           round.delta.DocPaths,
			Gap:                round.gap,
			MismatchedReviewer: round.mismatch,
			CaptureError:       record.CaptureError,
		},
		Snapshot:     record.Snapshot,
		Review:       convergenceReviewOutDetail(round),
		ReviewerCost: convergenceCostOut(round.reviewer),
		WorkerCost:   convergenceCostOut(round.worker),
	}
	return out
}

// convergenceWorkerToolUseは当該phaseのworker eventからtool_use観測数とfile変更tool
// の有無を数える。event recordが無いときは0観測として細分化しない。
func convergenceWorkerToolUse(events []state.TaskEventRecord, workerPhase string) (int, bool) {
	uses := 0
	mutating := false
	for _, event := range events {
		if event.Role != string(state.WorkerRole) || event.Phase != workerPhase {
			continue
		}
		for _, block := range event.Blocks {
			if block.Type != "tool_use" {
				continue
			}
			uses++
			if convergenceMutatingTools[block.Name] {
				mutating = true
			}
		}
	}
	return uses, mutating
}

func convergenceReviewOutDetail(round convergenceRound) convergenceReviewOut {
	out := convergenceReviewOut{
		Calls:    len(round.reviewer),
		Snapshot: "unknown",
	}
	for _, entry := range round.reviewer {
		if strings.HasSuffix(entry.Phase, "-risk-floor") {
			out.RiskFloorReemit = true
		}
		if entry.PacketStatus != "" {
			out.Outcome = stringPtr(entry.PacketStatus)
		}
		if entry.EffectiveRisk != "" {
			out.Risk = stringPtr(entry.EffectiveRisk)
		}
		if entry.ReviewerReportedRisk != "" {
			out.ReportedRisk = stringPtr(entry.ReviewerReportedRisk)
		}
		if entry.Snapshot != nil && entry.Snapshot.Matched != nil && *entry.Snapshot.Matched {
			out.Snapshot = "matched"
		}
	}
	if out.Outcome != nil && *out.Outcome == "FIX_REQUIRED" {
		out.Unresolved = true
	}
	return out
}

func convergenceCostOut(entries []state.ModelCallLog) *convergenceCost {
	if len(entries) == 0 {
		return nil
	}
	cost := convergenceCost{Calls: len(entries)}
	for _, entry := range entries {
		cost.InputTokens += entry.TreeUsage.InputTokens +
			entry.TreeUsage.CacheCreationInputTokens +
			entry.TreeUsage.CacheReadInputTokens
		cost.OutputTokens += entry.TreeUsage.OutputTokens
		cost.Turns += entry.TopLevelTurns
		cost.DurationMS += entry.WallDurationMS
		cost.TotalCostUSD += entry.TotalCostUSD
	}
	return &cost
}

func buildConvergenceSummary(rounds []convergenceRound) convergenceSummaryOut {
	byClass := make(map[string]*convergenceClassSummary)
	unresolved := 0
	high := 0
	for _, round := range rounds {
		class := round.delta.Class
		if _, ok := byClass[class]; !ok {
			byClass[class] = &convergenceClassSummary{Class: class}
		}
		summary := byClass[class]
		summary.Rounds++
		summary.ReviewerCalls += len(round.reviewer)
		for _, entry := range round.reviewer {
			summary.ReviewerInputTokens += entry.TreeUsage.InputTokens +
				entry.TreeUsage.CacheCreationInputTokens +
				entry.TreeUsage.CacheReadInputTokens
			summary.ReviewerOutputTokens += entry.TreeUsage.OutputTokens
			summary.ReviewerDurationMS += entry.WallDurationMS
		}
		if roundOutcomeUnresolved(round) {
			unresolved++
		}
		if roundRiskHigh(round) {
			high++
		}
	}
	summary := convergenceSummaryOut{
		ByClass:               make([]convergenceClassSummary, 0, len(byClass)),
		UnresolvedIssueRounds: unresolved,
		HighRounds:            high,
	}
	for _, class := range orderedSummaryClasses(byClass) {
		summary.ByClass = append(summary.ByClass, *byClass[class])
	}
	return summary
}

func orderedSummaryClasses(byClass map[string]*convergenceClassSummary) []string {
	order := []string{
		state.RoundDeltaSameSnapshot,
		convergenceDeltaVerificationOnly,
		state.RoundDeltaCommentFormat,
		state.RoundDeltaDocChange,
		state.RoundDeltaSemantic,
		state.RoundDeltaUnknown,
		state.RoundDeltaInitial,
	}
	classes := make([]string, 0, len(byClass))
	for _, class := range order {
		if _, ok := byClass[class]; ok {
			classes = append(classes, class)
		}
	}
	extra := make([]string, 0)
	for class := range byClass {
		if !containsString(classes, class) {
			extra = append(extra, class)
		}
	}
	sort.Strings(extra)
	return append(classes, extra...)
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func roundOutcomeUnresolved(round convergenceRound) bool {
	for _, entry := range round.reviewer {
		if entry.PacketStatus == "FIX_REQUIRED" {
			return true
		}
	}
	return false
}

func roundRiskHigh(round convergenceRound) bool {
	for _, entry := range round.reviewer {
		if entry.EffectiveRisk == "HIGH" || entry.ReviewerReportedRisk == "HIGH" {
			return true
		}
	}
	return false
}
