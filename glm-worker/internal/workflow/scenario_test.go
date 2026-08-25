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
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type scenarioStep struct {
	Packet json.RawMessage `json:"packet"`
	Error  string          `json:"error"`

	Signal string `json:"signal,omitempty"`

	Raw string `json:"raw,omitempty"`

	Usage   scenarioUsage `json:"usage,omitempty"`
	CostUSD float64       `json:"cost_usd,omitempty"`
}

type scenarioArtifact struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

const scenarioArtifactDirToken = "{{ARTIFACT_DIR}}"

const (
	scenarioPlanSeed          = "# plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/001-scenario.md`\n"
	scenarioPlanMutated       = "glm edited plan\n"
	scenarioActiveTaskPath    = "IMPLEMENTATION_TASKS/001-scenario.md"
	scenarioActiveTaskSeed    = "# ACTIVE task\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\n- scenario seed\n"
	scenarioActiveTaskMutated = "glm edited active task\n"
)

const telemetryClockInjectedStart = "injected-start"

type scenarioUsage struct {
	InputTokens  int64 `json:"input_tokens,omitempty"`
	OutputTokens int64 `json:"output_tokens,omitempty"`
}

type scenarioStats struct {
	ModelCalls       int `json:"model_calls"`
	WorkerCalls      int `json:"worker_calls"`
	ReviewerCalls    int `json:"reviewer_calls"`
	TransientRetries int `json:"transient_retries"`
	ResumeCommands   int `json:"resume_commands"`
	ProbeSuccess     int `json:"probe_success"`
	ProbeFailure     int `json:"probe_failure"`
	TotalAICalls     int `json:"total_ai_calls"`
}

type scenarioTelemetry struct {
	TaskCalls          int     `json:"task_calls"`
	ProbeCalls         int     `json:"probe_calls"`
	EventCalls         int     `json:"event_calls"`
	TaskInputTokens    int64   `json:"task_input_tokens"`
	TaskOutputTokens   int64   `json:"task_output_tokens"`
	TaskCostUSD        float64 `json:"task_cost_usd"`
	ProbeInputTokens   int64   `json:"probe_input_tokens"`
	ProbeOutputTokens  int64   `json:"probe_output_tokens"`
	ProbeCostUSD       float64 `json:"probe_cost_usd"`
	ProbeResolvedModel string  `json:"probe_resolved_model"`
}

type promptExpectation struct {
	Index       int      `json:"index"`
	Contains    []string `json:"contains"`
	NotContains []string `json:"not_contains,omitempty"`
}

type scenarioDoc struct {
	ID                   string         `json:"id"`
	Behavior             string         `json:"behavior"`
	Request              string         `json:"request"`
	Entry                string         `json:"entry"`
	InstructionFiles     []string       `json:"instruction_files"`
	ChangedPaths         []string       `json:"changed_paths"`
	RunnerSteps          []scenarioStep `json:"runner_steps"`
	ExpectedModels       []string       `json:"expected_models"`
	ExpectedPacketStatus string         `json:"expected_packet_status"`
	ExpectedPacketRisk   string         `json:"expected_packet_risk"`
	ExpectedTaskStatus   string         `json:"expected_task_status"`
	MustNotPass          bool           `json:"must_not_pass"`

	ExpectedPrompts []promptExpectation `json:"expected_prompts,omitempty"`

	ExpectedErrorStatus string `json:"expected_error_status,omitempty"`

	ExpectedProbeCount int `json:"expected_probe_count,omitempty"`

	ExpectedRunCount *int `json:"expected_run_count,omitempty"`

	ForbiddenErrorStatus string `json:"forbidden_error_status,omitempty"`

	ProbeErrors []string `json:"probe_errors,omitempty"`

	ProbeBlank bool `json:"probe_blank,omitempty"`

	ProbeResponses []string `json:"probe_responses,omitempty"`

	ProbeIsError bool `json:"probe_is_error,omitempty"`

	SleepAdvanceMinutes int `json:"sleep_advance_minutes,omitempty"`

	ReviewerMutatesWorktree bool `json:"reviewer_mutates_worktree,omitempty"`

	ReportOnlyMutatesWorktree bool `json:"report_only_mutates_worktree,omitempty"`

	WorkerMutatesPlanFile bool `json:"worker_mutates_plan_file,omitempty"`

	WorkerMutatesActiveTaskFile bool `json:"worker_mutates_active_task_file,omitempty"`

	PlanMissingActiveSection bool `json:"plan_missing_active_section,omitempty"`

	ActiveTaskFileMissing bool `json:"active_task_file_missing,omitempty"`

	SeedActiveTaskMetadata bool `json:"seed_active_task_metadata,omitempty"`

	ResumeCarriesActiveTaskBlock bool `json:"resume_carries_active_task_block,omitempty"`

	PlanFileInitiallyAbsent bool `json:"plan_file_initially_absent,omitempty"`

	PlanFileTrackedAbsent bool `json:"plan_file_tracked_absent,omitempty"`

	ExpectedOutputContains []string `json:"expected_output_contains,omitempty"`

	ExpectedStats *scenarioStats `json:"expected_stats,omitempty"`

	ExpectedTelemetry *scenarioTelemetry `json:"expected_telemetry,omitempty"`

	ExpectedTelemetryClock string `json:"expected_telemetry_clock,omitempty"`

	ExpectedCheckpoint string `json:"expected_checkpoint,omitempty"`

	ExpectedProviderClassification string `json:"expected_provider_classification,omitempty"`

	ArtifactFiles []scenarioArtifact `json:"artifact_files,omitempty"`
}

type scenarioFile struct {
	Version   int           `json:"version"`
	Scenarios []scenarioDoc `json:"scenarios"`
}

type manifestEntry struct {
	Path      string   `json:"path"`
	SHA256    string   `json:"sha256"`
	Scenarios []string `json:"scenarios"`
}

type manifestFile struct {
	Version          int             `json:"version"`
	InstructionFiles []manifestEntry `json:"instruction_files"`
}

func scenarioRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join("glm-worker", "scenarios", "scenarios.json")
	for d := dir; d != string(filepath.Separator); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
			return d
		}
	}
	t.Fatalf("scenario corpus root not found from %s", dir)
	return ""
}

func loadCorpus(t *testing.T) (scenarioFile, manifestFile) {
	t.Helper()
	base := filepath.Join(scenarioRepoRoot(t), "glm-worker", "scenarios")

	var sc scenarioFile
	scenarioBytes, err := os.ReadFile(filepath.Join(base, "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(scenarioBytes, &sc); err != nil {
		t.Fatalf("scenarios.json parse: %v", err)
	}

	var mf manifestFile
	manifestBytes, err := os.ReadFile(filepath.Join(base, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &mf); err != nil {
		t.Fatalf("manifest.json parse: %v", err)
	}
	return sc, mf
}

func sha256File(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func validateCorpus(sc scenarioFile, mf manifestFile) error {
	if sc.Version != 1 {
		return fmt.Errorf("corpus version must be 1: got %d", sc.Version)
	}
	if mf.Version != 1 {
		return fmt.Errorf("manifest version must be 1: got %d", mf.Version)
	}

	knownEntry := map[string]bool{"new_task": true, "resume": true}
	knownStatus := map[string]bool{"IMPLEMENTED": true, "PASS": true, "FIX_REQUIRED": true, "NEEDS_SOL_DECISION": true, "NEEDS_SOL_REVIEW": true}
	knownError := map[string]bool{"provider_unavailable": true, "rate_limited": true, "worker_error": true}
	knownRisk := map[string]bool{"LOW": true, "HIGH": true}
	knownTask := map[string]bool{"active": true, "waiting-decision": true, "waiting-sol-review": true, "complete": true, "rate-limited": true, "provider-unavailable": true}

	seenID := make(map[string]bool, len(sc.Scenarios))
	for _, s := range sc.Scenarios {
		if s.ID == "" {
			return errors.New("scenario ID empty")
		}
		if seenID[s.ID] {
			return fmt.Errorf("duplicate scenario ID %q", s.ID)
		}
		seenID[s.ID] = true
		if s.Behavior == "" {
			return fmt.Errorf("scenario %s behavior empty", s.ID)
		}
		if s.Request == "" {
			return fmt.Errorf("scenario %s request empty", s.ID)
		}
		if !knownEntry[s.Entry] {
			return fmt.Errorf("scenario %s unknown entry %q", s.ID, s.Entry)
		}
		if len(s.InstructionFiles) == 0 {
			return fmt.Errorf("scenario %s instruction_files empty", s.ID)
		}
		if len(s.RunnerSteps) == 0 {
			return fmt.Errorf("scenario %s runner_steps empty", s.ID)
		}
		if len(s.ExpectedModels) == 0 {
			return fmt.Errorf("scenario %s expected_models empty", s.ID)
		}
		if !knownStatus[s.ExpectedPacketStatus] {
			return fmt.Errorf("scenario %s unknown expected packet status %q", s.ID, s.ExpectedPacketStatus)
		}
		if !knownRisk[s.ExpectedPacketRisk] {
			return fmt.Errorf("scenario %s unknown expected packet risk %q", s.ID, s.ExpectedPacketRisk)
		}
		if !knownTask[s.ExpectedTaskStatus] {
			return fmt.Errorf("scenario %s unknown expected task status %q", s.ID, s.ExpectedTaskStatus)
		}
		if s.MustNotPass && s.ExpectedPacketStatus == "PASS" {
			return fmt.Errorf("scenario %s must_not_pass with expected PASS", s.ID)
		}
		mutationHooks := 0
		for _, on := range []bool{s.ReviewerMutatesWorktree, s.ReportOnlyMutatesWorktree, s.WorkerMutatesPlanFile, s.WorkerMutatesActiveTaskFile} {
			if on {
				mutationHooks++
			}
		}
		if mutationHooks > 1 {
			return fmt.Errorf("scenario %s declares multiple mutation hooks", s.ID)
		}
		preCallStopHooks := 0
		for _, on := range []bool{s.PlanMissingActiveSection, s.ActiveTaskFileMissing} {
			if on {
				preCallStopHooks++
			}
		}
		if preCallStopHooks > 1 || preCallStopHooks > 0 && mutationHooks > 0 {
			return fmt.Errorf("scenario %s declares incompatible pre-call hooks", s.ID)
		}

		if s.PlanFileTrackedAbsent {
			if s.WorkerMutatesPlanFile || s.ReviewerMutatesWorktree || s.ReportOnlyMutatesWorktree {
				return fmt.Errorf("scenario %s plan_file_tracked_absent with mutation hook", s.ID)
			}
			if s.ExpectedPacketStatus != "NEEDS_SOL_REVIEW" {
				return fmt.Errorf("scenario %s plan file tracked absent must expect fail-closed NEEDS_SOL_REVIEW", s.ID)
			}
			if s.ExpectedRunCount == nil || *s.ExpectedRunCount != 0 {
				return fmt.Errorf("scenario %s plan file tracked absent must expect zero model runs", s.ID)
			}
		}

		if s.ReportOnlyMutatesWorktree && s.ExpectedPacketStatus != "NEEDS_SOL_REVIEW" {
			return fmt.Errorf("scenario %s report-only mutation must expect fail-closed NEEDS_SOL_REVIEW", s.ID)
		}

		if s.WorkerMutatesPlanFile && s.ExpectedPacketStatus != "NEEDS_SOL_REVIEW" {
			return fmt.Errorf("scenario %s plan file mutation must expect fail-closed NEEDS_SOL_REVIEW", s.ID)
		}

		if s.WorkerMutatesActiveTaskFile {
			if s.ExpectedPacketStatus != "NEEDS_SOL_REVIEW" {
				return fmt.Errorf("scenario %s active task file mutation must expect fail-closed NEEDS_SOL_REVIEW", s.ID)
			}
		}

		for _, unresolvable := range []bool{s.PlanMissingActiveSection, s.ActiveTaskFileMissing} {
			if !unresolvable {
				continue
			}
			if s.ExpectedPacketStatus != "NEEDS_SOL_REVIEW" {
				return fmt.Errorf("scenario %s active task unresolvable must expect fail-closed NEEDS_SOL_REVIEW", s.ID)
			}
			if s.ExpectedRunCount == nil || *s.ExpectedRunCount != 0 {
				return fmt.Errorf("scenario %s active task unresolvable must expect zero model runs", s.ID)
			}
		}
		if s.PlanFileInitiallyAbsent && !s.WorkerMutatesPlanFile {
			return fmt.Errorf("scenario %s plan_file_initially_absent without worker_mutates_plan_file", s.ID)
		}

		if s.ResumeCarriesActiveTaskBlock {
			if s.Entry != "resume" {
				return fmt.Errorf("scenario %s resume_carries_active_task_block without resume entry", s.ID)
			}
			if s.PlanMissingActiveSection || s.ActiveTaskFileMissing || s.PlanFileTrackedAbsent || s.PlanFileInitiallyAbsent {
				return fmt.Errorf("scenario %s resume_carries_active_task_block with metadata-degrading hook", s.ID)
			}
		}
		for i, want := range s.ExpectedOutputContains {
			if want == "" {
				return fmt.Errorf("scenario %s empty expected_output_contains entry %d", s.ID, i)
			}
		}
		if s.ExpectedErrorStatus != "" {
			if !knownError[s.ExpectedErrorStatus] {
				return fmt.Errorf("scenario %s unknown expected error status %q", s.ID, s.ExpectedErrorStatus)
			}
			if s.ForbiddenErrorStatus != "" && s.ForbiddenErrorStatus == s.ExpectedErrorStatus {
				return fmt.Errorf("scenario %s forbidden error status equals expected", s.ID)
			}
		}
		if s.ForbiddenErrorStatus != "" && !knownError[s.ForbiddenErrorStatus] {
			return fmt.Errorf("scenario %s unknown forbidden error status %q", s.ID, s.ForbiddenErrorStatus)
		}
		if s.Entry == "resume" && s.ExpectedErrorStatus == "" && s.RunnerSteps[len(s.RunnerSteps)-1].Error == "" && s.RunnerSteps[len(s.RunnerSteps)-1].Signal == "" && s.RunnerSteps[len(s.RunnerSteps)-1].Raw == "" && len(s.RunnerSteps[len(s.RunnerSteps)-1].Packet) == 0 {
			return fmt.Errorf("scenario %s empty terminal step", s.ID)
		}
		knownCheckpoint := map[string]bool{"": true, "none": true, "rate-limited": true, "provider-unavailable": true}
		if !knownCheckpoint[s.ExpectedCheckpoint] {
			return fmt.Errorf("scenario %s unknown expected checkpoint %q", s.ID, s.ExpectedCheckpoint)
		}
		if s.SleepAdvanceMinutes < 0 {
			return fmt.Errorf("scenario %s negative sleep_advance_minutes", s.ID)
		}
		knownTelemetryClock := map[string]bool{"": true, telemetryClockInjectedStart: true}
		if !knownTelemetryClock[s.ExpectedTelemetryClock] {
			return fmt.Errorf("scenario %s unknown expected telemetry clock %q", s.ID, s.ExpectedTelemetryClock)
		}
		if s.ExpectedTelemetryClock == telemetryClockInjectedStart && s.SleepAdvanceMinutes > 0 {
			return fmt.Errorf("scenario %s injected-start telemetry clock conflicts with sleep_advance_minutes", s.ID)
		}
		if s.ExpectedProviderClassification != "" && s.ExpectedCheckpoint != "provider-unavailable" {
			return fmt.Errorf("scenario %s expected_provider_classification without provider-unavailable checkpoint", s.ID)
		}
		if es := s.ExpectedStats; es != nil {
			if es.WorkerCalls+es.ReviewerCalls != es.ModelCalls {
				return fmt.Errorf("scenario %s worker+reviewer calls != model calls", s.ID)
			}
			if es.ModelCalls+es.ProbeSuccess+es.ProbeFailure != es.TotalAICalls {
				return fmt.Errorf("scenario %s total_ai_calls != model+probe calls", s.ID)
			}
		}
		if et := s.ExpectedTelemetry; et != nil && s.ExpectedStats != nil {
			if et.TaskCalls != s.ExpectedStats.ModelCalls || et.ProbeCalls != s.ExpectedStats.ProbeSuccess+s.ExpectedStats.ProbeFailure {
				return fmt.Errorf("scenario %s telemetry call counts disagree with expected_stats", s.ID)
			}
		}
		if len(s.RunnerSteps) != len(s.ExpectedModels) {
			return fmt.Errorf("scenario %s runner_steps/expected_models count mismatch: %d vs %d", s.ID, len(s.RunnerSteps), len(s.ExpectedModels))
		}
		artifactNames := make(map[string]bool, len(s.ArtifactFiles))
		for _, af := range s.ArtifactFiles {
			if af.Name == "" || af.Name == "." || af.Name == ".." || filepath.IsAbs(af.Name) || strings.ContainsAny(af.Name, `/\`) {
				return fmt.Errorf("scenario %s invalid artifact file name %q", s.ID, af.Name)
			}
			if af.Content == "" {
				return fmt.Errorf("scenario %s artifact %s content empty", s.ID, af.Name)
			}
			artifactNames[af.Name] = true
		}
		artifactTokenUsed := false
		for i, step := range s.RunnerSteps {
			hasPacket := len(step.Packet) > 0
			hasErr := step.Error != ""
			hasSignal := step.Signal != ""
			hasRaw := step.Raw != ""
			kinds := 0
			for _, present := range []bool{hasPacket, hasErr, hasSignal, hasRaw} {
				if present {
					kinds++
				}
			}
			if kinds == 0 {
				return fmt.Errorf("scenario %s step %d empty", s.ID, i)
			}
			if kinds > 1 {
				return fmt.Errorf("scenario %s step %d has multiple terminal kinds", s.ID, i)
			}
			if hasPacket {
				result, parseErr := packet.ParseStructured(step.Packet)
				if parseErr != nil {
					return fmt.Errorf("scenario %s step %d invalid result: %w", s.ID, i, parseErr)
				}
				if err := validateTypedResult(result); err != nil {
					return fmt.Errorf("scenario %s step %d invalid result: %v", s.ID, i, err)
				}
				packetText := string(step.Packet)
				if err := validateScenarioArtifactToken(s.ID, packetText, artifactNames); err != nil {
					return err
				}
				if strings.Contains(packetText, scenarioArtifactDirToken) {
					artifactTokenUsed = true
				}
			}
		}
		if len(s.ArtifactFiles) > 0 && !artifactTokenUsed {
			return fmt.Errorf("scenario %s declares artifact_files but no runner step packet references %s", s.ID, scenarioArtifactDirToken)
		}
		for i, exp := range s.ExpectedPrompts {
			if exp.Index < 0 {
				return fmt.Errorf("scenario %s expected_prompts %d negative index", s.ID, i)
			}
			if len(exp.Contains) == 0 {
				return fmt.Errorf("scenario %s expected_prompts %d contains empty", s.ID, i)
			}
			for _, want := range exp.Contains {
				if want == "" {
					return fmt.Errorf("scenario %s expected_prompts %d empty contains entry", s.ID, i)
				}
			}
			for _, forbidden := range exp.NotContains {
				if forbidden == "" {
					return fmt.Errorf("scenario %s expected_prompts %d empty not_contains entry", s.ID, i)
				}
			}
		}
	}

	manifestPaths := make(map[string]manifestEntry, len(mf.InstructionFiles))
	for _, e := range mf.InstructionFiles {
		if e.Path == "" {
			return errors.New("manifest path empty")
		}
		if _, dup := manifestPaths[e.Path]; dup {
			return fmt.Errorf("duplicate manifest path %q", e.Path)
		}
		if e.SHA256 == "" {
			return fmt.Errorf("manifest %s sha256 empty", e.Path)
		}
		seenScenario := make(map[string]bool, len(e.Scenarios))
		for _, sid := range e.Scenarios {
			if sid == "" {
				return fmt.Errorf("manifest %s empty scenario ref", e.Path)
			}
			if seenScenario[sid] {
				return fmt.Errorf("manifest %s duplicate scenario %q", e.Path, sid)
			}
			seenScenario[sid] = true
			if !seenID[sid] {
				return fmt.Errorf("manifest %s references unknown scenario %q", e.Path, sid)
			}
		}
		manifestPaths[e.Path] = e
	}

	for _, s := range sc.Scenarios {
		seenFile := make(map[string]bool, len(s.InstructionFiles))
		for _, f := range s.InstructionFiles {
			if f == "" {
				return fmt.Errorf("scenario %s empty instruction_files entry", s.ID)
			}
			if seenFile[f] {
				return fmt.Errorf("scenario %s duplicate instruction_files %q", s.ID, f)
			}
			seenFile[f] = true
			entry, ok := manifestPaths[f]
			if !ok {
				return fmt.Errorf("scenario %s grounds on %s not pinned in manifest", s.ID, f)
			}
			listed := false
			for _, sid := range entry.Scenarios {
				if sid == s.ID {
					listed = true
					break
				}
			}
			if !listed {
				return fmt.Errorf("scenario %s grounds on %s but not listed by manifest", s.ID, f)
			}
		}
	}

	scenarioFiles := make(map[string]map[string]bool, len(sc.Scenarios))
	for _, s := range sc.Scenarios {
		set := make(map[string]bool, len(s.InstructionFiles))
		for _, f := range s.InstructionFiles {
			set[f] = true
		}
		scenarioFiles[s.ID] = set
	}
	for _, e := range mf.InstructionFiles {
		for _, sid := range e.Scenarios {
			if !scenarioFiles[sid][e.Path] {
				return fmt.Errorf("manifest %s lists scenario %q that does not declare path in instruction_files", e.Path, sid)
			}
		}
	}
	return nil
}

func validateScenarioArtifactToken(id string, line string, artifactNames map[string]bool) error {
	rest := line
	for {
		i := strings.Index(rest, scenarioArtifactDirToken)
		if i < 0 {
			return nil
		}
		after := rest[i+len(scenarioArtifactDirToken):]
		matched := false
		for name := range artifactNames {
			if strings.HasPrefix(after, "/"+name) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("scenario %s line references %s without matching artifact_files entry: %q", id, scenarioArtifactDirToken, line)
		}
		rest = after
	}
}

func TestScenarioCorpusContract(t *testing.T) {
	sc, mf := loadCorpus(t)
	if err := validateCorpus(sc, mf); err != nil {
		t.Fatalf("corpus contract violation: %v", err)
	}

	root := scenarioRepoRoot(t)
	for _, entry := range mf.InstructionFiles {
		actual := sha256File(t, filepath.Join(root, entry.Path))
		if actual != entry.SHA256 {
			t.Errorf("instruction file %s changed: expected %s got %s; re-confirm scenarios %v", entry.Path, entry.SHA256, actual, entry.Scenarios)
		}
	}
}

func validCorpus() (scenarioFile, manifestFile) {
	sc := scenarioFile{
		Version: 1,
		Scenarios: []scenarioDoc{{
			ID:               "s1",
			Behavior:         "b",
			Request:          "req",
			Entry:            "new_task",
			InstructionFiles: []string{"codex/glm-worker/prompts/WORKER.md"},
			RunnerSteps: []scenarioStep{
				{Packet: json.RawMessage(`{"status":"IMPLEMENTED","risk":"LOW","summary":"s","requirement_coverage":"c","tests":"t","unverified":"none"}`)},
				{Packet: json.RawMessage(`{"status":"PASS","risk":"LOW","summary":"s","requirement_coverage":"c","invariants":"i","test_evidence":"e","issues":"none","residual_risk":"none","targets":["final diff"]}`)},
			},
			ExpectedModels:       []string{"opus", "haiku"},
			ExpectedPacketStatus: "PASS",
			ExpectedPacketRisk:   "LOW",
			ExpectedTaskStatus:   "complete",
			MustNotPass:          false,
		}},
	}
	mf := manifestFile{
		Version: 1,
		InstructionFiles: []manifestEntry{{
			Path:      "codex/glm-worker/prompts/WORKER.md",
			SHA256:    "deadbeef",
			Scenarios: []string{"s1"},
		}},
	}
	return sc, mf
}

func TestScenarioCorpusContractRejectsInvalid(t *testing.T) {
	sc, mf := validCorpus()
	if err := validateCorpus(sc, mf); err != nil {
		t.Fatalf("baseline corpus must be valid: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(sc *scenarioFile, mf *manifestFile)
		want   string
	}{
		{"corpus version", func(sc *scenarioFile, _ *manifestFile) { sc.Version = 0 }, "corpus version"},
		{"manifest version", func(_ *scenarioFile, mf *manifestFile) { mf.Version = 2 }, "manifest version"},
		{"empty scenario ID", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].ID = "" }, "scenario ID empty"},
		{"duplicate scenario ID", func(sc *scenarioFile, _ *manifestFile) {
			dup := sc.Scenarios[0]
			dup.ID = "s1"
			sc.Scenarios = append(sc.Scenarios, dup)
		}, "duplicate scenario ID"},
		{"empty behavior", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].Behavior = "" }, "behavior empty"},
		{"empty request", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].Request = "" }, "request empty"},
		{"unknown entry", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].Entry = "decision" }, "unknown entry"},
		{"empty instruction_files", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].InstructionFiles = nil }, "instruction_files empty"},
		{"empty runner_steps", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].RunnerSteps = nil }, "runner_steps empty"},
		{"empty expected_models", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].ExpectedModels = nil }, "expected_models empty"},
		{"unknown expected packet status", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].ExpectedPacketStatus = "DONE" }, "expected packet status"},
		{"unknown expected packet risk", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].ExpectedPacketRisk = "MEDIUM" }, "expected packet risk"},
		{"unknown expected task status", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].ExpectedTaskStatus = "done" }, "expected task status"},
		{"must_not_pass with expected PASS", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].MustNotPass = true }, "must_not_pass"},
		{"multiple mutation hooks", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ReviewerMutatesWorktree = true
			sc.Scenarios[0].ReportOnlyMutatesWorktree = true
		}, "multiple mutation hooks"},
		{"report-only mutation without fail-closed terminal", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ReportOnlyMutatesWorktree = true
		}, "must expect fail-closed NEEDS_SOL_REVIEW"},
		{"plan file mutation without fail-closed terminal", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].WorkerMutatesPlanFile = true
		}, "plan file mutation must expect fail-closed NEEDS_SOL_REVIEW"},
		{"plan file initially absent without mutation hook", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].PlanFileInitiallyAbsent = true
		}, "without worker_mutates_plan_file"},
		{"plan file tracked absent with mutation hook", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].PlanFileTrackedAbsent = true
			sc.Scenarios[0].WorkerMutatesPlanFile = true
			sc.Scenarios[0].ExpectedPacketStatus = "NEEDS_SOL_REVIEW"
		}, "with mutation hook"},
		{"plan file tracked absent without zero-run expectation", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].PlanFileTrackedAbsent = true
			sc.Scenarios[0].ExpectedPacketStatus = "NEEDS_SOL_REVIEW"
		}, "must expect zero model runs"},
		{"empty expected output contains", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedOutputContains = []string{""}
		}, "empty expected_output_contains"},
		{"step count mismatch", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedModels = append(sc.Scenarios[0].ExpectedModels, "sonnet")
		}, "count mismatch"},
		{"empty step", func(sc *scenarioFile, _ *manifestFile) { sc.Scenarios[0].RunnerSteps[0] = scenarioStep{} }, "step 0 empty"},
		{"step both packet and error", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].RunnerSteps[0].Error = "boom"
		}, "multiple terminal kinds"},
		{"step both packet and raw", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].RunnerSteps[0].Raw = "{\"status\":\"IMPLEMENTED\"}\n"
		}, "multiple terminal kinds"},
		{"step invalid packet", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].RunnerSteps[0] = scenarioStep{Packet: json.RawMessage(`{"status":"PASS","risk":"LOW","summary":"s"}`)}
		}, "invalid result"},
		{"duplicate manifest path", func(_ *scenarioFile, mf *manifestFile) {
			mf.InstructionFiles = append(mf.InstructionFiles, manifestEntry{Path: "codex/glm-worker/prompts/WORKER.md", SHA256: "x", Scenarios: []string{"s1"}})
		}, "duplicate manifest path"},
		{"manifest empty sha256", func(_ *scenarioFile, mf *manifestFile) { mf.InstructionFiles[0].SHA256 = "" }, "sha256 empty"},
		{"manifest duplicate scenario ref", func(_ *scenarioFile, mf *manifestFile) {
			mf.InstructionFiles[0].Scenarios = []string{"s1", "s1"}
		}, "duplicate scenario"},
		{"manifest unknown scenario ref", func(_ *scenarioFile, mf *manifestFile) {
			mf.InstructionFiles[0].Scenarios = []string{"nope"}
		}, "unknown scenario"},
		{"scenario grounds on unpinned file", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].InstructionFiles = []string{"codex/instructions/worker/go.md"}
		}, "not pinned in manifest"},
		{"scenario not listed by manifest", func(sc *scenarioFile, mf *manifestFile) {
			second := sc.Scenarios[0]
			second.ID = "s2"
			sc.Scenarios = append(sc.Scenarios, second)
			mf.InstructionFiles[0].Scenarios = []string{"s2"}
		}, "not listed by manifest"},
		{"manifest lists scenario missing path", func(_ *scenarioFile, mf *manifestFile) {
			mf.InstructionFiles = append(mf.InstructionFiles, manifestEntry{Path: "codex/glm-worker/prompts/REVIEWER.md", SHA256: "x", Scenarios: []string{"s1"}})
		}, "does not declare path"},
		{"unknown expected checkpoint", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedCheckpoint = "waiting"
		}, "unknown expected checkpoint"},
		{"negative sleep advance", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].SleepAdvanceMinutes = -1
		}, "negative sleep_advance_minutes"},
		{"unknown expected telemetry clock", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedTelemetryClock = "wall"
		}, "unknown expected telemetry clock"},
		{"telemetry clock with sleep advance", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedTelemetryClock = telemetryClockInjectedStart
			sc.Scenarios[0].SleepAdvanceMinutes = 90
		}, "conflicts with sleep_advance_minutes"},
		{"classification without unavailable checkpoint", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedProviderClassification = "probe-contract"
		}, "without provider-unavailable checkpoint"},
		{"stats role counts disagree", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedStats = &scenarioStats{ModelCalls: 2, WorkerCalls: 2, ReviewerCalls: 1, TotalAICalls: 5, ProbeSuccess: 1, ProbeFailure: 2}
		}, "worker+reviewer calls != model calls"},
		{"stats total ai calls disagree", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedStats = &scenarioStats{ModelCalls: 2, WorkerCalls: 1, ReviewerCalls: 1, ProbeSuccess: 1, TotalAICalls: 5}
		}, "total_ai_calls != model+probe calls"},
		{"telemetry counts disagree with stats", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedStats = &scenarioStats{ModelCalls: 2, WorkerCalls: 1, ReviewerCalls: 1, TotalAICalls: 2}
			sc.Scenarios[0].ExpectedTelemetry = &scenarioTelemetry{TaskCalls: 3, ProbeCalls: 0}
		}, "telemetry call counts disagree"},
		{"expected prompts negative index", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedPrompts = []promptExpectation{{Index: -1, Contains: []string{"MODE:"}}}
		}, "negative index"},
		{"expected prompts empty contains", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ExpectedPrompts = []promptExpectation{{Index: 0}}
		}, "contains empty"},
		{"artifact file name with separator", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ArtifactFiles = []scenarioArtifact{{Name: "nested/evidence.md", Content: "x"}}
		}, "invalid artifact file name"},
		{"artifact content empty", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ArtifactFiles = []scenarioArtifact{{Name: "evidence.md"}}
		}, "content empty"},
		{"artifact files unused", func(sc *scenarioFile, _ *manifestFile) {
			sc.Scenarios[0].ArtifactFiles = []scenarioArtifact{{Name: "evidence.md", Content: "x"}}
		}, "no runner step packet references"},
		{"artifact token without declared file", func(sc *scenarioFile, _ *manifestFile) {
			packetText := string(sc.Scenarios[0].RunnerSteps[0].Packet)
			sc.Scenarios[0].RunnerSteps[0].Packet = json.RawMessage(strings.Replace(packetText, `"unverified":"none"`, `"unverified":"none","artifacts":["`+scenarioArtifactDirToken+`/evidence.md"]`, 1))
		}, "without matching artifact_files entry"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			sc, mf := validCorpus()
			c.mutate(&sc, &mf)
			err := validateCorpus(sc, mf)
			if err == nil {
				t.Fatal("expected contract violation, got nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q, want substring %q", err.Error(), c.want)
			}
		})
	}
}

func stepsFromScenario(doc scenarioDoc) []runnerStep {
	steps := make([]runnerStep, len(doc.RunnerSteps))
	for i, s := range doc.RunnerSteps {

		structured := string(s.Packet)
		if s.Raw != "" {
			structured = s.Raw
		}
		var runErr error
		if s.Error != "" {
			runErr = errors.New(s.Error)
		}
		output := ""
		if s.Signal != "" {
			output = s.Signal
			runErr = errors.New("exit status 1")
		}
		result := runner.RunResult{
			TopLevelUsage: runner.TokenUsage{
				InputTokens:  s.Usage.InputTokens,
				OutputTokens: s.Usage.OutputTokens,
			},
			TotalCostUSD: s.CostUSD,
		}
		steps[i] = runnerStep{output: output, runErr: runErr, result: result, structured: structured}
	}
	return steps
}

func workflowErrorKind(err error) string {
	var workerErr *WorkerError
	var limitErr runner.ZaiRateLimitError
	var providerErr *runner.ProviderUnavailableError
	switch {
	case errors.As(err, &limitErr):
		return "rate_limited"
	case errors.As(err, &providerErr):
		return "provider_unavailable"
	case errors.As(err, &workerErr):
		return "worker_error"
	default:
		return "internal"
	}
}

func validateTypedResult(result packet.Result) error {
	if err := packet.ValidateWorkerResult(result); err == nil {
		return nil
	}
	return packet.ValidateReviewerResult(result)
}

func lastPacketFromOutput(t *testing.T, out string) packet.Result {
	t.Helper()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	emitted := ""
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			emitted = strings.TrimSpace(lines[i])
			break
		}
	}
	if emitted == "" {
		t.Fatalf("no emitted result in output:\n%s", out)
	}
	value, err := packet.ParseStructured([]byte(emitted))
	if err != nil {
		t.Fatalf("emitted result is not machine JSON: %v:\n%s", err, out)
	}
	return value
}

func runScenario(t *testing.T, doc scenarioDoc) {
	t.Helper()
	st := newStateStoreT(t)
	r := &scriptedRunner{steps: stepsFromScenario(doc)}
	if len(doc.ArtifactFiles) > 0 {

		r.artifactFiles = doc.ArtifactFiles
		r.taskArtifactDir = st.PrepareArtifactDir
	}
	if doc.ProbeErrors != nil {
		r.probeErrs = make([]error, len(doc.ProbeErrors))
		for i, text := range doc.ProbeErrors {
			if text != "" {
				r.probeErrs[i] = errors.New(text)
			}
		}
	}
	r.probeBlankResponse = doc.ProbeBlank
	r.probeResponses = doc.ProbeResponses
	r.probeIsError = doc.ProbeIsError
	w := newWorkflowT(t, st, r)
	if doc.SleepAdvanceMinutes > 0 {

		clock := newFakeClock()
		step := time.Duration(doc.SleepAdvanceMinutes) * time.Minute
		w.now = clock.nowFunc
		w.sleep = func(d time.Duration) {
			clock.sleeps = append(clock.sleeps, d)
			clock.now = clock.now.Add(step)
		}
	}
	buf := &bytes.Buffer{}
	w.output = buf
	var mutationRepoRoot string
	if doc.ReviewerMutatesWorktree || doc.ReportOnlyMutatesWorktree || doc.WorkerMutatesPlanFile || doc.PlanFileTrackedAbsent || doc.WorkerMutatesActiveTaskFile || doc.PlanMissingActiveSection || doc.ActiveTaskFileMissing || doc.SeedActiveTaskMetadata || doc.ResumeCarriesActiveTaskBlock {
		mutationRepoRoot = initMutationRepo(t)
		mutate := func(root string) error {
			return os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("mutated\n"), 0o644)
		}
		seedActiveTaskMetadata := func() {
			t.Helper()
			if err := os.MkdirAll(filepath.Join(mutationRepoRoot, implementationTasksDir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mutationRepoRoot, scenarioActiveTaskPath), []byte(scenarioActiveTaskSeed), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(mutationRepoRoot, implementationPlanFile), []byte(scenarioPlanSeed), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if doc.WorkerMutatesPlanFile {

			if !doc.PlanFileInitiallyAbsent {
				seedActiveTaskMetadata()
			}
			mutate = func(root string) error {
				return os.WriteFile(filepath.Join(root, implementationPlanFile), []byte(scenarioPlanMutated), 0o644)
			}
		}
		if doc.WorkerMutatesActiveTaskFile {
			seedActiveTaskMetadata()
			mutate = func(root string) error {
				return os.WriteFile(filepath.Join(root, scenarioActiveTaskPath), []byte(scenarioActiveTaskMutated), 0o644)
			}
		}
		if doc.SeedActiveTaskMetadata || doc.ResumeCarriesActiveTaskBlock {

			seedActiveTaskMetadata()
			mutate = nil
		}
		if doc.PlanMissingActiveSection {

			if err := os.WriteFile(filepath.Join(mutationRepoRoot, implementationPlanFile), []byte("parent owned plan\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			mutate = nil
		}
		if doc.ActiveTaskFileMissing {

			if err := os.WriteFile(filepath.Join(mutationRepoRoot, implementationPlanFile), []byte(scenarioPlanSeed), 0o644); err != nil {
				t.Fatal(err)
			}
			mutate = nil
		}
		if doc.PlanFileTrackedAbsent {

			if err := os.WriteFile(filepath.Join(mutationRepoRoot, implementationPlanFile), []byte(scenarioPlanSeed), 0o644); err != nil {
				t.Fatal(err)
			}
			gitIn(t, mutationRepoRoot, "add", implementationPlanFile)
			if err := os.Remove(filepath.Join(mutationRepoRoot, implementationPlanFile)); err != nil {
				t.Fatal(err)
			}
			mutate = nil
		}
		mr := &mutatingRunner{
			repoRoot: mutationRepoRoot,
			steps:    stepsFromScenario(doc),
			mutate:   mutate,
		}
		switch {
		case doc.ReportOnlyMutatesWorktree:
			mr.mutatePhase = "worker-report-only-1"
		case doc.WorkerMutatesPlanFile || doc.WorkerMutatesActiveTaskFile:
			mr.mutatePhase = "worker-new"
		}
		w.runner = mr
		w.config.RepoRoot = mutationRepoRoot
		w.captureSnapshot = state.CaptureGitSnapshot
	}
	if doc.ChangedPaths != nil {
		changedPaths := doc.ChangedPaths
		w.collectChangedPaths = func(string, string) ([]string, error) { return changedPaths, nil }
	}

	var scenarioErr error
	switch doc.Entry {
	case "new_task":
		scenarioErr = w.ExecuteNewTask(doc.Request)
	case "resume":
		if err := st.Write("last-request", doc.Request); err != nil {
			t.Fatal(err)
		}

		{

			savedPrompt := "p"
			if doc.ResumeCarriesActiveTaskBlock {
				savedPrompt = newTaskPrompt(doc.Request, scenarioActiveTaskPath)
				if err := st.Write(activeTaskStateKey, scenarioActiveTaskPath); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
				Stage:                             state.ResumeStageWorker,
				Phase:                             "worker-new",
				Role:                              state.WorkerRole,
				Model:                             "opus",
				Effort:                            "high",
				Prompt:                            savedPrompt,
				OriginalPrompt:                    savedPrompt,
				Request:                           doc.Request,
				ProviderUnavailable:               true,
				ProviderUnavailableClassification: "http-503",
				ProviderUnavailableProbes:         4,
			}); err != nil {
				t.Fatal(err)
			}
			if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
				t.Fatal(err)
			}
		}
		scenarioErr = w.ExecuteResume()
	default:
		t.Fatalf("unsupported entry %q for scenario %s", doc.Entry, doc.ID)
	}

	if doc.ReviewerMutatesWorktree || doc.ReportOnlyMutatesWorktree || doc.WorkerMutatesPlanFile || doc.PlanFileTrackedAbsent || doc.WorkerMutatesActiveTaskFile || doc.PlanMissingActiveSection || doc.ActiveTaskFileMissing || doc.SeedActiveTaskMetadata || doc.ResumeCarriesActiveTaskBlock {
		mr := w.runner.(*mutatingRunner)
		r.prompts = mr.prompts
		r.models = mr.models
		if doc.WorkerMutatesPlanFile {

			if content, err := os.ReadFile(filepath.Join(mutationRepoRoot, implementationPlanFile)); err != nil || string(content) != scenarioPlanMutated {
				t.Fatalf("workerによるplan file変更が自動復元・編集されています: %q %v", content, err)
			}
		} else if doc.WorkerMutatesActiveTaskFile {

			if content, err := os.ReadFile(filepath.Join(mutationRepoRoot, scenarioActiveTaskPath)); err != nil || string(content) != scenarioActiveTaskMutated {
				t.Fatalf("workerによるACTIVE task file変更が自動復元・編集されています: %q %v", content, err)
			}
		} else if doc.PlanFileTrackedAbsent {

			if _, err := os.Stat(filepath.Join(mutationRepoRoot, implementationPlanFile)); !os.IsNotExist(err) {
				t.Fatalf("追跡中plan欠損が復元・生成されています: %v", err)
			}
			if out := gitIn(t, mutationRepoRoot, "ls-files", "--", implementationPlanFile); out == "" {
				t.Fatal("seed失敗: plan fileがindexへ追跡されていません")
			}
		} else if doc.ReviewerMutatesWorktree || doc.ReportOnlyMutatesWorktree {
			if content, err := os.ReadFile(filepath.Join(mutationRepoRoot, "tracked.txt")); err != nil || string(content) != "mutated\n" {
				t.Fatalf("mutation hook対象呼出でrepositoryが変更されていません: %q %v", content, err)
			}
		}
	}

	if len(r.prompts) == 0 && doc.ExpectedProbeCount == 0 && doc.ExpectedRunCount == nil {
		t.Fatal("production runner was not invoked")
	}
	for i, p := range r.prompts {

		isReformat := strings.Contains(p, "PACKETだけを再出力") || strings.Contains(p, "結果を再出力") || strings.Contains(p, "結果だけを再出力")
		hasRequest := strings.Contains(p, doc.Request)
		hasMode := strings.Contains(p, "MODE:") || strings.Contains(p, "REVIEW_MODE:")

		isResume := strings.Contains(p, "再開してください")
		if !(hasMode || isReformat || isResume) {
			t.Fatalf("prompt %d is not a production-generated prompt:\n%s", i, p)
		}
		if !isReformat && !isResume && !hasRequest {
			t.Fatalf("prompt %d does not transmit USER_REQUEST:\n%s", i, p)
		}
	}
	if len(r.prompts) > 0 && !strings.Contains(r.prompts[0], "MODE:") && !strings.Contains(r.prompts[0], "再開してください") {
		t.Fatalf("worker prompt is not a production NEW_TASK/RESUME prompt:\n%s", r.prompts[0])
	}

	if doc.ExpectedRunCount != nil {
		if len(r.prompts) != *doc.ExpectedRunCount {
			t.Fatalf("本task Run回数 = %d want %d", len(r.prompts), *doc.ExpectedRunCount)
		}
	}
	if doc.ExpectedProbeCount > 0 {
		if len(r.probes) != doc.ExpectedProbeCount {
			t.Fatalf("probe回数 = %d want %d", len(r.probes), doc.ExpectedProbeCount)
		}
	}

	if doc.ExpectedRunCount == nil {
		if got, want := strings.Join(r.models, ","), strings.Join(doc.ExpectedModels, ","); got != want {
			t.Fatalf("model routing = %q want %q", got, want)
		}
	}

	for _, exp := range doc.ExpectedPrompts {
		if exp.Index >= len(r.prompts) {
			t.Fatalf("expected_prompts index %d out of range: prompts=%d", exp.Index, len(r.prompts))
		}
		got := r.prompts[exp.Index]
		for _, want := range exp.Contains {
			if !strings.Contains(got, want) {
				t.Fatalf("prompt %d does not contain %q:\n%s", exp.Index, want, got)
			}
		}
		for _, forbidden := range exp.NotContains {
			if strings.Contains(got, forbidden) {
				t.Fatalf("prompt %d must not contain %q:\n%s", exp.Index, forbidden, got)
			}
		}
	}

	if doc.ExpectedErrorStatus != "" {
		if scenarioErr == nil {
			t.Fatalf("expected error terminal %s, got success with packet:\n%s", doc.ExpectedErrorStatus, buf.String())
		}
		if got := workflowErrorKind(scenarioErr); got != doc.ExpectedErrorStatus {
			t.Fatalf("error terminal kind = %q want %q (%v)", got, doc.ExpectedErrorStatus, scenarioErr)
		}
		if doc.ForbiddenErrorStatus != "" && workflowErrorKind(scenarioErr) == doc.ForbiddenErrorStatus {
			t.Fatalf("error terminal must not be %s:\n%v", doc.ForbiddenErrorStatus, scenarioErr)
		}
	} else {
		if scenarioErr != nil {
			t.Fatalf("scenario execution error: %v", scenarioErr)
		}
		pkt := lastPacketFromOutput(t, buf.String())
		if err := validateTypedResult(pkt); err != nil {
			t.Fatalf("emitted result fails production contract: %v", err)
		}
		if string(pkt.Status) != doc.ExpectedPacketStatus {
			t.Fatalf("packet status = %q want %q", pkt.Status, doc.ExpectedPacketStatus)
		}
		if string(pkt.Risk) != doc.ExpectedPacketRisk {
			t.Fatalf("packet risk = %q want %q", pkt.Risk, doc.ExpectedPacketRisk)
		}
		for _, want := range doc.ExpectedOutputContains {
			if !strings.Contains(buf.String(), want) {
				t.Fatalf("wrapper output does not contain %q:\n%s", want, buf.String())
			}
		}
		if doc.MustNotPass && pkt.Status == packet.StatusPass {
			t.Fatalf("must_not_pass scenario ended in PASS")
		}
	}
	if got := string(st.TaskStatus()); got != doc.ExpectedTaskStatus {
		t.Fatalf("task status = %q want %q", got, doc.ExpectedTaskStatus)
	}
	if doc.ExpectedCheckpoint != "" {
		cp, cpErr := st.LoadResumeCheckpoint()
		switch doc.ExpectedCheckpoint {
		case "none":
			if cpErr == nil {
				t.Fatalf("resume checkpoint = %#v want none", cp)
			}
		case "rate-limited":
			if cpErr != nil || !cp.RateLimited {
				t.Fatalf("rate-limited checkpointが保存されていない: %#v err=%v", cp, cpErr)
			}
		case "provider-unavailable":
			if cpErr != nil || !cp.ProviderUnavailable {
				t.Fatalf("provider-unavailable checkpointが保存されていない: %#v err=%v", cp, cpErr)
			}
			if doc.ExpectedProviderClassification != "" && cp.ProviderUnavailableClassification != doc.ExpectedProviderClassification {
				t.Fatalf("provider-unavailable分類 = %q want %q", cp.ProviderUnavailableClassification, doc.ExpectedProviderClassification)
			}
		}
	}
	if doc.ExpectedStats != nil {
		verifyScenarioStats(t, st, *doc.ExpectedStats)
	}
	if doc.ExpectedTelemetry != nil {
		verifyScenarioTelemetry(t, st, *doc.ExpectedTelemetry)
	}
	if doc.ExpectedTelemetryClock == telemetryClockInjectedStart {
		if err := checkTelemetryClock(taskLogs(t, st)); err != nil {
			t.Fatalf("scenario %s telemetry clock: %v", doc.ID, err)
		}
	}
}

func verifyScenarioStats(t *testing.T, st *state.StateStore, want scenarioStats) {
	t.Helper()
	stats := currentStats(t, st)
	probeCalls := stats.ProbeOutcome["probe_success"] + stats.ProbeOutcome["probe_failure"]
	got := scenarioStats{
		ModelCalls:       stats.ModelCalls,
		WorkerCalls:      stats.WorkerCalls,
		ReviewerCalls:    stats.ReviewerCalls,
		TransientRetries: stats.TransientRetries,
		ResumeCommands:   stats.ResumeCommands,
		ProbeSuccess:     stats.ProbeOutcome["probe_success"],
		ProbeFailure:     stats.ProbeOutcome["probe_failure"],
		TotalAICalls:     stats.ModelCalls + probeCalls,
	}
	if got != want {
		t.Fatalf("stats = %+v want %+v", got, want)
	}
}

func verifyScenarioTelemetry(t *testing.T, st *state.StateStore, want scenarioTelemetry) {
	t.Helper()
	got := scenarioTelemetry{}
	for _, l := range taskLogs(t, st) {
		switch l.CallType {
		case state.CallTypeTask:
			got.TaskCalls++
			got.TaskInputTokens += l.TopLevelUsage.InputTokens
			got.TaskOutputTokens += l.TopLevelUsage.OutputTokens
			got.TaskCostUSD += l.TotalCostUSD
		case state.CallTypeProbe:
			got.ProbeCalls++
			got.ProbeInputTokens += l.TopLevelUsage.InputTokens
			got.ProbeOutputTokens += l.TopLevelUsage.OutputTokens
			got.ProbeCostUSD += l.TotalCostUSD
			if want.ProbeResolvedModel != "" {
				usage, ok := l.ResolvedModelUsage[want.ProbeResolvedModel]
				if !ok || usage.OutputTokens <= 0 {
					t.Fatalf("probe recordのresolved model %qが記録されていない: %+v", want.ProbeResolvedModel, l.ResolvedModelUsage)
				}
			}
		case state.CallTypeEvent:
			got.EventCalls++
		}
	}
	if got.TaskCalls != want.TaskCalls || got.ProbeCalls != want.ProbeCalls || got.EventCalls != want.EventCalls {
		t.Fatalf("telemetry call counts = task/probe/event %d/%d/%d want %d/%d/%d",
			got.TaskCalls, got.ProbeCalls, got.EventCalls, want.TaskCalls, want.ProbeCalls, want.EventCalls)
	}
	if got.TaskInputTokens != want.TaskInputTokens || got.TaskOutputTokens != want.TaskOutputTokens ||
		got.ProbeInputTokens != want.ProbeInputTokens || got.ProbeOutputTokens != want.ProbeOutputTokens {
		t.Fatalf("telemetry tokens = %+v want %+v", got, want)
	}
	if got.TaskCostUSD != want.TaskCostUSD || got.ProbeCostUSD != want.ProbeCostUSD {
		t.Fatalf("telemetry cost = task/probe %v/%v want %v/%v", got.TaskCostUSD, got.ProbeCostUSD, want.TaskCostUSD, want.ProbeCostUSD)
	}
}

func checkTelemetryClock(logs []state.ModelCallLog) error {
	if len(logs) == 0 {
		return errors.New("telemetry recordが無いためclock検証が空で通過")
	}
	for i, l := range logs {
		if l.StartedAt != testFixedTime || l.CompletedAt != testFixedTime {
			return fmt.Errorf("record %d (%s %s)時刻 = %s/%s want %s: wall clock取得の再導入", i, l.CallType, l.Phase, l.StartedAt, l.CompletedAt, testFixedTime)
		}
		if l.WallDurationMS != 0 {
			return fmt.Errorf("record %d (%s %s)wall duration = %d: wall clock由来のduration", i, l.CallType, l.Phase, l.WallDurationMS)
		}
	}
	return nil
}

func TestScenarioCorpusDrivenThroughProductionGate(t *testing.T) {
	sc, mf := loadCorpus(t)
	if err := validateCorpus(sc, mf); err != nil {
		t.Fatalf("corpus contract violation: %v", err)
	}
	if len(sc.Scenarios) == 0 {
		t.Fatal("scenarios.json has no scenarios")
	}
	for _, doc := range sc.Scenarios {
		doc := doc
		t.Run(doc.ID, func(t *testing.T) {
			runScenario(t, doc)
		})
	}
}

func TestTelemetryClockCheckRejectsWallClockRecords(t *testing.T) {
	injected := state.ModelCallLog{CallType: state.CallTypeTask, Phase: "worker-new", StartedAt: testFixedTime, CompletedAt: testFixedTime}
	if err := checkTelemetryClock([]state.ModelCallLog{injected}); err != nil {
		t.Fatalf("注入clock由来recordが拒否されました: %v", err)
	}
	wallNow := time.Now().UTC()
	cases := []struct {
		name string
		logs []state.ModelCallLog
		want string
	}{
		{"wall clock timestamps", []state.ModelCallLog{{
			CallType:    state.CallTypeTask,
			Phase:       "worker-new",
			StartedAt:   wallNow,
			CompletedAt: wallNow,
		}}, "wall clock取得の再導入"},
		{"wall clock duration", []state.ModelCallLog{injected, {
			CallType:       state.CallTypeTask,
			Phase:          "review",
			StartedAt:      testFixedTime,
			CompletedAt:    testFixedTime,
			WallDurationMS: wallNow.Sub(testFixedTime).Milliseconds(),
		}}, "wall clock由来のduration"},
		{"no records", nil, "clock検証が空で通過"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := checkTelemetryClock(c.logs)
			if err == nil {
				t.Fatal("wall clock由来recordが検出されません")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %q want substring %q", err.Error(), c.want)
			}
		})
	}
}
