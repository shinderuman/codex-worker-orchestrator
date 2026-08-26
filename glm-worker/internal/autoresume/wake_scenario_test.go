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

type codexWakeScenario struct {
	ID                    string                   `json:"id"`
	Behavior              string                   `json:"behavior"`
	Now                   string                   `json:"now"`
	ParentThreadSpecified bool                     `json:"parent_thread_specified"`
	FiredAutomationID     string                   `json:"fired_automation_id"`
	NotifyError           string                   `json:"notify_error"`
	ResetResponses        []string                 `json:"reset_responses"`
	UpdateResponses       []string                 `json:"update_responses"`
	PauseErrors           []string                 `json:"pause_errors"`
	Verifications         []autoresumeVerification `json:"verifications"`
	ExpectedReserved      bool                     `json:"expected_reserved"`
	ExpectedActions       []string                 `json:"expected_actions"`
	ExpectedManualID      string                   `json:"expected_manual_id"`
}

type codexWakeFile struct {
	Version   int                 `json:"version"`
	Scenarios []codexWakeScenario `json:"scenarios"`
}

type codexWakeManifest struct {
	CodexWakeInstructionSHA256 string `json:"codex_wake_instruction_sha256"`
	ScenarioCount              int    `json:"scenario_count"`
}

var codexWakeCanonicalOrder = []string{"notify_parent", "fetch_reset", "update_active", "verify"}

func codexWakeRequiredScenarioIDs() map[string]bool {
	return map[string]bool{
		"codexwake-firing-updates-same-automation-id-without-delete":     false,
		"codexwake-update-failure-pauses-same-automation-without-delete": false,
		"codexwake-suggestion-card-response-is-not-reservation":          false,
		"codexwake-verify-fail-retries-once-then-pauses-without-delete":  false,
		"codexwake-past-reset-after-refetch-fails-closed":                false,
		"codexwake-missing-automation-id-fails-without-update":           false,
	}
}

func loadCodexWakeCorpus(t *testing.T) (codexWakeFile, codexWakeManifest) {
	t.Helper()
	base := filepath.Join(scenarioRepoRoot(t), "glm-worker", "scenarios")

	var corpus codexWakeFile
	corpusBytes, err := os.ReadFile(filepath.Join(base, "codexwake.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(corpusBytes, &corpus); err != nil {
		t.Fatalf("codexwake.json parse: %v", err)
	}

	var manifest codexWakeManifest
	manifestBytes, err := os.ReadFile(filepath.Join(base, "codexwake-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("codexwake-manifest.json parse: %v", err)
	}
	return corpus, manifest
}

func validateCodexWakeCorpus(corpus codexWakeFile, manifest codexWakeManifest) error {
	if corpus.Version != 1 {
		return fmt.Errorf("corpus version must be 1: got %d", corpus.Version)
	}
	required := codexWakeRequiredScenarioIDs()
	seenID := make(map[string]bool, len(corpus.Scenarios))
	for _, s := range corpus.Scenarios {
		if s.ID == "" {
			return errors.New("codex wake scenario ID empty")
		}
		if seenID[s.ID] {
			return fmt.Errorf("duplicate codex wake scenario ID %q", s.ID)
		}
		seenID[s.ID] = true
		if s.Behavior == "" {
			return fmt.Errorf("scenario %s behavior empty", s.ID)
		}
		if _, err := time.Parse(time.RFC3339, s.Now); err != nil {
			return fmt.Errorf("scenario %s now is not RFC3339: %v", s.ID, err)
		}
		for _, raw := range s.ResetResponses {
			if raw == "" {
				continue
			}
			if _, err := time.Parse(time.RFC3339, raw); err != nil {
				return fmt.Errorf("scenario %s reset response %q is not RFC3339: %v", s.ID, raw, err)
			}
		}
		if _, ok := required[s.ID]; ok {
			required[s.ID] = true
		}
		if err := validateCodexWakeActions(s); err != nil {
			return err
		}
	}
	for id, present := range required {
		if !present {
			return fmt.Errorf("codex wake corpus missing required scenario %q", id)
		}
	}
	if manifest.ScenarioCount != len(corpus.Scenarios) {
		return fmt.Errorf("manifest scenario_count = %d want %d", manifest.ScenarioCount, len(corpus.Scenarios))
	}
	if manifest.CodexWakeInstructionSHA256 == "" {
		return errors.New("manifest codex_wake_instruction_sha256 empty")
	}
	return nil
}

func validateCodexWakeActions(s codexWakeScenario) error {
	if len(s.ExpectedActions) == 0 {
		return fmt.Errorf("scenario %s expected_actions empty", s.ID)
	}
	joined := strings.Join(s.ExpectedActions, "\n")
	last := s.ExpectedActions[len(s.ExpectedActions)-1]
	if s.ExpectedReserved && last != "report_reserved" {
		return fmt.Errorf("scenario %s reserved scenario must end with report_reserved", s.ID)
	}
	if !s.ExpectedReserved && last != "report_failure" {
		return fmt.Errorf("scenario %s failure scenario must end with report_failure", s.ID)
	}
	if !s.ExpectedReserved && strings.Contains(joined, "report_reserved") {
		return fmt.Errorf("scenario %s must not report reservation success", s.ID)
	}
	for _, a := range s.ExpectedActions {
		kind := actionKindOf(a)
		if kind == "delete" || kind == "create_placeholder" {
			return fmt.Errorf("scenario %s must not delete or create an automation in the wake flow: %q", s.ID, a)
		}
		if kind == "update_active" && a != "update_active:"+s.FiredAutomationID {
			return fmt.Errorf("scenario %s must update only the fired automation ID: %q", s.ID, a)
		}
	}
	if s.ExpectedReserved {
		if strings.Contains(joined, "pause:") {
			return fmt.Errorf("scenario %s reserved scenario must not pause the automation", s.ID)
		}
		if !hasAction(s.ExpectedActions, "update_active:"+s.FiredAutomationID) || !hasAction(s.ExpectedActions, "verify") {
			return fmt.Errorf("scenario %s reserved scenario must update and verify the fired automation", s.ID)
		}
	}
	if !s.ExpectedReserved && s.FiredAutomationID != "" && !hasAction(s.ExpectedActions, "pause:"+s.FiredAutomationID) {
		return fmt.Errorf("scenario %s failure scenario must pause the fired automation instead of deleting it", s.ID)
	}
	if !s.ExpectedReserved && s.FiredAutomationID == "" && strings.Contains(joined, "pause:") {
		return fmt.Errorf("scenario %s must not pause an automation without a fired automation ID", s.ID)
	}
	if err := checkCodexWakeActionOrder(s.ExpectedActions); err != nil {
		return fmt.Errorf("scenario %s: %v", s.ID, err)
	}
	return nil
}

func checkCodexWakeActionOrder(actions []string) error {
	prev := -1
	for _, kind := range codexWakeCanonicalOrder {
		idx := -1
		for i, a := range actions {
			if actionKindOf(a) == kind {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if idx < prev {
			return fmt.Errorf("canonical wake order violated: first %q appears before the previous canonical step", kind)
		}
		prev = idx
	}
	return nil
}

func actionKindOf(a string) string {
	if idx := strings.Index(a, ":"); idx >= 0 {
		return a[:idx]
	}
	return a
}

func hasAction(actions []string, want string) bool {
	for _, a := range actions {
		if a == want {
			return true
		}
	}
	return false
}

func TestCodexWakeScenarioCorpusContract(t *testing.T) {
	corpus, manifest := loadCodexWakeCorpus(t)
	if err := validateCodexWakeCorpus(corpus, manifest); err != nil {
		t.Fatalf("codex wake corpus contract violation: %v", err)
	}
	sum := sha256FileForAutoresume(t, filepath.Join(scenarioRepoRoot(t), "codex", "instructions", "codex-auto-resume.md"))
	if sum != manifest.CodexWakeInstructionSHA256 {
		t.Fatalf("codex-auto-resume.md changed: expected %s got %s; re-confirm codex wake scenarios", manifest.CodexWakeInstructionSHA256, sum)
	}
}

func TestCodexWakeScenarioCorpusDrivenThroughRescheduleContract(t *testing.T) {
	corpus, manifest := loadCodexWakeCorpus(t)
	if err := validateCodexWakeCorpus(corpus, manifest); err != nil {
		t.Fatalf("codex wake corpus contract violation: %v", err)
	}
	for _, doc := range corpus.Scenarios {
		doc := doc
		t.Run(doc.ID, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, doc.Now)
			if err != nil {
				t.Fatalf("scenario %s now parse: %v", doc.ID, err)
			}
			verifications := make([]verification, len(doc.Verifications))
			for i, v := range doc.Verifications {
				var outcome Outcome
				switch v.Outcome {
				case "PASS":
					outcome = Pass
				case "FAIL":
					outcome = Fail
				case "UNAVAILABLE":
					outcome = Unavailable
				default:
					t.Fatalf("scenario %s unknown verification outcome %q", doc.ID, v.Outcome)
				}
				verifications[i] = verification{Outcome: outcome, ManualAppConfirmed: v.ManualAppConfirmed}
			}
			env := &scriptedWakeEnv{
				resetResponses:  doc.ResetResponses,
				updateResponses: doc.UpdateResponses,
				pauseErrors:     doc.PauseErrors,
				verifications:   verifications,
				notifyError:     doc.NotifyError,
				t:               t,
			}
			result := orchestrateWakeReschedule(env, doc.ParentThreadSpecified, doc.FiredAutomationID, now)

			got := make([]string, len(result.Actions))
			for i, a := range result.Actions {
				got[i] = a.String()
			}
			if strings.Join(got, "\n") != strings.Join(doc.ExpectedActions, "\n") {
				t.Fatalf("scenario %s actions = %v want %v (failure reason: %s)", doc.ID, got, doc.ExpectedActions, result.FailureReason)
			}
			if result.Reserved != doc.ExpectedReserved {
				t.Fatalf("scenario %s reserved = %v want %v (failure reason: %s)", doc.ID, result.Reserved, doc.ExpectedReserved, result.FailureReason)
			}
			if !doc.ExpectedReserved && result.FailureReason == "" {
				t.Fatalf("scenario %s reports failure without reason", doc.ID)
			}
			if result.ManualID != doc.ExpectedManualID {
				t.Fatalf("scenario %s manual ID = %q want %q", doc.ID, result.ManualID, doc.ExpectedManualID)
			}
			if doc.ExpectedManualID != "" && env.pauseCalls == 0 {
				t.Fatalf("scenario %s expects manual ID but no pause was attempted", doc.ID)
			}
			for _, a := range result.Actions {
				if a.Kind != actionUpdateActive {
					continue
				}
				if err := checkWakeUpdateAction(doc.ID, a, env, doc.FiredAutomationID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func checkWakeUpdateAction(scenarioID string, a action, env *scriptedWakeEnv, firedAutomationID string) error {
	if a.AutomationID != firedAutomationID {
		return fmt.Errorf("scenario %s update target = %q want fired automation %q", scenarioID, a.AutomationID, firedAutomationID)
	}
	if a.Status != "ACTIVE" {
		return fmt.Errorf("scenario %s update status = %q want ACTIVE", scenarioID, a.Status)
	}
	if want := "DTSTART:" + a.DTStart + "\nRRULE:FREQ=DAILY;COUNT=1"; a.Rrule != want {
		return fmt.Errorf("scenario %s update rrule = %q want %q", scenarioID, a.Rrule, want)
	}
	if strings.HasSuffix(a.DTStart, "Z") {
		return fmt.Errorf("scenario %s DTSTART must not end with Z: %q", scenarioID, a.DTStart)
	}
	if len(env.fetchedAt) == 0 {
		return fmt.Errorf("scenario %s updated without a fetched reset", scenarioID)
	}
	lastFetch := env.fetchedAt[len(env.fetchedAt)-1]
	wantDTStart := lastFetch.Add(wakeResetMargin).UTC().Format(dtStartLayout)
	if a.DTStart != wantDTStart {
		return fmt.Errorf("scenario %s DTSTART = %q want %q (reset + 2m)", scenarioID, a.DTStart, wantDTStart)
	}
	return nil
}
