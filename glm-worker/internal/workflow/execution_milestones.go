package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	executionMilestoneStateFile = "execution-milestones.json"
	executionMilestoneVersion   = 1
	executionMilestoneHeading   = "## Execution milestones"
	maxExecutionMilestones      = 8
	maxExecutionMilestoneText   = 2048
)

var executionMilestoneIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type executionMilestoneDefinition struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	Acceptance string `json:"acceptance"`
}

type executionMilestoneDocument struct {
	Milestones []executionMilestoneDefinition `json:"milestones"`
}

type executionMilestoneRecord struct {
	ID                  string             `json:"id"`
	Scope               string             `json:"scope"`
	Acceptance          string             `json:"acceptance"`
	Status              string             `json:"status"`
	CompletedAt         *time.Time          `json:"completed_at,omitempty"`
	CompletedCallID     string             `json:"completed_call_id,omitempty"`
	CompletedSessionID  string             `json:"completed_session_id,omitempty"`
	Summary             string             `json:"summary,omitempty"`
	RequirementCoverage string             `json:"requirement_coverage,omitempty"`
	Tests               string             `json:"tests,omitempty"`
	Unverified          string             `json:"unverified,omitempty"`
	Artifacts           []string           `json:"artifacts,omitempty"`
	Snapshot            *state.GitSnapshot `json:"snapshot,omitempty"`
}

type executionMilestonePlan struct {
	Version                  int                             `json:"version"`
	TaskID                   string                          `json:"task_id"`
	ActiveTaskPath           string                          `json:"active_task_path"`
	DefinitionSHA256         string                          `json:"definition_sha256"`
	CurrentIndex             int                             `json:"current_index"`
	Milestones               []executionMilestoneRecord      `json:"milestones"`
	DeferredParentValidation *packet.ParentValidationRequest `json:"deferred_parent_validation,omitempty"`
	UpdatedAt                time.Time                       `json:"updated_at"`
}

type executionMilestoneRunner struct {
	config config.AppConfig
	state  *state.StateStore
	base   ModelRunner
	now    func() time.Time
}

// NewExecutionMilestoneRunner keeps one semantic task in the existing workflow while allowing
// explicitly planned execution units to use fresh worker sessions. It adds no model call when the
// ACTIVE task has no Execution milestones section.
func NewExecutionMilestoneRunner(cfg config.AppConfig, st *state.StateStore, base ModelRunner) ModelRunner {
	return &executionMilestoneRunner{config: cfg, state: st, base: base, now: time.Now}
}

func (r *executionMilestoneRunner) Probe(model string) (runner.ProbeResult, error) {
	return r.base.Probe(model)
}

func (r *executionMilestoneRunner) Run(
	role state.SessionRole,
	phase string,
	model string,
	readOnly bool,
	effort string,
	prompt string,
	outputPath string,
) (runner.RunResult, error) {
	if role != state.WorkerRole {
		return r.base.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	}

	plan, enabled, err := r.loadOrInitializePlan(phase)
	if err != nil {
		return runner.RunResult{}, err
	}
	if !enabled || plan.CurrentIndex >= len(plan.Milestones) {
		return r.base.Run(role, phase, model, readOnly, effort, prompt, outputPath)
	}

	basePrompt := prompt
	for plan.CurrentIndex < len(plan.Milestones) {
		index := plan.CurrentIndex
		currentPrompt := renderExecutionMilestonePrompt(basePrompt, plan, index)
		currentOutput := outputPath
		if index < len(plan.Milestones)-1 {
			currentOutput = executionMilestoneOutputPath(outputPath, index)
		}

		result, runErr := r.base.Run(role, phase, model, readOnly, effort, currentPrompt, currentOutput)
		if runErr != nil {
			return result, runErr
		}
		parsed, parseErr := packet.ParseStructured(result.StructuredOutput)
		if parseErr != nil || parsed.Status != packet.StatusImplemented {
			// The owning workflow keeps semantic packet validation, result correction, Sol decisions,
			// provider recovery and all non-IMPLEMENTED routing authority.
			return result, nil
		}

		if index == len(plan.Milestones)-1 {
			if err := applyDeferredParentValidation(&parsed, plan.DeferredParentValidation); err != nil {
				return result, err
			}
			if data, err := parsed.MachineJSON(); err != nil {
				return result, err
			} else {
				result.StructuredOutput = data
			}
		}

		if err := r.completeMilestone(plan, index, result, parsed); err != nil {
			return result, err
		}
		if index == len(plan.Milestones)-1 {
			return result, nil
		}

		if err := r.state.Remove("worker.id", "worker.ready"); err != nil {
			return result, fmt.Errorf("rotate worker session after execution milestone %q: %w", plan.Milestones[index].ID, err)
		}
	}
	return runner.RunResult{}, errors.New("execution milestone runner reached an invalid terminal state")
}

func (r *executionMilestoneRunner) loadOrInitializePlan(phase string) (*executionMilestonePlan, bool, error) {
	taskID, err := r.state.TaskID()
	if err != nil {
		return nil, false, nil
	}
	activeTaskPath := r.state.ReadOr(activeTaskStateKey, "")
	if activeTaskPath == "" {
		return nil, false, nil
	}

	definitions, present, err := readExecutionMilestoneDefinitions(r.config.RepoRoot, activeTaskPath)
	if err != nil {
		return nil, false, fmt.Errorf("execution milestones for %s: %w", phase, err)
	}
	stored, storedErr := r.loadStoredPlan()
	if !present {
		if storedErr == nil && stored.TaskID == taskID {
			return nil, false, fmt.Errorf("execution milestones disappeared from active task after execution began")
		}
		if storedErr == nil && stored.TaskID != taskID {
			_ = r.state.Remove(executionMilestoneStateFile)
		}
		return nil, false, nil
	}

	digest, err := executionMilestoneDefinitionDigest(definitions)
	if err != nil {
		return nil, false, err
	}
	if storedErr != nil {
		if !errors.Is(storedErr, os.ErrNotExist) {
			return nil, false, storedErr
		}
		plan := newExecutionMilestonePlan(taskID, activeTaskPath, definitions, digest, r.now())
		if err := r.savePlan(plan); err != nil {
			return nil, false, err
		}
		return plan, true, nil
	}
	if stored.TaskID != taskID || stored.ActiveTaskPath != activeTaskPath {
		plan := newExecutionMilestonePlan(taskID, activeTaskPath, definitions, digest, r.now())
		if err := r.savePlan(plan); err != nil {
			return nil, false, err
		}
		return plan, true, nil
	}
	if stored.DefinitionSHA256 == digest {
		if err := validateStoredExecutionMilestonePlan(stored); err != nil {
			return nil, false, err
		}
		return stored, true, nil
	}

	if err := reconcileExecutionMilestoneDefinitions(stored, definitions, digest, r.now()); err != nil {
		return nil, false, err
	}
	if err := r.state.Remove("worker.id", "worker.ready"); err != nil {
		return nil, false, fmt.Errorf("rotate worker session after milestone revision: %w", err)
	}
	if err := r.savePlan(stored); err != nil {
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
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, true, err
	}
	if err := validateExecutionMilestoneDefinitions(doc.Milestones); err != nil {
		return nil, true, err
	}
	return doc.Milestones, true, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("Execution milestones JSON contains trailing data")
	} else if !errors.Is(err, os.ErrClosed) && err.Error() != "EOF" {
		return fmt.Errorf("decode Execution milestones trailing data: %w", err)
	}
	return nil
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
			ID: definition.ID, Scope: definition.Scope, Acceptance: definition.Acceptance, Status: "pending",
		}
	}
	return &executionMilestonePlan{
		Version:          executionMilestoneVersion,
		TaskID:           taskID,
		ActiveTaskPath:   activeTaskPath,
		DefinitionSHA256: digest,
		CurrentIndex:     0,
		Milestones:       milestones,
		UpdatedAt:        now.UTC(),
	}
}

func (r *executionMilestoneRunner) loadStoredPlan() (*executionMilestonePlan, error) {
	data, err := os.ReadFile(r.state.Path(executionMilestoneStateFile))
	if err != nil {
		return nil, err
	}
	var plan executionMilestonePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("decode %s: %w", executionMilestoneStateFile, err)
	}
	return &plan, nil
}

func (r *executionMilestoneRunner) savePlan(plan *executionMilestonePlan) error {
	if err := validateStoredExecutionMilestonePlan(plan); err != nil {
		return err
	}
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("encode execution milestone state: %w", err)
	}
	return r.state.Write(executionMilestoneStateFile, string(data))
}

func validateStoredExecutionMilestonePlan(plan *executionMilestonePlan) error {
	if plan.Version != executionMilestoneVersion || plan.TaskID == "" || plan.ActiveTaskPath == "" {
		return errors.New("invalid execution milestone state identity/version")
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
		if old.ID != updated.ID || old.Scope != updated.Scope || old.Acceptance != updated.Acceptance {
			return fmt.Errorf("completed milestone %q is immutable", old.ID)
		}
	}
	completed := append([]executionMilestoneRecord(nil), plan.Milestones[:plan.CurrentIndex]...)
	pending := make([]executionMilestoneRecord, 0, len(definitions)-plan.CurrentIndex)
	for _, definition := range definitions[plan.CurrentIndex:] {
		pending = append(pending, executionMilestoneRecord{
			ID: definition.ID, Scope: definition.Scope, Acceptance: definition.Acceptance, Status: "pending",
		})
	}
	plan.Milestones = append(completed, pending...)
	plan.DefinitionSHA256 = digest
	plan.DeferredParentValidation = nil
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
	completed := make([]string, 0, index)
	artifacts := make([]string, 0)
	for _, item := range plan.Milestones[:index] {
		completed = append(completed, item.ID)
		artifacts = append(artifacts, item.Artifacts...)
	}
	completedText := "none"
	if len(completed) > 0 {
		completedText = strings.Join(completed, ",")
	}
	artifactText := "none"
	if len(artifacts) > 0 {
		artifacts = uniqueSortedStrings(artifacts)
		artifactText = strings.Join(artifacts, ",")
	}
	final := index == len(plan.Milestones)-1
	return fmt.Sprintf(`%s

EXECUTION_MILESTONE_CONTEXT:
SOURCE_AUTHORITY: active-task-file
INDEX: %d/%d
ID: %s
FINAL_MILESTONE: %t
COMPLETED_MILESTONES: %s
COMPLETED_MILESTONE_ARTIFACTS: %s
SCOPE:
%s
ACCEPTANCE:
%s
RULES:
- task-wide Original instruction / Amendments / Contract / Must not / Acceptance criteria remain authoritative.
- implement and validate the current milestone only; do not start later milestones.
- current Git/worktree already contains completed milestone results; do not reimplement them.
- IMPLEMENTED means this milestone acceptance is satisfied. On the final milestone, integrate the full task and report task-wide requirement coverage/tests.
- do not run an independent reviewer or add Sol ceremony for milestone completion; the wrapper retains task-wide final review authority.
END_EXECUTION_MILESTONE_CONTEXT
`, strings.TrimRight(base, "\n"), index+1, len(plan.Milestones), milestone.ID, final, completedText, artifactText, milestone.Scope, milestone.Acceptance)
}

func (r *executionMilestoneRunner) completeMilestone(plan *executionMilestonePlan, index int, result runner.RunResult, parsed packet.Result) error {
	if index != plan.CurrentIndex || index < 0 || index >= len(plan.Milestones) {
		return errors.New("execution milestone completion index mismatch")
	}
	snapshot, err := state.CaptureGitSnapshot(r.config.RepoRoot)
	if err != nil {
		return fmt.Errorf("capture execution milestone snapshot: %w", err)
	}
	when := r.now().UTC()
	record := &plan.Milestones[index]
	record.Status = "complete"
	record.CompletedAt = &when
	record.CompletedCallID = result.CallID
	record.CompletedSessionID = result.SessionID
	record.Summary = parsed.Summary
	record.RequirementCoverage = parsed.RequirementCoverage
	record.Tests = parsed.Tests
	record.Unverified = parsed.Unverified
	record.Artifacts = uniqueSortedStrings(parsed.Artifacts)
	record.Snapshot = &snapshot
	if request := parsed.ParentValidationRequest(); request != nil {
		merged, err := mergeParentValidation(plan.DeferredParentValidation, request)
		if err != nil {
			return fmt.Errorf("milestone %q parent validation: %w", record.ID, err)
		}
		plan.DeferredParentValidation = merged
	}
	plan.CurrentIndex++
	plan.UpdatedAt = when
	return r.savePlan(plan)
}

func applyDeferredParentValidation(result *packet.Result, deferred *packet.ParentValidationRequest) error {
	if deferred == nil {
		return nil
	}
	merged, err := mergeParentValidation(deferred, result.ParentValidationRequest())
	if err != nil {
		return fmt.Errorf("merge deferred parent validation into final milestone: %w", err)
	}
	result.SetParentValidationRequest(merged)
	return nil
}

func mergeParentValidation(left, right *packet.ParentValidationRequest) (*packet.ParentValidationRequest, error) {
	if left == nil {
		if right == nil {
			return nil, nil
		}
		copy := *right
		return &copy, nil
	}
	if right == nil {
		copy := *left
		return &copy, nil
	}
	if left.WorkingDir != right.WorkingDir {
		return nil, fmt.Errorf("cannot preserve parent validations with different working directories %q and %q", left.WorkingDir, right.WorkingDir)
	}
	if left.Form == right.Form {
		copy := *left
		return &copy, nil
	}
	forms := map[string]bool{left.Form: true, right.Form: true}
	if forms[packet.ParentValidationGoTest] && forms[packet.ParentValidationGoTestRace] {
		return &packet.ParentValidationRequest{Form: packet.ParentValidationGoTestRace, WorkingDir: left.WorkingDir}, nil
	}
	return nil, fmt.Errorf("cannot preserve incompatible parent validation forms %q and %q", left.Form, right.Form)
}

func executionMilestoneOutputPath(outputPath string, index int) string {
	if outputPath == "" {
		return ""
	}
	ext := filepath.Ext(outputPath)
	stem := strings.TrimSuffix(outputPath, ext)
	return fmt.Sprintf("%s.milestone-%02d%s", stem, index+1, ext)
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

// Keep bytes imported deliberately close to the JSON EOF helper so malformed trailing JSON never
// degrades into a second parse path.
var _ = bytes.MinRead
