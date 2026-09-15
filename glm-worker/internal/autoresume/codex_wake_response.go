package autoresume

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type automationToolEnvelope struct {
	IsError *bool                   `json:"isError"`
	Content []automationToolContent `json:"content"`
}

type automationToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type automationResponseFacts struct {
	AutomationID string
	Mode         string
	Status       string
	Message      string
}

const automationIsErrorFallbackReason = "automation tool returned isError=true"

var (
	automationFailureMessage = regexp.MustCompile(`(?i)(^|[^a-z])(invalid|error|failed)([^a-z]|$)`)
	automationSuccessMessage = regexp.MustCompile(`(?i)(^|[^a-z])(created|updated|saved|scheduled|success|successful|completed)([^a-z]|$)`)
)

func AdvanceCodexWakeTransaction(token string, rawResponse []byte, automationsDir, dbPath string, readDB DBReader) CodexWakeOutput {
	transaction, transactionID, err := decodeCodexWakeTransaction(token)
	if err != nil {
		return CodexWakeOutput{Version: codexWakeTransactionVersion, Status: CodexWakeStatusFailed, Reason: err.Error()}
	}
	facts, responseReason := parseAutomationToolResponse(rawResponse)
	if transaction.Stage == stageCreatePlaceholder {
		return advanceCodexWakeCreate(transaction, transactionID, facts, responseReason)
	}
	return advanceCodexWakeUpdate(transaction, transactionID, facts, responseReason, automationsDir, dbPath, readDB)
}

func advanceCodexWakeCreate(transaction codexWakeTransaction, transactionID string, facts automationResponseFacts, responseReason string) CodexWakeOutput {
	if responseReason == "" {
		responseReason = validateAutomationResponseFacts(facts, "create", pausedStatus, transaction.ExpectedAutomationID)
	}
	if responseReason != "" {
		output := codexWakeFailureOutput(transaction, transactionID, responseReason)
		if facts.AutomationID != "" {
			output.Cleanup = codexWakeDeleteSpec(facts.AutomationID)
		}
		return output
	}
	transaction.Stage = stageUpdateOneShot
	transaction.CreatedByTransaction = true
	transaction.Attempt = 1
	return codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction))
}

func advanceCodexWakeUpdate(transaction codexWakeTransaction, transactionID string, facts automationResponseFacts, responseReason, automationsDir, dbPath string, readDB DBReader) CodexWakeOutput {
	return advanceUpdateTransaction(
		transaction, transactionID, facts, responseReason,
		codexWakeVerificationParams(transaction, automationsDir, dbPath),
		readDB,
		codexWakeUpdateFailure,
		codexWakeVerifiedOutput,
	)
}

func codexWakeVerificationParams(transaction codexWakeTransaction, automationsDir, dbPath string) Params {
	return Params{
		AutomationKey:    transaction.ExpectedAutomationID,
		ExpectedRFC3339:  transaction.WakeAtRFC3339,
		ExpectedThreadID: transaction.WakeThreadID,
		AutomationsDir:   automationsDir,
		DBPath:           dbPath,
	}
}

func advanceUpdateTransaction[TTransaction any, TOutput any](
	transaction TTransaction,
	transactionID string,
	facts automationResponseFacts,
	responseReason string,
	verifyParams Params,
	readDB DBReader,
	writeFailure func(TTransaction, string, string, bool) TOutput,
	verifiedOutput func(TTransaction, string, Result) TOutput,
) TOutput {
	if responseReason == "" {
		responseReason = validateAutomationResponseFacts(facts, "update", activeStatus, verifyParams.AutomationKey)
	}
	if responseReason != "" {
		return writeFailure(transaction, transactionID, responseReason, true)
	}
	verification := Verify(verifyParams, readDB)
	if verification.Outcome == Pass {
		return verifiedOutput(transaction, transactionID, verification)
	}
	if verification.Outcome == Unavailable {
		return writeFailure(transaction, transactionID, "saved-state verification unavailable: "+verification.Reason, false)
	}
	return writeFailure(transaction, transactionID, "saved-state verification failed: "+verification.Reason, true)
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

func parseAutomationToolResponse(raw []byte) (automationResponseFacts, string) {
	var envelope automationToolEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return automationResponseFacts{}, "malformed automation tool response: " + err.Error()
	}
	if envelope.IsError == nil {
		return automationResponseFacts{}, "automation tool response has no isError field"
	}
	facts, reason := automationContentFacts(envelope.Content)
	if *envelope.IsError {
		if reason == "" {
			reason = automationIsErrorFallbackReason
		}
		return facts, reason
	}
	if reason != "" {
		return facts, reason
	}
	return facts, ""
}

func automationContentFacts(content []automationToolContent) (automationResponseFacts, string) {
	texts, reason := automationTextPayloads(content)
	if reason != "" {
		return automationResponseFacts{}, reason
	}
	jsonBlocks := make([]string, 0, len(texts))
	proseBlocks := make([]string, 0, len(texts))
	for _, text := range texts {
		if json.Valid([]byte(text)) {
			jsonBlocks = append(jsonBlocks, text)
			continue
		}
		proseBlocks = append(proseBlocks, text)
	}
	if len(jsonBlocks) == 0 {
		return automationResponseFacts{}, "automation tool text payload is not machine JSON"
	}
	if len(jsonBlocks) > 1 {
		return automationResponseFacts{}, "automation tool response has ambiguous machine JSON payloads"
	}
	facts, err := collectAutomationResponseFacts(jsonBlocks[0])
	if err != nil {
		return automationResponseFacts{}, err.Error()
	}
	if len(proseBlocks) > 1 {
		return automationResponseFacts{}, "automation tool response has ambiguous non-JSON payloads"
	}
	if facts.Message == "" {
		if len(proseBlocks) != 1 {
			return automationResponseFacts{}, "automation tool response is missing an unambiguous success or failure message"
		}
		facts.Message = proseBlocks[0]
	}
	if len(proseBlocks) == 1 && facts.Message != proseBlocks[0] {
		return automationResponseFacts{}, "automation tool response has extra non-JSON payloads beside the machine message"
	}
	return facts, ""
}

func automationTextPayloads(content []automationToolContent) ([]string, string) {
	if len(content) == 0 {
		return nil, "automation tool response must contain non-empty text payloads"
	}
	texts := make([]string, 0, len(content))
	for _, block := range content {
		if block.Type != "text" || strings.TrimSpace(block.Text) == "" {
			return nil, "automation tool response must contain non-empty text payloads"
		}
		text := strings.TrimSpace(block.Text)
		if strings.Contains(text, "Rendered suggestion") || strings.Contains(text, "suggested_create") {
			return nil, "automation tool returned a suggestion instead of a persisted write"
		}
		texts = append(texts, text)
	}
	return texts, ""
}

func collectAutomationResponseFacts(payloadText string) (automationResponseFacts, error) {
	var payload any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return automationResponseFacts{}, fmt.Errorf("automation tool text payload is not machine JSON")
	}
	fields := map[string]map[string]struct{}{
		"automation_id": {},
		"mode":          {},
		"status":        {},
		"message":       {},
	}
	collectAutomationFields(payload, fields)
	id, err := oneAutomationField(fields["automation_id"], "automation ID")
	if err != nil {
		return automationResponseFacts{}, err
	}
	mode, err := oneAutomationField(fields["mode"], "mode")
	if err != nil {
		return automationResponseFacts{}, err
	}
	status, err := oneAutomationField(fields["status"], "status")
	if err != nil {
		return automationResponseFacts{}, err
	}
	message, err := optionalAutomationField(fields["message"], "message")
	if err != nil {
		return automationResponseFacts{}, err
	}
	return automationResponseFacts{AutomationID: id, Mode: mode, Status: status, Message: message}, nil
}

func optionalAutomationField(values map[string]struct{}, name string) (string, error) {
	if len(values) > 1 {
		return "", fmt.Errorf("automation tool response has ambiguous %s", name)
	}
	for value := range values {
		return value, nil
	}
	return "", nil
}

func collectAutomationFields(value any, fields map[string]map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			canonical := automationResponseFieldName(key)
			if canonical != "" {
				if text, ok := child.(string); ok && strings.TrimSpace(text) != "" {
					fields[canonical][strings.TrimSpace(text)] = struct{}{}
				}
			}
			collectAutomationFields(child, fields)
		}
	case []any:
		for _, child := range typed {
			collectAutomationFields(child, fields)
		}
	}
}

func automationResponseFieldName(key string) string {
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

func oneAutomationField(values map[string]struct{}, name string) (string, error) {
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

func validateAutomationResponseFacts(facts automationResponseFacts, mode, status, automationID string) string {
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
	if message == "" || automationFailureMessage.MatchString(message) {
		return "automation tool response has an explicit failure or empty message"
	}
	if !automationSuccessMessage.MatchString(message) {
		return "automation tool response has no explicit success message"
	}
	return ""
}

func CodexWakePersistencePaths(codexConfigDir string) (string, string) {
	return filepath.Join(codexConfigDir, "automations"), filepath.Join(codexConfigDir, "sqlite", "codex-dev.db")
}
