package controller

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexrollout"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type runtimeTranscriptWindowRecord struct {
	SessionID     string `json:"session_id,omitempty"`
	RolloutID     string `json:"rollout_id,omitempty"`
	TotalBytes    int64  `json:"total_bytes"`
	StartOffset   int64  `json:"start_offset"`
	EndOffset     int64  `json:"end_offset"`
	RecordsBefore int    `json:"records_before,omitempty"`
	RecordsAfter  int    `json:"records_after,omitempty"`
	Basis         string `json:"basis"`
}

type runtimeSessionAssociation struct {
	AttemptID        string                          `json:"attempt_id"`
	RuntimeTaskID    string                          `json:"runtime_task_id"`
	Task             SemanticTaskRef                 `json:"semantic_task_ref"`
	SessionIDs       []string                        `json:"session_ids"`
	Parent           *state.ParentCodexIdentity      `json:"parent,omitempty"`
	WindowStart      time.Time                       `json:"window_start"`
	WindowEnd        time.Time                       `json:"window_end"`
	ModelTranscripts []runtimeTranscriptWindowRecord `json:"model_transcripts,omitempty"`
	ParentRollouts   []runtimeTranscriptWindowRecord `json:"parent_rollouts,omitempty"`
	GuardianRollouts []runtimeTranscriptWindowRecord `json:"guardian_rollouts,omitempty"`
}

func (c *attemptRuntimeEvidence) captureSessions() error {
	sessions, err := c.runtimeSessionIDs()
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if err := c.captureClaudeSessions(ids); err != nil {
		return err
	}
	parent, err := c.captureCodexSessions()
	if err != nil {
		return err
	}
	association := runtimeSessionAssociation{
		AttemptID: c.seal.AttemptID, RuntimeTaskID: c.taskID, Task: c.seal.SemanticTaskRef,
		SessionIDs: ids, Parent: parent,
		WindowStart: c.seal.StartedAt, WindowEnd: c.seal.SealedAt,
		ModelTranscripts: c.modelTranscripts, ParentRollouts: c.parentRollouts, GuardianRollouts: c.guardianRollouts,
	}
	data, err := json.Marshal(association)
	if err != nil {
		return err
	}
	ref, err := c.store.PutEvidenceObject("session-association", "application/json", c.seal.AttemptID+":runtime-sessions", true, data)
	if err != nil {
		return err
	}
	c.seal.SessionAssociationRefs = append(c.seal.SessionAssociationRefs, ref)
	return nil
}

func (c *attemptRuntimeEvidence) captureClaudeSessions(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	matches, err := runner.FindClaudeTranscriptPaths(c.store.config.ClaudeConfigDir, ids)
	if err != nil {
		return fmt.Errorf("capture associated model transcripts: %w", err)
	}
	for _, id := range ids {
		paths := matches[id]
		sort.Strings(paths)
		if len(paths) == 0 {
			return fmt.Errorf("required model transcript is missing for session %s", id)
		}
		for i, path := range paths {
			logical := fmt.Sprintf("session/%s/%d", id, i)
			if err := c.addWindowedFile(path, evidenceKindModelTranscript, c.seal.AttemptID+":"+logical, id, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *attemptRuntimeEvidence) captureCodexSessions() (*state.ParentCodexIdentity, error) {
	parent, err := c.runtime.CurrentParentCodexIdentity()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rollouts, err := codexrollout.Scan(c.store.config.CodexConfigDir)
	if err != nil {
		return nil, err
	}
	chain, err := runtimeParentRolloutChain(rollouts, parent.ThreadID)
	if err != nil {
		return nil, err
	}
	for i, rollout := range chain {
		logical := fmt.Sprintf("parent/%s/%d", parent.ThreadID, i)
		if err := c.addRawTranscriptFile(rollout.AbsolutePath, evidenceKindParentTranscript, c.seal.AttemptID+":"+logical, rollout.ID); err != nil {
			return nil, err
		}
	}
	if err := c.captureGuardianSessions(parent.ThreadID, rollouts); err != nil {
		return nil, err
	}
	return &parent, nil
}

func (c *attemptRuntimeEvidence) readEventSessionIDs() ([]string, error) {
	file, err := os.Open(c.runtime.TaskEventLogPath(c.taskID))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	var ids []string
	for scanner.Scan() {
		record, err := state.ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			return nil, err
		}
		if record.SessionID != "" {
			ids = append(ids, record.SessionID)
		}
	}
	return ids, scanner.Err()
}

func (c *attemptRuntimeEvidence) runtimeSessionIDs() (map[string]struct{}, error) {
	sessions, err := c.modelSessionIDs()
	if err != nil {
		return nil, err
	}
	events, err := c.readEventSessionIDs()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, id := range events {
		sessions[id] = struct{}{}
	}
	for _, name := range []string{"worker.id", "reviewer.id", "failure-path-reviewer.id"} {
		value, err := c.runtime.Read(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if value != "" {
			sessions[value] = struct{}{}
		}
	}
	return sessions, nil
}

func (c *attemptRuntimeEvidence) modelSessionIDs() (map[string]struct{}, error) {
	sessions := map[string]struct{}{}
	logs, err := c.runtime.ReadModelCallLogs(c.taskID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		stats, statsErr := c.runtime.CurrentTaskStats()
		if statsErr != nil && !errors.Is(statsErr, os.ErrNotExist) {
			return nil, statsErr
		}
		if statsErr == nil && stats.ModelCalls > 0 {
			return nil, fmt.Errorf("runtime telemetry is missing for recorded model calls")
		}
	}
	for _, log := range logs {
		if log.TaskID != "" && log.TaskID != c.taskID {
			return nil, fmt.Errorf("runtime telemetry task identity is inconsistent")
		}
		if runtimeTranscriptSession(log) {
			sessions[log.SessionID] = struct{}{}
		}
	}
	return sessions, nil
}

func (c *attemptRuntimeEvidence) captureGuardianSessions(parentThreadID string, rollouts []codexrollout.Rollout) error {
	for _, rollout := range rollouts {
		if rollout.ParentThreadID != parentThreadID || !rollout.GuardianSource {
			continue
		}
		if err := c.addRawTranscriptFile(rollout.AbsolutePath, evidenceKindGuardianTranscript, c.seal.AttemptID+":guardian/"+rollout.ID, rollout.ID); err != nil {
			return err
		}
	}
	return nil
}

func runtimeParentRolloutChain(rollouts []codexrollout.Rollout, parentThreadID string) ([]codexrollout.Rollout, error) {
	matches := codexrollout.Matching(rollouts, parentThreadID)
	if len(matches) == 0 {
		return nil, fmt.Errorf("required parent rollout is missing")
	}
	if len(matches) == 1 {
		return matches, nil
	}
	chain, reason := codexrollout.ResolveChain(matches)
	if reason != "" {
		return nil, fmt.Errorf("required parent rollout association is ambiguous: %s", reason)
	}
	return chain, nil
}

func runtimeTranscriptSession(log state.ModelCallLog) bool {
	return log.CallType != state.CallTypeProbe && log.CallType != state.CallTypeEvent && log.SessionID != "unknown" && log.SessionID != ""
}
