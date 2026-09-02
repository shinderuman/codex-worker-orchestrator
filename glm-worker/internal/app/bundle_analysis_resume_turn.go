package app

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type analysisResumeTurnEvidence struct {
	statusTaskIDs      []string
	continuationTaskID string
	conflicted         bool
}

type analysisTaskOwnership struct {
	status  string
	initial *analysisRolloutTurn
	final   *analysisRolloutTurn
	owned   map[string]struct{}
}

type analysisRolloutCompletedEvent struct {
	Type   string                      `json:"type"`
	TurnID string                      `json:"turn_id"`
	Item   *analysisRolloutCommandItem `json:"item"`
}

type analysisRolloutCommandItem struct {
	Type     string   `json:"type"`
	Command  []string `json:"command"`
	ExitCode *int     `json:"exit_code"`
	Stdout   string   `json:"stdout"`
}

type analysisResumeStatus struct {
	TaskID          string           `json:"task_id"`
	TaskStatus      state.TaskStatus `json:"task_status"`
	ResumeAvailable bool             `json:"resume_available"`
	RateLimited     *struct {
		Limited bool `json:"limited"`
	} `json:"rate_limited"`
}

const codexRolloutItemCompletedType = "item_completed"

const codexRolloutCommandExecutionType = "CommandExecution"

const analysisWorkerStatusCommand = "glm-worker --status"

const analysisParentResumeCommand = "glm-parent-action resume"

func resolveAnalysisTaskOwnership(association codexAssociation, turns []analysisRolloutTurn, taskStart, collectionEnd time.Time, taskID string) analysisTaskOwnership {
	initial := resolveAnalysisOwningTurn(turns, taskStart)
	if initial.status != analysisStatusAvailable || initial.turn == nil {
		return analysisTaskOwnership{status: analysisStatusUnknown}
	}
	ownership := analysisTaskOwnership{
		status:  analysisStatusAvailable,
		initial: initial.turn,
		final:   initial.turn,
		owned:   map[string]struct{}{initial.turn.TurnID: {}},
	}
	evidence := analysisResumeTurnEvidenceFromRollout(association, taskStart, collectionEnd)
	for i := range turns {
		turn := &turns[i]
		if !analysisContinuationCandidate(turn, ownership.initial) {
			continue
		}
		if analysisApplyContinuationTurn(&ownership, turn, evidence[turn.TurnID], taskID) {
			return analysisTaskOwnership{status: analysisStatusUnknown}
		}
	}
	return ownership
}

func analysisContinuationCandidate(turn, initial *analysisRolloutTurn) bool {
	return turn.TurnID != initial.TurnID && turn.StartedAt.After(initial.StartedAt)
}

func analysisApplyContinuationTurn(ownership *analysisTaskOwnership, turn *analysisRolloutTurn, evidence *analysisResumeTurnEvidence, taskID string) bool {
	if evidence == nil {
		return false
	}
	if evidence.conflicted && analysisResumeEvidenceTouchesTask(evidence, taskID) {
		return true
	}
	if evidence.continuationTaskID != taskID {
		return false
	}
	if !ownership.final.HasComplete || turn.StartedAt.Before(ownership.final.CompletedAt) {
		return true
	}
	ownership.owned[turn.TurnID] = struct{}{}
	ownership.final = turn
	return false
}

func analysisResumeEvidenceTouchesTask(evidence *analysisResumeTurnEvidence, taskID string) bool {
	return evidence.continuationTaskID == taskID || analysisTaskIDPresent(evidence.statusTaskIDs, taskID)
}

func analysisResumeTurnEvidenceFromRollout(association codexAssociation, start, end time.Time) map[string]*analysisResumeTurnEvidence {
	if association.ParentStatus != codexStatusIncluded || association.ParentPath == "" {
		return nil
	}
	file, err := os.Open(association.ParentPath)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	evidence := map[string]*analysisResumeTurnEvidence{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		analysisObserveResumeEvidenceLine(evidence, scanner.Bytes(), start, end)
	}
	if scanner.Err() != nil {
		return nil
	}
	return evidence
}

func analysisObserveResumeEvidenceLine(evidence map[string]*analysisResumeTurnEvidence, line []byte, start, end time.Time) {
	turnID, command, stdout, ok := analysisResumeCommandFromLine(line, start, end)
	if !ok {
		return
	}
	turnEvidence := evidence[turnID]
	if turnEvidence == nil {
		turnEvidence = &analysisResumeTurnEvidence{}
		evidence[turnID] = turnEvidence
	}
	analysisRecordResumeCommand(turnEvidence, command, stdout)
}

func analysisResumeCommandFromLine(line []byte, start, end time.Time) (string, string, string, bool) {
	var record codexRolloutScanLine
	if err := json.Unmarshal(line, &record); err != nil || record.Type != "event_msg" {
		return "", "", "", false
	}
	if !analysisTimestampWithin(record.Timestamp, start, end) {
		return "", "", "", false
	}
	return analysisResumeCommandFromPayload(record.Payload)
}

func analysisTimestampWithin(raw string, start, end time.Time) bool {
	timestamp, err := time.Parse(time.RFC3339Nano, raw)
	return err == nil && !timestamp.Before(start) && !timestamp.After(end)
}

func analysisResumeCommandFromPayload(payload json.RawMessage) (string, string, string, bool) {
	var event analysisRolloutCompletedEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.Type != codexRolloutItemCompletedType || event.TurnID == "" || event.Item == nil {
		return "", "", "", false
	}
	command, ok := analysisSuccessfulExactCommand(event.Item)
	if !ok {
		return "", "", "", false
	}
	return event.TurnID, command, event.Item.Stdout, true
}

func analysisSuccessfulExactCommand(item *analysisRolloutCommandItem) (string, bool) {
	if item.Type != codexRolloutCommandExecutionType || item.ExitCode == nil || *item.ExitCode != 0 {
		return "", false
	}
	return analysisExactCompletedCommand(item.Command)
}

func analysisRecordResumeCommand(evidence *analysisResumeTurnEvidence, command, stdout string) {
	switch command {
	case analysisWorkerStatusCommand:
		if taskID, valid := analysisResumeStatusTaskID(stdout); valid {
			evidence.statusTaskIDs = analysisAppendUniqueTaskID(evidence.statusTaskIDs, taskID)
		}
	case analysisParentResumeCommand:
		analysisConfirmResumeEvidence(evidence)
	}
}

func analysisExactCompletedCommand(command []string) (string, bool) {
	if len(command) != 3 || command[1] != "-lc" {
		return "", false
	}
	script := strings.TrimSpace(command[2])
	switch script {
	case analysisWorkerStatusCommand, analysisParentResumeCommand:
		return script, true
	default:
		return "", false
	}
}

func analysisResumeStatusTaskID(stdout string) (string, bool) {
	var status analysisResumeStatus
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &status); err != nil {
		return "", false
	}
	if status.TaskID == "" || status.TaskStatus != state.TaskStatusRateLimited || !status.ResumeAvailable ||
		status.RateLimited == nil || !status.RateLimited.Limited {
		return "", false
	}
	return status.TaskID, true
}

func analysisAppendUniqueTaskID(taskIDs []string, taskID string) []string {
	for _, existing := range taskIDs {
		if existing == taskID {
			return taskIDs
		}
	}
	return append(taskIDs, taskID)
}

func analysisConfirmResumeEvidence(evidence *analysisResumeTurnEvidence) {
	if len(evidence.statusTaskIDs) == 0 {
		return
	}
	if len(evidence.statusTaskIDs) != 1 {
		evidence.conflicted = true
		return
	}
	taskID := evidence.statusTaskIDs[0]
	if evidence.continuationTaskID == "" {
		evidence.continuationTaskID = taskID
		return
	}
	if evidence.continuationTaskID != taskID {
		evidence.conflicted = true
	}
}

func analysisTaskIDPresent(taskIDs []string, taskID string) bool {
	for _, existing := range taskIDs {
		if existing == taskID {
			return true
		}
	}
	return false
}

func analysisTaskFinalizationInterval(execution analysisExecutionBoundary, ownership analysisTaskOwnership) bundleAnalysisInterval {
	if analysisSingleOwnedTurn(ownership) {
		return analysisFinalizationInterval(execution, analysisOwningTurn{status: ownership.status, turn: ownership.initial})
	}
	interval := bundleAnalysisInterval{Status: analysisStatusUnknown}
	if ownership.status != analysisStatusAvailable || ownership.final == nil {
		return interval
	}
	if execution.status == analysisStatusUnknown {
		return interval
	}
	if execution.status == analysisStatusOpen {
		interval.Status = analysisStatusOpen
		return interval
	}
	if !ownership.final.HasComplete {
		interval.Status = analysisStatusOpen
		return interval
	}
	if execution.end.Before(ownership.final.StartedAt) || ownership.final.CompletedAt.Before(execution.end) {
		return interval
	}
	interval.Status = analysisStatusAvailable
	interval.Start = analysisTimestamp(execution.end)
	interval.End = analysisTimestamp(ownership.final.CompletedAt)
	return interval
}

func analysisTaskSubsequentRequests(association codexAssociation, scan bundleRolloutScan, ownership analysisTaskOwnership, collectionEnd time.Time) bundleAnalysisSubsequents {
	if analysisSingleOwnedTurn(ownership) {
		return analysisSubsequentRequests(association, scan, analysisOwningTurn{status: ownership.status, turn: ownership.initial}, collectionEnd)
	}
	subsequent := bundleAnalysisSubsequents{
		Status:      analysisStatusUnknown,
		Attribution: analysisAttributionSubsequent,
	}
	if association.ParentStatus != codexStatusIncluded || ownership.status != analysisStatusAvailable || ownership.initial == nil || ownership.final == nil {
		return subsequent
	}
	if collectionEnd.IsZero() {
		return subsequent
	}
	if !ownership.initial.HasComplete || !ownership.final.HasComplete {
		subsequent.Status = analysisStatusOpen
		return subsequent
	}
	subsequent.Status = analysisStatusAvailable
	for i := range scan.turns {
		turn := &scan.turns[i]
		if analysisTaskOwnsTurn(ownership, turn) {
			continue
		}
		if !turn.StartedAt.After(ownership.initial.CompletedAt) || turn.StartedAt.After(collectionEnd) {
			continue
		}
		subsequent.Turns = append(subsequent.Turns, analysisSubsequentTurn(scan, turn, collectionEnd))
	}
	return subsequent
}

func analysisTaskOwnsTurn(ownership analysisTaskOwnership, turn *analysisRolloutTurn) bool {
	_, owned := ownership.owned[turn.TurnID]
	return owned
}

func analysisTaskFinalizationTokenDelta(association codexAssociation, scan bundleRolloutScan, execution analysisExecutionBoundary, ownership analysisTaskOwnership, interval bundleAnalysisInterval) bundleAnalysisTokenDelta {
	if analysisSingleOwnedTurn(ownership) {
		return analysisFinalizationTokenDelta(association, scan, execution, analysisOwningTurn{status: ownership.status, turn: ownership.initial}, interval)
	}
	delta := bundleAnalysisTokenDelta{Status: interval.Status}
	if association.ParentStatus != codexStatusIncluded || interval.Status != analysisStatusAvailable || ownership.final == nil {
		return delta
	}
	return analysisAnchoredTokenDelta(scan, execution.end, ownership.final.CompletedAt)
}

func analysisSingleOwnedTurn(ownership analysisTaskOwnership) bool {
	return ownership.status == analysisStatusAvailable && ownership.initial != nil && ownership.final == ownership.initial
}
