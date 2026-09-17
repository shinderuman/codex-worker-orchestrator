package autoresume

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type AutoResumeFallbackPlan struct {
	TaskID               string
	RepoRoot             string
	ParentThreadID       string
	ExpectedAutomationID string
	ResetAtRFC3339       string
	ResumeAtRFC3339      string
	Stage                string
	Attempt              int
	CreatedByTransaction bool
}

type AutoResumeFallbackDecision string

const (
	AutoResumeFallbackLocalWait    AutoResumeFallbackDecision = "local_wait"
	AutoResumeFallbackExternalWake AutoResumeFallbackDecision = "external_wake"

	AutoResumeAbortAutomationUpdateUnavailable = "automation_update_unavailable"
)

func AutoResumeFallbackPlanFromToken(token string) (AutoResumeFallbackPlan, error) {
	transaction, _, err := decodeAutoResumeTransaction(token)
	if err != nil {
		return AutoResumeFallbackPlan{}, err
	}
	return autoResumeFallbackPlan(transaction), nil
}

func EvaluateAutoResumeFallback(token, automationsDir, dbPath string, readDB DBReader) (AutoResumeFallbackPlan, AutoResumeFallbackDecision, error) {
	transaction, _, err := decodeAutoResumeTransaction(token)
	if err != nil {
		return AutoResumeFallbackPlan{}, "", err
	}
	plan := autoResumeFallbackPlan(transaction)
	decision, err := evaluateAutoResumeFallbackPersistence(transaction, automationsDir, dbPath, readDB)
	if err != nil {
		return plan, "", err
	}
	return plan, decision, nil
}

func autoResumeFallbackPlan(transaction autoResumeTransaction) AutoResumeFallbackPlan {
	return AutoResumeFallbackPlan{
		TaskID:               transaction.TaskID,
		RepoRoot:             transaction.RepoRoot,
		ParentThreadID:       transaction.ParentThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		ResumeAtRFC3339:      transaction.ResumeAtRFC3339,
		Stage:                transaction.Stage,
		Attempt:              transaction.Attempt,
		CreatedByTransaction: transaction.CreatedByTransaction,
	}
}

func evaluateAutoResumeFallbackPersistence(transaction autoResumeTransaction, automationsDir, dbPath string, readDB DBReader) (AutoResumeFallbackDecision, error) {
	params := autoResumeVerificationParams(transaction, automationsDir, dbPath)
	verification := Verify(params, readDB)
	if verification.Outcome == Pass {
		return AutoResumeFallbackExternalWake, nil
	}

	tomlPath := filepath.Join(automationsDir, transaction.ExpectedAutomationID, "automation.toml")
	data, tomlReadErr := os.ReadFile(tomlPath)
	db, dbReadErr := readDB(dbPath, transaction.ExpectedAutomationID)
	tomlMissing := os.IsNotExist(tomlReadErr)
	dbMissing := errors.Is(dbReadErr, ErrRowNotFound)
	if tomlMissing && dbMissing {
		return AutoResumeFallbackLocalWait, nil
	}
	if tomlMissing != dbMissing {
		return "", fmt.Errorf("auto-resume fallback external persistence is partial at stage %s attempt %d", transaction.Stage, transaction.Attempt)
	}
	if tomlReadErr != nil {
		return "", fmt.Errorf("auto-resume fallback automation state is not safely readable: %w", tomlReadErr)
	}
	if dbReadErr != nil {
		return "", fmt.Errorf("auto-resume fallback scheduler state is not safely readable: %w", dbReadErr)
	}

	toml, err := parseAutomationTOML(data)
	if err != nil {
		return "", fmt.Errorf("auto-resume fallback automation state is malformed: %w", err)
	}
	if toml.ID != transaction.ExpectedAutomationID || toml.Name != transaction.ExpectedAutomationID || toml.TargetThreadID != transaction.ParentThreadID {
		return "", fmt.Errorf("auto-resume fallback automation identity does not match the transaction")
	}
	if toml.Prompt != buildAutoResumePrompt(transaction) {
		return "", fmt.Errorf("auto-resume fallback automation prompt does not match the transaction")
	}
	if toml.Status != pausedStatus || toml.Rrule != placeholderHourlyRRule {
		return "", fmt.Errorf("auto-resume fallback external wake is neither the exact ACTIVE one-shot nor the exact PAUSED placeholder: %s", verification.Reason)
	}
	if db.ID != transaction.ExpectedAutomationID || db.Status != pausedStatus || db.Rrule != placeholderHourlyRRule {
		return "", fmt.Errorf("auto-resume fallback scheduler state does not match the PAUSED placeholder")
	}
	return AutoResumeFallbackLocalWait, nil
}
