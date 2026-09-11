package autoresume

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type codexWakeToolEnvelope struct {
	IsError *bool                  `json:"isError"`
	Content []codexWakeToolContent `json:"content"`
}

type codexWakeToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type codexWakeResponseFacts struct {
	AutomationID string
	Mode         string
	Status       string
	Message      string
}

var (
	codexWakeFailureMessage = regexp.MustCompile(`(?i)(^|[^a-z])(invalid|error|failed)([^a-z]|$)`)
	codexWakeSuccessMessage = regexp.MustCompile(`(?i)(^|[^a-z])(created|updated|saved|scheduled|success|successful|completed)([^a-z]|$)`)
)

func AdvanceCodexWakeTransaction(token string, rawResponse []byte, automationsDir, dbPath string, readDB DBReader) CodexWakeOutput {
	transaction, transactionID, err := decodeCodexWakeTransaction(token)
	if err != nil {
		return CodexWakeOutput{Version: codexWakeTransactionVersion, Status: CodexWakeStatusFailed, Reason: err.Error()}
	}
	facts, responseReason := parseCodexWakeToolResponse(rawResponse)
	if transaction.Stage == codexWakeStageCreate {
		return advanceCodexWakeCreate(transaction, transactionID, facts, responseReason)
	}
	return advanceCodexWakeUpdate(transaction, transactionID, facts, responseReason, automationsDir, dbPath, readDB)
}

func advanceCodexWakeCreate(transaction codexWakeTransaction, transactionID string, facts codexWakeResponseFacts, responseReason string) CodexWakeOutput {
	if responseReason == "" {
		responseReason = validateCodexWakeResponseFacts(facts, "create", codexWakePaused, transaction.ExpectedAutomationID)
	}
	if responseReason != "" {
		output := codexWakeFailureOutput(transaction, transactionID, responseReason)
		if facts.AutomationID != "" {
			output.Cleanup = codexWakeDeleteSpec(facts.AutomationID)
		}
		return output
	}
	transaction.Stage = codexWakeStageUpdate
	transaction.CreatedByTransaction = true
	transaction.Attempt = 1
	return codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction))
}

func advanceCodexWakeUpdate(transaction codexWakeTransaction, transactionID string, facts codexWakeResponseFacts, responseReason, automationsDir, dbPath string, readDB DBReader) CodexWakeOutput {
	if responseReason == "" {
		responseReason = validateCodexWakeResponseFacts(facts, "update", codexWakeActive, transaction.ExpectedAutomationID)
	}
	if responseReason != "" {
		return codexWakeUpdateFailure(transaction, transactionID, responseReason, true)
	}
	verification := Verify(Params{
		AutomationKey:    transaction.ExpectedAutomationID,
		ExpectedRFC3339:  transaction.WakeAtRFC3339,
		ExpectedThreadID: transaction.WakeThreadID,
		AutomationsDir:   automationsDir,
		DBPath:           dbPath,
	}, readDB)
	if verification.Outcome == Pass {
		return codexWakeVerifiedOutput(transaction, transactionID, verification)
	}
	if verification.Outcome == Unavailable {
		return codexWakeUpdateFailure(transaction, transactionID, "saved-state verification unavailable: "+verification.Reason, false)
	}
	return codexWakeUpdateFailure(transaction, transactionID, "saved-state verification failed: "+verification.Reason, true)
}

func codexWakeUpdateFailure(transaction codexWakeTransaction, transactionID, reason string, retryable bool) CodexWakeOutput {
	if retryable && transaction.Attempt < 2 {
		transaction.Attempt++
		output := codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction))
		output.Reason = reason
		return output
	}
	output := codexWakeFailureOutput(transaction, transactionID, reason)
	if transaction.CreatedByTransaction {
		output.Cleanup = codexWakeDeleteSpec(transaction.ExpectedAutomationID)
	} else if transaction.WakeInvocation {
		output.Cleanup = codexWakePauseSpec(transaction)
	}
	return output
}

func codexWakeFailureOutput(transaction codexWakeTransaction, transactionID, reason string) CodexWakeOutput {
	return CodexWakeOutput{
		Version:              codexWakeTransactionVersion,
		Status:               CodexWakeStatusFailed,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		WakeThreadID:         transaction.WakeThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		WakeAtRFC3339:        transaction.WakeAtRFC3339,
		Attempt:              transaction.Attempt,
		Reason:               reason,
	}
}

func codexWakeVerifiedOutput(transaction codexWakeTransaction, transactionID string, verification Result) CodexWakeOutput {
	return CodexWakeOutput{
		Version:              codexWakeTransactionVersion,
		Status:               CodexWakeStatusVerified,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		WakeThreadID:         transaction.WakeThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		WakeAtRFC3339:        transaction.WakeAtRFC3339,
		Attempt:              transaction.Attempt,
		Verification:         &verification,
	}
}

func parseCodexWakeToolResponse(raw []byte) (codexWakeResponseFacts, string) {
	var envelope codexWakeToolEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return codexWakeResponseFacts{}, "malformed automation tool response: " + err.Error()
	}
	if envelope.IsError == nil {
		return codexWakeResponseFacts{}, "automation tool response has no isError field"
	}
	facts, reason := codexWakeContentFacts(envelope.Content)
	if *envelope.IsError {
		if reason == "" {
			reason = "automation tool returned isError=true"
		}
		return facts, reason
	}
	if reason != "" {
		return facts, reason
	}
	return facts, ""
}

func codexWakeContentFacts(content []codexWakeToolContent) (codexWakeResponseFacts, string) {
	if len(content) != 1 || content[0].Type != "text" || strings.TrimSpace(content[0].Text) == "" {
		return codexWakeResponseFacts{}, "automation tool response must contain exactly one non-empty text payload"
	}
	text := strings.TrimSpace(content[0].Text)
	if strings.Contains(text, "Rendered suggestion") || strings.Contains(text, "suggested_create") {
		return codexWakeResponseFacts{}, "automation tool returned a suggestion instead of a persisted write"
	}
	var payload any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return codexWakeResponseFacts{}, "automation tool text payload is not machine JSON"
	}
	facts, err := collectCodexWakeResponseFacts(payload)
	if err != nil {
		return codexWakeResponseFacts{}, err.Error()
	}
	return facts, ""
}

func collectCodexWakeResponseFacts(payload any) (codexWakeResponseFacts, error) {
	fields := map[string]map[string]struct{}{
		"automation_id": {},
		"mode":          {},
		"status":        {},
		"message":       {},
	}
	collectCodexWakeFields(payload, fields)
	id, err := oneCodexWakeField(fields["automation_id"], "automation ID")
	if err != nil {
		return codexWakeResponseFacts{}, err
	}
	mode, err := oneCodexWakeField(fields["mode"], "mode")
	if err != nil {
		return codexWakeResponseFacts{}, err
	}
	status, err := oneCodexWakeField(fields["status"], "status")
	if err != nil {
		return codexWakeResponseFacts{}, err
	}
	message, err := oneCodexWakeField(fields["message"], "message")
	if err != nil {
		return codexWakeResponseFacts{}, err
	}
	return codexWakeResponseFacts{AutomationID: id, Mode: mode, Status: status, Message: message}, nil
}

func collectCodexWakeFields(value any, fields map[string]map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			canonical := codexWakeResponseFieldName(key)
			if canonical != "" {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					fields[canonical][strings.TrimSpace(text)] = struct{}{}
				}
			}
			collectCodexWakeFields(child, fields)
		}
	case []any:
		for _, child := range typed {
			collectCodexWakeFields(child, fields)
		}
	}
}

func codexWakeResponseFieldName(key string) string {
	switch key {
	case "automation_id", "automationId", "id":
		return "automation_id"
	case "mode":
		return "mode"
	case "status":
		return "status"
	case "message":
		return "message"
	default:
		return ""
	}
}

func oneCodexWakeField(values map[string]struct{}, name string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("automation tool response is missing %s", name)
	}
	if len(values) != 1 {
		return "", fmt.Errorf("automation tool response has ambiguous %s", name)
	}
	for value := range values {
		return value, nil
	}
	return "", nil
}

func validateCodexWakeResponseFacts(facts codexWakeResponseFacts, mode, status, automationID string) string {
	if facts.AutomationID != automationID {
		return fmt.Sprintf("automation ID mismatch: got %q want %q", facts.AutomationID, automationID)
	}
	if facts.Mode != mode {
		return fmt.Sprintf("automation mode mismatch: got %q want %q", facts.Mode, mode)
	}
	if facts.Status != status {
		return fmt.Sprintf("automation status mismatch: got %q want %q", facts.Status, status)
	}
	message := strings.TrimSpace(facts.Message)
	if message == "" || codexWakeFailureMessage.MatchString(message) {
		return "automation tool response has an explicit failure or empty message"
	}
	if !codexWakeSuccessMessage.MatchString(message) {
		return "automation tool response has no explicit success message"
	}
	return ""
}

func CodexWakePersistencePaths(codexConfigDir string) (string, string) {
	return filepath.Join(codexConfigDir, "automations"), filepath.Join(codexConfigDir, "sqlite", "codex-dev.db")
}
