package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	executionMilestoneStateFile       = "execution-milestones.json"
	executionMilestoneVersion         = 1
	executionMilestoneHeading         = "## Execution milestones"
	executionMilestoneTaskAuthority   = "active-task:Contract,Must-not,Acceptance-criteria"
	maxExecutionMilestones            = 8
	maxExecutionMilestoneText         = 2048
	maxCompletedMilestonePromptSummary = 512
)

var executionMilestoneIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type executionMilestoneDefinition struct {
	ID          string `json:"id"`
	Scope       string `json:"scope"`
	Acceptance  string `json:"acceptance"`
	FreshWorker bool   `json:"fresh_worker,omitempty"`
}

type executionMilestoneDocument struct {
	Milestones []executionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneRecord struct {
	ID                       string             `json:"id"`
	Scope                    string             `json:"scope"`
	Acceptance               string             `json:"acceptance"`
	FreshWorker              bool               `json:"fresh_worker,omitempty"`
	Status                   string             `json:"status"`
	CompletedAt              *time.Time          `json:"completed_at,omitempty"`
	CompletedCallID          string             `json:"completed_call_id,omitempty"`
	CompletedWorkerSessionID string             `json:"completed_worker_session_id,omitempty"`
	Summary                  string             `json:"summary,omitempty"`
	RequirementCoverage      string             `json:"requirement_coverage,omitempty"`
	Tests                    string             `json:"tests,omitempty"`
	Unverified               string             `json:"unverified,omitempty"`
	ParentValidationEvidence string             `json:"parent_validation_evidence,omitempty"`
	Artifacts                []string           `json:"artifacts,omitempty"`
	Snapshot                 *state.GitSnapshot `json:"snapshot,omitempty"`
}

type executionMilestonePlan struct {
	Version              int                        `json:"version"`
	TaskID               string                     `json:"task_id"`
	ActiveTaskPath       string                     `json:"active_task_path"`
	TaskContractAuthority string                    `json:"task_contract_authority"`
	DefinitionSHA256     string                     `json:"definition_sha256"`
	CurrentIndex         int                        `json:"current_index"`
	Milestones           []executionMilestoneRecord `json:"milestones"`
	UpdatedAt            time.Time                  `json:"updated_at"`
}

func (w *Workflow) initializeExecutionMilestones(activeTaskPath string) error {
	if err := w.state.Remove(executionMilestoneStateFile); err != nil {
		return err
	}
	if activeTaskPath == "" {
		return nil
	}
	definitions, present, err := readExecutionMilestoneDefinitions(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return fmt.Errorf("execution milestones: %w", err)
	}
	if !present {
		return nil
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	digest, err := executionMilestoneDefinitionDigest(definitions)
	if err != nil {
		return err
	}
	return w.saveExecutionMilestonePlan(newExecutionMilestonePlan(taskID, activeTaskPath, definitions, digest, w.now()))
}

func (w *Workflow) decorateExecutionMilestoneCheckpoint(checkpoint state.ResumeCheckpoint) (state.ResumeCheckpoint, error) {
	if checkpoint.Role != state.WorkerRole || checkpoint.ReportOnly {
		return checkpoint, nil
	}
	plan, enabled, err := w.syncExecutionMilestonePlan()
	if err != nil || !enabled || plan.CurrentIndex >= len(plan.Milestones) {
		return checkpoint, err
	}
	checkpoint.Prompt = renderExecutionMilestonePrompt(checkpoint.Prompt, plan, plan.CurrentIndex)
	checkpoint.OriginalPrompt = checkpoint.Prompt
	return checkpoint, nil
}

func (w *Workflow) advanceExecutionMilestone(request string, result packet.Result) (bool, error) {
	if result.Status != packet.StatusImplemented {
		return false, nil
	}
	plan, enabled, err := w.syncExecutionMilestonePlan()
	if err != nil || !enabled || plan.CurrentIndex >= len(plan.Milestones) {
		return false, err
	}
	if err := w.completeExecutionMilestone(plan, result); err != nil {
		return true, err
	}
	if plan.CurrentIndex >= len(plan.Milestones) {
		return false, nil
	}

	next := plan.Milestones[plan.CurrentIndex]
	if next.FreshWorker {
		if err := w.state.Remove("worker.id", "worker.ready"); err != nil {
			return true, fmt.Errorf("fresh worker for milestone %q: %w", next.ID, err)
		}
	}
	checkpoint := w.nextExecutionMilestoneCheckpoint(request, plan)
	return true, w.executeWorkerCheckpoint(request, checkpoint, false)
}

func (w *Workflow) nextExecutionMilestoneCheckpoint(request string, plan *executionMilestonePlan) state.ResumeCheckpoint {
	decision := w.state.ReadOr("last-decision", "")
	prompt := newTaskPrompt(request, plan.ActiveTaskPath)
	effort := w.config.RoutineEffort
	if decision != "" {
		prompt = decisionPrompt(request, decision, plan.ActiveTaskPath)
		effort = w.config.EscalatedEffort
	}
	return state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          fmt.Sprintf("worker-milestone-%d", plan.CurrentIndex+1),
		Role:           state.WorkerRole,
		Model:          w.config.WorkerModel,
		ReadOnly:       false,
		Effort:         effort,
		Prompt:         prompt,
		OriginalPrompt: prompt,
		Request:        request,
		Decision:       decision,
	}
}

func (w *Workflow) completeExecutionMilestone(plan *executionMilestonePlan, result packet.Result) error {
	index := plan.CurrentIndex
	if index < 0 || index >= len(plan.Milestones) {
		return errors.New("execution milestone completion index is invalid")
	}
	snapshot, err := w.captureSnapshot(w.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("capture execution milestone snapshot: %w", err)
	}
	when := w.now().UTC()
	record := &plan.Milestones[index]
	record.Status = "complete"
	record.CompletedAt = &when
	record.CompletedCallID = w.lastCallID
	record.CompletedWorkerSessionID = w.state.ReadOr("worker.id", "")
	record.Summary = result.Summary
	record.RequirementCoverage = result.RequirementCoverage
	record.Tests = result.Tests
	record.Unverified = result.Unverified
	record.ParentValidationEvidence = result.ParentValidationEvidence
	record.Artifacts = uniqueSortedStrings(result.Artifacts)
	record.Snapshot = &snapshot
	plan.CurrentIndex++
	plan.UpdatedAt = when
	return w.saveExecutionMilestonePlan(plan)
}

func (w *Workflow) syncExecutionMilestonePlan() (*executionMilestonePlan, bool, error) {
	taskID, err := w.state.TaskID()
	if err != nil {
		return nil, false, nil
	}
	activeTaskPath := w.state.ReadOr(activeTaskStateKey, "")
	if activeTaskPath == "" {
		return nil, false, nil
	}
	definitions, present, err := readExecutionMilestoneDefinitions(w.config.RepoRoot, activeTaskPath)
	if err != nil {
		return nil, false, fmt.Errorf("execution milestones: %w", err)
	}
	stored, storedErr := w.loadExecutionMilestonePlan()
	if !present {
		if errors.Is(storedErr, os.ErrNotExist) {
			return nil, false, nil
		}
		if storedErr != nil {
			return nil, false, storedErr
		}
		if stored.TaskID != taskID {
			_ = w.state.Remove(executionMilestoneStateFile)
			return nil, false, nil
		}
		if stored.CurrentIndex == 0 {
			if err := w.state.Remove(executionMilestoneStateFile); err != nil {
				return nil, false, err
			}
			return nil, false, nil
		}
		return nil, false, errors.New("Execution milestones cannot be removed after a milestone completed")
	}

	digest, err := executionMilestoneDefinitionDigest(definitions)
	if err != nil {
		return nil, false, err
	}
	if errors.Is(storedErr, os.ErrNotExist) || (storedErr == nil && (stored.TaskID != taskID || stored.ActiveTaskPath != activeTaskPath)) {
		plan := newExecutionMilestonePlan(taskID, activeTaskPath, definitions, digest, w.now())
		if err := w.saveExecutionMilestonePlan(plan); err != nil {
			return nil, false, err
		}
		return plan, true, nil
	}
	if storedErr != nil {
		return nil, false, storedErr
	}
	if err := validateStoredExecutionMilestonePlan(stored); err != nil {
		return nil, false, err
	}
	if stored.DefinitionSHA256 == digest {
		return stored, true, nil
	}
	if err := reconcileExecutionMilestoneDefinitions(stored, definitions, digest, w.now()); err != nil {
		return nil, false, err
	}
	if err := w.saveExecutionMilestonePlan(stored); err != nil {
		return nil, false, err
	}
	return stored, true, nil
}

func readExecutionMilestoneDefinitions(repoRoot, activeTaskPath string) ([]executionMilestoneDefinition, bool, error) {
	clean := filepath.Clean(filepath.FromSlash(activeTaskPath))
	if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, false, fmt.Errorf("invalid active task path %q", activeTaskPath)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, clean))
	if err != nil {
		return nil, false, fmt.Errorf("read active task: %w", err)
	}
	return parseExecutionMilestoneDefinitions(string(data))
}

func parseExecutionMilestoneDefinitions(task string) ([]executionMilestoneDefinition, bool, error) {
	lines := strings.Split(task, "\n")
	start := -1
	end := len(lines)
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == executionMilestoneHeading {
			if start >= 0 {
				return nil, true, errors.New("Execution milestones section appears more than once")
			}
			start = i + 1
			continue
		}
		if start >= 0 && i >= start && strings.HasPrefix(line, "## ") {
			end = i
			break
		}
	}
	if start < 0 {
		return nil, false, nil
	}
	section := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if strings.HasPrefix(section, "```json") {
		section = strings.TrimSpace(strings.TrimPrefix(section, "```json"))
		if !strings.HasSuffix(section, "```") {
			return nil, true, errors.New("Execution milestones JSON fence is not closed")
		}
		section = strings.TrimSpace(strings.TrimSuffix(section, "```"))
	}
	if section == "" {
		return nil, true, errors.New("Execution milestones section is empty")
	}

	var doc executionMilestoneDocument
	decoder := json.NewDecoder(strings.NewReader(section))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, true, fmt.Errorf("decode Execution milestones JSON: %w", err)
	}
	if err := ensureExecutionMilestoneJSONEOF(decoder); err != nil {
		return nil, true, err
	}
	if err := validateExecutionMilestoneDefinitions(doc.Milestones); err != nil {
		return nil, true, err
	}
	return doc.Milestones, true, nil
}

func ensureExecutionMilestoneJSONEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("Execution milestones JSON contains trailing data")
	}
	return fmt.Errorf("decode Execution milestones trailing data: %w", err)
}

func validateExecutionMilestoneDefinitions(definitions []executionMilestoneDefinition) error {
	if len(definitions) < 2 {
		return errors.New("Execution milestones requires at least two milestones; omit the section for a small task")
	}
	if len(definitions) > maxExecutionMilestones {
		return fmt.Errorf("Execution milestones exceeds maximum of %d", maxExecutionMilestones)
	}
	seen := make(map[string]struct{}, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Scope = strings.TrimSpace(definition.Scope)
		definition.Acceptance = strings.TrimSpace(definition.Acceptance)
		if !executionMilestoneIDPattern.MatchString(definition.ID) {
			return fmt.Errorf("milestone %d has invalid id %q", index+1, definition.ID)
		}
		if _, exists := seen[definition.ID]; exists {
			return fmt.Errorf("milestone id %q is duplicated", definition.ID)
		}
		seen[definition.ID] = struct{}{}
		if definition.Scope == "" || definition.Acceptance == "" {
			return fmt.Errorf("milestone %q requires non-empty scope and acceptance", definition.ID)
		}
		if len(definition.Scope) > maxExecutionMilestoneText || len(definition.Acceptance) > maxExecutionMilestoneText {
			return fmt.Errorf("milestone %q scope/acceptance exceeds %d bytes", definition.ID, maxExecutionMilestoneText)
		}
	}
	return nil
}

func newExecutionMilestonePlan(taskID, activeTaskPath string, definitions []executionMilestoneDefinition, digest string, now time.Time) *executionMilestonePlan {
	milestones := make([]executionMilestoneRecord, len(definitions))
	for index, definition := range definitions {
		milestones[index] = executionMilestoneRecord{
			ID: definition.ID, Scope: definition.Scope, Acceptance: definition.Acceptance,
			FreshWorker: definition.FreshWorker, Status: "pending",
		}
	}
	return &executionMilestonePlan{
		Version: executionMilestoneVersion, TaskID: taskID, ActiveTaskPath: activeTaskPath,
		TaskContractAuthority: executionMilestoneTaskAuthority,
		DefinitionSHA256: digest, CurrentIndex: 0, Milestones: milestones, UpdatedAt: now.UTC(),
	}
}

func (w *Workflow) loadExecutionMilestonePlan() (*executionMilestonePlan, error) {
	data, err := os.ReadFile(w.state.Path(executionMilestoneStateFile))
	if err != nil {
		return nil, err
	}
	var plan executionMilestonePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decode %s: %w", executionMilestoneStateFile, err)
	}
	return &plan, nil
}

func (w *Workflow) saveExecutionMilestonePlan(plan *executionMilestonePlan) error {
	if err := validateStoredExecutionMilestonePlan(plan); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode execution milestone state: %w", err)
	}
	return w.state.Write(executionMilestoneStateFile, string(data))
}

func validateStoredExecutionMilestonePlan(plan *executionMilestonePlan) error {
	if plan.Version != executionMilestoneVersion || plan.TaskID == "" || plan.ActiveTaskPath == "" || plan.TaskContractAuthority != executionMilestoneTaskAuthority {
		return errors.New("invalid execution milestone state identity/version/authority")
	}
	if plan.CurrentIndex < 0 || plan.CurrentIndex > len(plan.Milestones) {
		return errors.New("invalid execution milestone current_index")
	}
	for index, milestone := range plan.Milestones {
		want := "pending"
		if index < plan.CurrentIndex {
			want = "complete"
		}
		if milestone.Status != want {
			return fmt.Errorf("milestone %q status %q does not match current_index", milestone.ID, milestone.Status)
		}
		if want == "complete" && (milestone.CompletedAt == nil || milestone.Snapshot == nil) {
			return fmt.Errorf("completed milestone %q is missing durable completion evidence", milestone.ID)
		}
	}
	return nil
}

func reconcileExecutionMilestoneDefinitions(plan *executionMilestonePlan, definitions []executionMilestoneDefinition, digest string, now time.Time) error {
	if plan.CurrentIndex >= len(plan.Milestones) {
		return errors.New("completed execution milestone plan cannot be revised")
	}
	if len(definitions) <= plan.CurrentIndex {
		return errors.New("revised execution milestone plan must retain a pending current/future milestone")
	}
	for index := 0; index < plan.CurrentIndex; index++ {
		old := plan.Milestones[index]
		updated := definitions[index]
		if old.ID != updated.ID || old.Scope != updated.Scope || old.Acceptance != updated.Acceptance || old.FreshWorker != updated.FreshWorker {
			return fmt.Errorf("completed milestone %q is immutable", old.ID)
		}
	}
	completed := append([]executionMilestoneRecord(nil), plan.Milestones[:plan.CurrentIndex]...)
	pending := make([]executionMilestoneRecord, 0, len(definitions)-plan.CurrentIndex)
	for _, definition := range definitions[plan.CurrentIndex:] {
		pending = append(pending, executionMilestoneRecord{
			ID: definition.ID, Scope: definition.Scope, Acceptance: definition.Acceptance,
			FreshWorker: definition.FreshWorker, Status: "pending",
		})
	}
	plan.Milestones = append(completed, pending...)
	plan.DefinitionSHA256 = digest
	plan.UpdatedAt = now.UTC()
	return nil
}

func executionMilestoneDefinitionDigest(definitions []executionMilestoneDefinition) (string, error) {
	data, err := json.Marshal(definitions)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func renderExecutionMilestonePrompt(base string, plan *executionMilestonePlan, index int) string {
	milestone := plan.Milestones[index]
	var completed strings.Builder
	for _, item := range plan.Milestones[:index] {
		completed.WriteString("- ")
		completed.WriteString(item.ID)
		completed.WriteString(": ")
		completed.WriteString(truncateExecutionMilestonePromptText(item.Summary))
		completed.WriteByte('\n')
	}
	completedText := "none"
	if completed.Len() > 0 {
		completedText = strings.TrimRight(completed.String(), "\n")
	}
	return fmt.Sprintf(`%s

EXECUTION_MILESTONE_CONTEXT:
STATE_FILE: %s
TASK_CONTRACT_AUTHORITY: %s
INDEX: %d/%d
ID: %s
FINAL_MILESTONE: %t
FRESH_WORKER_FOR_THIS_MILESTONE: %t
COMPLETED_MILESTONES:
%s
SCOPE:
%s
ACCEPTANCE:
%s
RULES:
- task-wide Original instruction / Amendments / Contract / Must not / Acceptance criteria remain authoritative; this milestone cannot weaken or replace them.
- implement and validate only the current milestone; do not start later milestones.
- current Git/worktree and durable milestone state contain completed work; do not reimplement completed milestones unless the current milestone requires an integration correction.
- IMPLEMENTED means the current milestone acceptance is satisfied. The wrapper records completion evidence and advances without an intermediate reviewer/Sol ceremony.
- on the final milestone, integrate the full task and report task-wide requirement coverage/tests; task-wide independent review/Sol authority remains unchanged.
END_EXECUTION_MILESTONE_CONTEXT
`, strings.TrimRight(base, "\n"), executionMilestoneStateFile, executionMilestoneTaskAuthority, index+1, len(plan.Milestones), milestone.ID, index == len(plan.Milestones)-1, milestone.FreshWorker, completedText, milestone.Scope, milestone.Acceptance)
}

func truncateExecutionMilestonePromptText(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= maxCompletedMilestonePromptSummary {
		return value
	}
	runes := []rune(value)
	for len(string(runes)) > maxCompletedMilestonePromptSummary && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func uniqueSortedStrings(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
