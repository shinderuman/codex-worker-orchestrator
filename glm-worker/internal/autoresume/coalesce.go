package autoresume

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	DecisionCoalesce      = "coalesce"
	DecisionCreateGLMWake = "create_glm_wake"

	codexWakeKeyPrefix = "codex-5h-wake-"

	maxCoalesceDelay = 10 * time.Minute
)

type CoalesceParams struct {
	ParentThreadID  string
	ResumeAtRFC3339 string
	AutomationsDir  string
	DBPath          string
}

type CoalesceResult struct {
	Decision         string
	Reason           string
	ParentThread     string
	ResumeAtUTC      string
	WakeAutomationID string
	WakeThread       string
	WakeNextRunUTC   string
	AddedWaitSeconds int64
}

type wakeEntity struct {
	Candidate AutomationTOML
	Problem   string
}

func CheckCoalesce(params CoalesceParams, readDB DBReader) (CoalesceResult, error) {
	if !keyPattern.MatchString(params.ParentThreadID) {
		return CoalesceResult{}, fmt.Errorf("invalid parent thread ID format: %q", params.ParentThreadID)
	}
	resumeAt, err := time.Parse(time.RFC3339, params.ResumeAtRFC3339)
	if err != nil {
		return CoalesceResult{}, fmt.Errorf("invalid resume time %q: %v", params.ResumeAtRFC3339, err)
	}
	resumeAtUTC := resumeAt.UTC()
	result := CoalesceResult{
		Decision:     DecisionCreateGLMWake,
		ParentThread: params.ParentThreadID,
		ResumeAtUTC:  resumeAtUTC.Format(time.RFC3339),
	}

	entities, reason := enumerateWakeEntities(params.AutomationsDir, params.ParentThreadID)
	if reason != "" {
		result.Reason = reason
		return result, nil
	}
	candidates := []AutomationTOML{}
	for _, entity := range entities {
		if entity.Problem != "" {
			result.Reason = entity.Problem
			return result, nil
		}
		candidates = append(candidates, entity.Candidate)
	}
	if len(candidates) == 0 {
		result.Reason = "no codex wake automation targets the parent thread"
		return result, nil
	}
	if len(candidates) > 1 {
		result.Reason = fmt.Sprintf("ambiguous codex wake automations targeting the parent thread: %d", len(candidates))
		return result, nil
	}
	return evaluateWakeCandidate(candidates[0], params, resumeAtUTC, readDB, result)
}

func enumerateWakeEntities(dir string, parentThreadID string) ([]wakeEntity, string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ""
		}
		return nil, "wake enumeration unavailable: " + err.Error()
	}

	entities := []wakeEntity{}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), codexWakeKeyPrefix) {
			continue
		}
		toml, problem := readWakeTOML(filepath.Join(dir, entry.Name(), "automation.toml"), entry.Name())
		if problem == "" && !strings.Contains(toml.Prompt, parentThreadID) {
			continue
		}
		entities = append(entities, wakeEntity{Candidate: toml, Problem: problem})
	}
	return entities, ""
}

func readWakeTOML(path string, dirName string) (AutomationTOML, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return AutomationTOML{}, "wake automation entity unreadable: " + path
	}
	toml, err := parseAutomationTOML(data)
	if err != nil {
		return AutomationTOML{}, fmt.Sprintf("wake automation TOML invalid (%s): %v", path, err)
	}
	if toml.ID != dirName {
		return AutomationTOML{}, fmt.Sprintf("wake automation id %q does not match its directory %q", toml.ID, dirName)
	}
	if toml.Name != dirName {
		return AutomationTOML{}, fmt.Sprintf("wake automation name %q does not match its directory %q", toml.Name, dirName)
	}
	return toml, ""
}

func evaluateWakeCandidate(toml AutomationTOML, params CoalesceParams, resumeAt time.Time, readDB DBReader, result CoalesceResult) (CoalesceResult, error) {
	if toml.Status != "ACTIVE" {
		result.Reason = fmt.Sprintf("wake automation status is %q want ACTIVE", toml.Status)
		return result, nil
	}
	if toml.ID != codexWakeKeyPrefix+toml.TargetThreadID {
		result.Reason = fmt.Sprintf("wake automation id %q does not bind to target_thread_id %q", toml.ID, toml.TargetThreadID)
		return result, nil
	}

	db, err := readDB(params.DBPath, toml.ID)
	if err != nil {
		if errors.Is(err, ErrRowNotFound) {
			result.Reason = "wake automation row not found in the scheduler database"
		} else {
			result.Reason = "wake schedule verification unavailable: " + err.Error()
		}
		return result, nil
	}
	if db.Status != "ACTIVE" {
		result.Reason = fmt.Sprintf("wake scheduler status is %q want ACTIVE", db.Status)
		return result, nil
	}
	if !db.HasNextRun {
		result.Reason = "wake next_run_at is NULL"
		return result, nil
	}
	wakeAt := time.UnixMilli(db.NextRunAt).UTC()
	if reason := validateRrule(toml.Rrule, wakeAt.Format(dtStartLayout)); reason != "" {
		result.Reason = "wake rrule does not match the one-shot next_run_at: " + reason
		return result, nil
	}
	if wakeAt.Before(resumeAt) {
		result.Reason = fmt.Sprintf(
			"wake next run %s is before the GLM resume time %s",
			wakeAt.Format(time.RFC3339), resumeAt.Format(time.RFC3339),
		)
		return result, nil
	}
	delay := wakeAt.Sub(resumeAt)
	if delay > maxCoalesceDelay {
		result.Reason = fmt.Sprintf(
			"wake next run %s delays the GLM resume by %s beyond the coalesce limit %s",
			wakeAt.Format(time.RFC3339), delay.Truncate(time.Second), maxCoalesceDelay,
		)
		return result, nil
	}

	result.Decision = DecisionCoalesce
	result.WakeAutomationID = toml.ID
	result.WakeThread = toml.TargetThreadID
	result.WakeNextRunUTC = wakeAt.Format(time.RFC3339)
	result.AddedWaitSeconds = int64(delay / time.Second)
	return result, nil
}
