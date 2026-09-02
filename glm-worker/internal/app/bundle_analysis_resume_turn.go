package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const codexRolloutItemCompletedType = "item_completed"

const codexRolloutCommandExecutionType = "CommandExecution"

const analysisWorkerStatusCommand = "glm-worker --status"

const analysisParentResumeCommand = "glm-parent-action resume"

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

func observeAnalysisRolloutContinuation(scan *bundleRolloutScan, payload json.RawMessage) {
	var event analysisRolloutCompletedEvent
	if err := json.Unmarshal(payload, &event); err != nil || event.Type != codexRolloutItemCompletedType || event.TurnID == "" || event.Item == nil {
		return
	}
	item := event.Item
	if item.Type != codexRolloutCommandExecutionType || item.ExitCode == nil || *item.ExitCode != 0 {
		return
	}
	command, ok := analysisExactCompletedCommand(item.Command)
	if !ok {
		return
	}
	if scan.resumeTurns == nil {
		scan.resumeTurns = map[string]*analysisResumeTurnEvidence{}
	}
	evidence := scan.resumeTurns[event.TurnID]
	if evidence == nil {
		evidence = &analysisResumeTurnEvidence{}
		scan.resumeTurns[event.TurnID] = evidence
	}
	switch command {
	case analysisWorkerStatusCommand:
		if taskID, valid := analysisResumeStatusTaskID(item.Stdout); valid {
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

func resolveAnalysisTaskOwnership(scan bundleRolloutScan, taskStart time.Time, taskID string) analysisTaskOwnership {
	var containing []*analysisRolloutTurn
	for i := range scan.turns {
		turn := &scan.turns[i]
		if turn.StartedAt.After(taskStart) {
			continue
		}
		if turn.HasComplete && turn.CompletedAt.Before(taskStart) {
			continue
		}
		containing = append(containing, turn)
	}
	if len(containing) != 1 {
		return analysisTaskOwnership{status: analysisStatusUnknown}
	}

	initial := containing[0]
	ownership := analysisTaskOwnership{
		status:  analysisStatusAvailable,
		initial: initial,
		final:   initial,
		owned:   map[string]struct{}{initial.TurnID: {}},
	}
	for i := range scan.turns {
		turn := &scan.turns[i]
		if turn.TurnID == initial.TurnID || !turn.StartedAt.After(initial.StartedAt) {
			continue
		}
		evidence := scan.resumeTurns[turn.TurnID]
		if evidence == nil {
			continue
		}
		if evidence.conflicted && (evidence.continuationTaskID == taskID || analysisTaskIDPresent(evidence.statusTaskIDs, taskID)) {
			return analysisTaskOwnership{status: analysisStatusUnknown}
		}
		if evidence.continuationTaskID != taskID {
			continue
		}
		if !ownership.final.HasComplete || turn.StartedAt.Before(ownership.final.CompletedAt) {
			return analysisTaskOwnership{status: analysisStatusUnknown}
		}
		ownership.owned[turn.TurnID] = struct{}{}
		ownership.final = turn
	}
	return ownership
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
		if _, taskOwned := ownership.owned[turn.TurnID]; taskOwned {
			continue
		}
		if !turn.StartedAt.After(ownership.initial.CompletedAt) || turn.StartedAt.After(collectionEnd) {
			continue
		}
		subsequent.Turns = append(subsequent.Turns, analysisSubsequentTurn(scan, turn, collectionEnd))
	}
	return subsequent
}

func analysisTaskFinalizationTokenDelta(association codexAssociation, scan bundleRolloutScan, execution analysisExecutionBoundary, ownership analysisTaskOwnership, interval bundleAnalysisInterval) bundleAnalysisTokenDelta {
	delta := bundleAnalysisTokenDelta{Status: interval.Status}
	if association.ParentStatus != codexStatusIncluded || interval.Status != analysisStatusAvailable || ownership.final == nil {
		return delta
	}
	return analysisAnchoredTokenDelta(scan, execution.end, ownership.final.CompletedAt)
}
