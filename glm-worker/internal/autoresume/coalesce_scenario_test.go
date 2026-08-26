package autoresume

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type coalesceDBFixture struct {
	Status      string `json:"status"`
	NextRunAtMS int64  `json:"next_run_at_ms"`
	NextRunNull bool   `json:"next_run_null"`
}

type coalesceAutomationFixture struct {
	Dir            string             `json:"dir"`
	ID             string             `json:"id"`
	Name           string             `json:"name"`
	Status         string             `json:"status"`
	TargetThreadID string             `json:"target_thread_id"`
	Prompt         string             `json:"prompt"`
	PromptParent   bool               `json:"prompt_targets_parent"`
	DTStart        string             `json:"dtstart"`
	DB             *coalesceDBFixture `json:"db"`
	DBError        string             `json:"db_error"`
}

type coalesceScenario struct {
	ID                    string                      `json:"id"`
	Kind                  string                      `json:"kind"`
	Behavior              string                      `json:"behavior"`
	ParentThread          string                      `json:"parent_thread"`
	ResumeAt              string                      `json:"resume_at"`
	Automations           []coalesceAutomationFixture `json:"automations"`
	StatusTaskID          string                      `json:"status_task_id"`
	StatusTaskStatus      string                      `json:"status_task_status"`
	StatusResumeAvailable bool                        `json:"status_resume_available"`
	ExpectedTaskID        string                      `json:"expected_task_id"`
	ExpectedDecision      string                      `json:"expected_decision"`
	ExpectedReason        string                      `json:"expected_reason"`
	ExpectedWakeID        string                      `json:"expected_wake_id"`
	ExpectedAddedWait     int64                       `json:"expected_added_wait_seconds"`
	ExpectedResume        bool                        `json:"expected_resume"`
	ExpectedActions       []string                    `json:"expected_actions"`
}

type coalesceFile struct {
	Version   int                `json:"version"`
	Scenarios []coalesceScenario `json:"scenarios"`
}

type coalesceManifest struct {
	CoalesceInstructionSHA256 string `json:"coalesce_instruction_sha256"`
	ScenarioCount             int    `json:"scenario_count"`
}

func coalesceRequiredScenarioIDs() map[string]bool {
	return map[string]bool{
		"coalesce-active-wake-within-window-skips-glm-wake":          false,
		"coalesce-window-lower-boundary-wake-equals-resume":          false,
		"coalesce-window-upper-boundary-ten-minute-wake":             false,
		"coalesce-early-wake-creates-glm-wake":                       false,
		"coalesce-late-wake-beyond-ten-minutes-creates-glm-wake":     false,
		"coalesce-no-wake-for-parent-thread-creates-glm-wake":        false,
		"coalesce-paused-wake-creates-glm-wake":                      false,
		"coalesce-unverifiable-schedule-creates-glm-wake":            false,
		"coalesce-multiple-matching-wakes-creates-glm-wake":          false,
		"coalesce-wake-id-binding-mismatch-creates-glm-wake":         false,
		"coalesce-non-wake-automations-ignored-creates-glm-wake":     false,
		"coalesce-empty-automation-store-creates-glm-wake":           false,
		"coalesce-wake-receipt-matching-rate-limited-task-resumes":   false,
		"coalesce-wake-receipt-task-id-mismatch-skips-resume":        false,
		"coalesce-wake-receipt-status-not-rate-limited-skips-resume": false,
		"coalesce-wake-receipt-resume-unavailable-skips-resume":      false,
		"coalesce-wake-receipt-without-packet-task-id-do-not-resume": false,
	}
}

func loadCoalesceCorpus(t *testing.T) (coalesceFile, coalesceManifest) {
	t.Helper()
	base := filepath.Join(scenarioRepoRoot(t), "glm-worker", "scenarios")

	var corpus coalesceFile
	corpusBytes, err := os.ReadFile(filepath.Join(base, "coalesce.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(corpusBytes, &corpus); err != nil {
		t.Fatalf("coalesce.json parse: %v", err)
	}

	var manifest coalesceManifest
	manifestBytes, err := os.ReadFile(filepath.Join(base, "coalesce-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("coalesce-manifest.json parse: %v", err)
	}
	return corpus, manifest
}

func validateCoalesceCorpus(corpus coalesceFile, manifest coalesceManifest) error {
	if corpus.Version != 1 {
		return fmt.Errorf("corpus version must be 1: got %d", corpus.Version)
	}
	required := coalesceRequiredScenarioIDs()
	seenID := make(map[string]bool, len(corpus.Scenarios))
	for _, s := range corpus.Scenarios {
		if s.ID == "" {
			return errors.New("coalesce scenario ID empty")
		}
		if seenID[s.ID] {
			return fmt.Errorf("duplicate coalesce scenario ID %q", s.ID)
		}
		seenID[s.ID] = true
		if s.Behavior == "" {
			return fmt.Errorf("scenario %s behavior empty", s.ID)
		}
		if _, ok := required[s.ID]; ok {
			required[s.ID] = true
		}
		if s.Kind == "packet" {
			if err := validateCoalescePacketScenario(s); err != nil {
				return err
			}
		} else if s.Kind == "wake_receipt" {
			if err := validateCoalesceWakeReceiptScenario(s); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("scenario %s unknown kind %q", s.ID, s.Kind)
		}
	}
	for id, present := range required {
		if !present {
			return fmt.Errorf("coalesce corpus missing required scenario %q", id)
		}
	}
	if manifest.ScenarioCount != len(corpus.Scenarios) {
		return fmt.Errorf("manifest scenario_count = %d want %d", manifest.ScenarioCount, len(corpus.Scenarios))
	}
	if manifest.CoalesceInstructionSHA256 == "" {
		return errors.New("manifest coalesce_instruction_sha256 empty")
	}
	return nil
}

func validateCoalescePacketScenario(s coalesceScenario) error {
	if _, err := time.Parse(time.RFC3339, s.ResumeAt); err != nil {
		return fmt.Errorf("scenario %s resume_at is not RFC3339: %v", s.ID, err)
	}
	if len(s.Automations) > 0 && s.ParentThread == "" {
		return fmt.Errorf("scenario %s parent_thread empty", s.ID)
	}
	if s.ExpectedDecision != DecisionCoalesce && s.ExpectedDecision != DecisionCreateGLMWake {
		return fmt.Errorf("scenario %s expected_decision %q is not a known decision", s.ID, s.ExpectedDecision)
	}
	joined := strings.Join(s.ExpectedActions, "\n")
	if strings.Contains(joined, "create_placeholder") || strings.Contains(joined, "update_active") {
		return fmt.Errorf("scenario %s must not mutate automations from the coalesce gate: %v", s.ID, s.ExpectedActions)
	}
	if s.ExpectedDecision == DecisionCoalesce {
		if s.ExpectedWakeID == "" {
			return fmt.Errorf("scenario %s coalesce without expected_wake_id", s.ID)
		}
		if strings.Contains(joined, "proceed_glm_reservation") {
			return fmt.Errorf("scenario %s coalesce scenario must not proceed to the GLM reservation: %v", s.ID, s.ExpectedActions)
		}
	} else {
		if s.ExpectedReason == "" {
			return fmt.Errorf("scenario %s create_glm_wake without expected_reason", s.ID)
		}
		if s.ExpectedWakeID != "" || s.ExpectedAddedWait != 0 {
			return fmt.Errorf("scenario %s create_glm_wake must not expect a coalesced wake", s.ID)
		}
		if !strings.Contains(joined, "proceed_glm_reservation") {
			return fmt.Errorf("scenario %s must proceed to the GLM reservation: %v", s.ID, s.ExpectedActions)
		}
	}
	return nil
}

func validateCoalesceWakeReceiptScenario(s coalesceScenario) error {
	if s.StatusTaskStatus == "" || s.ExpectedActions == nil {
		return fmt.Errorf("scenario %s wake_receipt fields incomplete", s.ID)
	}
	if s.ExpectedTaskID == "" && s.ExpectedResume {
		return fmt.Errorf("scenario %s must not resume without an expected task ID (fail closed)", s.ID)
	}
	if s.ExpectedResume && s.ExpectedReason != "" {
		return fmt.Errorf("scenario %s resume scenario must not carry a mismatch reason", s.ID)
	}
	if !s.ExpectedResume && s.ExpectedReason == "" {
		return fmt.Errorf("scenario %s mismatch scenario requires a reason", s.ID)
	}
	return nil
}

type coalesceStatusSnapshot struct {
	TaskID          string
	TaskStatus      string
	ResumeAvailable bool
}

func decideCoalescedWakeResume(snapshot coalesceStatusSnapshot, expectedTaskID string) (bool, string) {
	if expectedTaskID == "" {
		return false, "expected task ID unavailable"
	}
	if snapshot.TaskStatus != "rate-limited" {
		return false, fmt.Sprintf("task status is %q want rate-limited", snapshot.TaskStatus)
	}
	if !snapshot.ResumeAvailable {
		return false, "resume is not available"
	}
	if snapshot.TaskID != expectedTaskID {
		return false, "task id mismatch"
	}
	return true, ""
}

func coalescePacketActions(result CoalesceResult) []string {
	actions := []string{"check_wake_coalesce"}
	if result.Decision == DecisionCoalesce {
		return append(actions, "report_coalesced:"+result.WakeAutomationID)
	}
	return append(actions, "proceed_glm_reservation")
}

func coalesceWakeReceiptActions(resume bool, reason string, expectedTaskID string) []string {
	actions := []string{"status_check"}
	if resume {
		return append(actions, "resume")
	}
	if expectedTaskID == "" {
		return append(actions, "manual_fallback:"+reason)
	}
	return append(actions, "report_mismatch:"+reason)
}

func materializeCoalesceFixtures(t *testing.T, dir string, parentThread string, automations []coalesceAutomationFixture) DBReader {
	t.Helper()
	rows := map[string]DBRow{}
	failures := map[string]error{}
	for _, a := range automations {
		prompt := a.Prompt
		if strings.HasPrefix(a.Dir, codexWakeKeyPrefix) {
			thread := coalesceOtherParentThread
			if a.PromptParent {
				thread = parentThread
			}
			prompt = coalescePrompt(thread)
		}
		writeCoalesceTOML(t, filepath.Join(dir, a.Dir, "automation.toml"), a.ID, a.Name, a.Status, a.TargetThreadID, prompt, a.DTStart)
		if a.DBError != "" {
			failures[a.ID] = errors.New(a.DBError)
			continue
		}
		if a.DB == nil {
			continue
		}
		at, err := time.Parse(dtStartLayout, a.DTStart)
		if err != nil {
			t.Fatalf("scenario automation %s dtstart parse: %v", a.Dir, err)
		}
		nextRun := a.DB.NextRunAtMS
		if nextRun == 0 {
			nextRun = at.UnixMilli()
		}
		rows[a.ID] = DBRow{
			ID:         a.ID,
			Status:     a.DB.Status,
			Rrule:      "DTSTART:" + a.DTStart + "\nRRULE:FREQ=DAILY;COUNT=1",
			NextRunAt:  nextRun,
			HasNextRun: !a.DB.NextRunNull,
		}
	}
	return func(dbPath, key string) (DBRow, error) {
		if failure, ok := failures[key]; ok {
			return DBRow{}, failure
		}
		row, ok := rows[key]
		if !ok {
			return DBRow{}, ErrRowNotFound
		}
		return row, nil
	}
}

func TestCoalesceScenarioCorpusContract(t *testing.T) {
	corpus, manifest := loadCoalesceCorpus(t)
	if err := validateCoalesceCorpus(corpus, manifest); err != nil {
		t.Fatalf("coalesce corpus contract violation: %v", err)
	}
	sum := sha256FileForAutoresume(t, filepath.Join(scenarioRepoRoot(t), "codex", "instructions", "glm-auto-resume.md"))
	if sum != manifest.CoalesceInstructionSHA256 {
		t.Fatalf("glm-auto-resume.md changed: expected %s got %s; re-confirm coalesce scenarios", manifest.CoalesceInstructionSHA256, sum)
	}
}

func TestCoalesceScenarioCorpusDrivenThroughCheckContract(t *testing.T) {
	corpus, manifest := loadCoalesceCorpus(t)
	if err := validateCoalesceCorpus(corpus, manifest); err != nil {
		t.Fatalf("coalesce corpus contract violation: %v", err)
	}
	for _, doc := range corpus.Scenarios {
		doc := doc
		t.Run(doc.ID, func(t *testing.T) {
			if doc.Kind == "wake_receipt" {
				resume, reason := decideCoalescedWakeResume(coalesceStatusSnapshot{
					TaskID:          doc.StatusTaskID,
					TaskStatus:      doc.StatusTaskStatus,
					ResumeAvailable: doc.StatusResumeAvailable,
				}, doc.ExpectedTaskID)
				if resume != doc.ExpectedResume || reason != doc.ExpectedReason {
					t.Fatalf("wake receipt = (%v, %q) want (%v, %q)", resume, reason, doc.ExpectedResume, doc.ExpectedReason)
				}
				assertCoalesceActions(t, doc.ID, coalesceWakeReceiptActions(resume, reason, doc.ExpectedTaskID), doc.ExpectedActions)
				return
			}

			dir := t.TempDir()
			readDB := materializeCoalesceFixtures(t, dir, doc.ParentThread, doc.Automations)
			result, err := CheckCoalesce(CoalesceParams{
				ParentThreadID:  doc.ParentThread,
				ResumeAtRFC3339: doc.ResumeAt,
				AutomationsDir:  dir,
				DBPath:          "unused",
			}, readDB)
			if err != nil {
				t.Fatalf("scenario %s check error: %v", doc.ID, err)
			}
			if result.Decision != doc.ExpectedDecision {
				t.Fatalf("scenario %s decision = %q want %q (reason: %s)", doc.ID, result.Decision, doc.ExpectedDecision, result.Reason)
			}
			if result.Reason != doc.ExpectedReason {
				t.Fatalf("scenario %s reason = %q want %q", doc.ID, result.Reason, doc.ExpectedReason)
			}
			if result.WakeAutomationID != doc.ExpectedWakeID {
				t.Fatalf("scenario %s wake id = %q want %q", doc.ID, result.WakeAutomationID, doc.ExpectedWakeID)
			}
			if result.AddedWaitSeconds != doc.ExpectedAddedWait {
				t.Fatalf("scenario %s added wait = %d want %d", doc.ID, result.AddedWaitSeconds, doc.ExpectedAddedWait)
			}
			assertCoalesceActions(t, doc.ID, coalescePacketActions(result), doc.ExpectedActions)
		})
	}
}

func assertCoalesceActions(t *testing.T, scenarioID string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("scenario %s actions = %v want %v", scenarioID, got, want)
	}
}
