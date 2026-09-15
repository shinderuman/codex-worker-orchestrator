package autoresume

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type AutoResumeWriteSpec struct {
	Boundary       string `json:"boundary"`
	Tool           string `json:"tool"`
	Mode           string `json:"mode"`
	AutomationID   string `json:"automation_id,omitempty"`
	Name           string `json:"name,omitempty"`
	TargetThreadID string `json:"target_thread_id,omitempty"`
	Status         string `json:"status,omitempty"`
	RRule          string `json:"rrule,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
}

type AutoResumeAuthority struct {
	Permission   string `json:"permission"`
	Continuation string `json:"continuation"`
	GLMScope     string `json:"glm_scope"`
}

type AutoResumeOutput struct {
	Version              int                  `json:"version"`
	Status               string               `json:"status"`
	Stage                string               `json:"stage,omitempty"`
	TransactionID        string               `json:"transaction_id,omitempty"`
	TaskID               string               `json:"task_id,omitempty"`
	RepoRoot             string               `json:"repo_root,omitempty"`
	ParentThreadID       string               `json:"parent_thread_id,omitempty"`
	ExpectedAutomationID string               `json:"expected_automation_id,omitempty"`
	ResetAtRFC3339       string               `json:"reset_at_rfc3339,omitempty"`
	ResumeAtRFC3339      string               `json:"resume_at_rfc3339,omitempty"`
	Coalesce             *CoalesceResult      `json:"coalesce,omitempty"`
	Attempt              int                  `json:"attempt,omitempty"`
	Authority            *AutoResumeAuthority `json:"authority,omitempty"`
	VerifyCommand        []string             `json:"verify_command,omitempty"`
	Write                *AutoResumeWriteSpec `json:"write,omitempty"`
	Cleanup              *AutoResumeWriteSpec `json:"cleanup,omitempty"`
	Token                string               `json:"token,omitempty"`
	Reason               string               `json:"reason,omitempty"`
	Verification         *Result              `json:"verification,omitempty"`
}

type AutoResumePlanParams struct {
	TaskID          string
	RepoRoot        string
	ParentThreadID  string
	AutomationKey   string
	ResetAtRFC3339  string
	ResumeAtRFC3339 string
	RunControl      string
	AutomationsDir  string
	DBPath          string
	Now             time.Time
}

type autoResumeTransaction struct {
	Version              int    `json:"version"`
	Stage                string `json:"stage"`
	TaskID               string `json:"task_id"`
	RepoRoot             string `json:"repo_root"`
	ParentThreadID       string `json:"parent_thread_id"`
	ExpectedAutomationID string `json:"expected_automation_id"`
	ResetAtRFC3339       string `json:"reset_at_rfc3339"`
	ResumeAtRFC3339      string `json:"resume_at_rfc3339"`
	RunControl           string `json:"run_control,omitempty"`
	CoalesceDecision     string `json:"coalesce_decision"`
	Nonce                string `json:"nonce"`
	CreatedByTransaction bool   `json:"created_by_transaction"`
	Attempt              int    `json:"attempt"`
}

const (
	autoResumeTransactionVersion = 1

	AutoResumeStatusWriteRequired = "write_required"
	AutoResumeStatusVerified      = "verified"
	AutoResumeStatusFailed        = "failed"
	AutoResumeStatusCoalesced     = "coalesced"

	autoResumePromptTrigger = "GLM 5h auto-resume trigger."

	autoResumeAuthorityPermission   = `IMPLEMENTATION_RULES.md "repository automation authority" permanently authorizes creating, updating, and deleting resume automations for this repository without per-automation confirmation`
	autoResumeAuthorityContinuation = "this trigger resumes the already-authorized current task in the same checkout, task, and session"
	autoResumeAuthorityGLMScope     = "GLM workers are prohibited from Git remote write only, not from GLM execution in general"

	autoResumeRefusalReasonLimit    = 256
	autoResumeRefusalTruncationMark = "...[truncated]"
)

var autoResumeKeyPattern = regexp.MustCompile(`^glm-worker-resume-[A-Za-z0-9_-]+$`)

func BuildAutoResumeTransaction(params AutoResumePlanParams, readDB DBReader) (AutoResumeOutput, error) {
	if err := validateAutoResumePlanParams(params); err != nil {
		return AutoResumeOutput{}, err
	}
	coalesce, err := CheckCoalesce(CoalesceParams{
		ParentThreadID:  params.ParentThreadID,
		ResumeAtRFC3339: params.ResumeAtRFC3339,
		AutomationsDir:  params.AutomationsDir,
		DBPath:          params.DBPath,
	}, readDB)
	if err != nil {
		return AutoResumeOutput{}, err
	}
	output := autoResumeIdentityOutput(params, &coalesce)
	if coalesce.Decision == DecisionCoalesce {
		output.Status = AutoResumeStatusCoalesced
		return output, nil
	}
	nonce, err := newTransactionNonce()
	if err != nil {
		return AutoResumeOutput{}, err
	}
	transaction := autoResumeTransaction{
		Version:              autoResumeTransactionVersion,
		TaskID:               params.TaskID,
		RepoRoot:             params.RepoRoot,
		ParentThreadID:       params.ParentThreadID,
		ExpectedAutomationID: params.AutomationKey,
		ResetAtRFC3339:       params.ResetAtRFC3339,
		ResumeAtRFC3339:      params.ResumeAtRFC3339,
		RunControl:           params.RunControl,
		CoalesceDecision:     coalesce.Decision,
		Nonce:                nonce,
		Attempt:              1,
	}
	found, err := resolveAutoResumeAutomation(params.AutomationsDir, params.AutomationKey, params.ParentThreadID)
	if err != nil {
		return AutoResumeOutput{}, err
	}
	output, err = autoResumePlannedWrite(transaction, coalesce, found)
	if err != nil {
		return AutoResumeOutput{}, err
	}
	return output, nil
}

func autoResumePlannedWrite(transaction autoResumeTransaction, coalesce CoalesceResult, existing bool) (AutoResumeOutput, error) {
	if existing {
		transaction.Stage = stageUpdateOneShot
	} else {
		transaction.Stage = stageCreatePlaceholder
	}
	output, err := autoResumeWriteOutput(transaction, autoResumeSpecForStage(transaction))
	if err != nil {
		return AutoResumeOutput{}, err
	}
	output.Coalesce = &coalesce
	return output, nil
}

func autoResumeSpecForStage(transaction autoResumeTransaction) AutoResumeWriteSpec {
	if transaction.Stage == stageCreatePlaceholder {
		return autoResumeCreateSpec(transaction)
	}
	return autoResumeUpdateSpec(transaction)
}

func AdvanceAutoResumeTransaction(token string, rawResponse []byte, automationsDir, dbPath string, readDB DBReader) AutoResumeOutput {
	transaction, transactionID, err := decodeAutoResumeTransaction(token)
	if err != nil {
		authority := autoResumeAuthoritySpec()
		return AutoResumeOutput{Version: autoResumeTransactionVersion, Status: AutoResumeStatusFailed, Authority: &authority, Reason: err.Error()}
	}
	facts, responseReason := parseAutomationToolResponse(rawResponse)
	responseReason = autoResumeRefusalReason(responseReason, facts)
	if transaction.Stage == stageCreatePlaceholder {
		return advanceAutoResumeCreate(transaction, transactionID, facts, responseReason)
	}
	return advanceAutoResumeUpdate(transaction, transactionID, facts, responseReason, automationsDir, dbPath, readDB)
}

func AutoResumeTransactionContext(token string) (string, error) {
	transaction, _, err := decodeAutoResumeTransaction(token)
	if err != nil {
		return "", err
	}
	return transaction.ParentThreadID, nil
}

func validateAutoResumePlanParams(params AutoResumePlanParams) error {
	if !uuidPattern.MatchString(params.ParentThreadID) {
		return fmt.Errorf("invalid parent thread ID: %q", params.ParentThreadID)
	}
	if params.TaskID == "" || params.RepoRoot == "" {
		return fmt.Errorf("rate-limited task evidence is incomplete")
	}
	if !autoResumeKeyPattern.MatchString(params.AutomationKey) {
		return fmt.Errorf("invalid auto-resume automation key: %q", params.AutomationKey)
	}
	resetAt, err := time.Parse(time.RFC3339, params.ResetAtRFC3339)
	if err != nil {
		return fmt.Errorf("invalid rate-limit reset time: %w", err)
	}
	resumeAt, err := time.Parse(time.RFC3339, params.ResumeAtRFC3339)
	if err != nil {
		return fmt.Errorf("invalid auto-resume time: %w", err)
	}
	if !resetAt.After(params.Now.UTC()) {
		return fmt.Errorf("rate-limit reset is not in the future: %s", resetAt.Format(time.RFC3339))
	}
	if !resumeAt.After(resetAt) {
		return fmt.Errorf("auto-resume time %s is not after the reset time %s", resumeAt.Format(time.RFC3339), resetAt.Format(time.RFC3339))
	}
	return nil
}

func autoResumeIdentityOutput(params AutoResumePlanParams, coalesce *CoalesceResult) AutoResumeOutput {
	return AutoResumeOutput{
		Version:              autoResumeTransactionVersion,
		TaskID:               params.TaskID,
		RepoRoot:             params.RepoRoot,
		ParentThreadID:       params.ParentThreadID,
		ExpectedAutomationID: params.AutomationKey,
		ResetAtRFC3339:       params.ResetAtRFC3339,
		ResumeAtRFC3339:      params.ResumeAtRFC3339,
		Coalesce:             coalesce,
	}
}

func resolveAutoResumeAutomation(dir, key, parentThreadID string) (bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, key, "automation.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read existing resume automation: %w", err)
	}
	toml, err := parseAutomationTOML(data)
	if err != nil {
		return false, fmt.Errorf("existing resume automation is not safely readable: %w", err)
	}
	if toml.ID != key || toml.Name != key {
		return false, fmt.Errorf("existing resume automation identity mismatch: id=%q name=%q want %q", toml.ID, toml.Name, key)
	}
	if toml.TargetThreadID != parentThreadID {
		return false, fmt.Errorf("existing resume automation targets thread %q want the current parent %q", toml.TargetThreadID, parentThreadID)
	}
	return true, nil
}

func autoResumeAuthoritySpec() AutoResumeAuthority {
	return AutoResumeAuthority{
		Permission:   autoResumeAuthorityPermission,
		Continuation: autoResumeAuthorityContinuation,
		GLMScope:     autoResumeAuthorityGLMScope,
	}
}

func buildAutoResumePrompt(transaction autoResumeTransaction) string {
	lines := []string{
		autoResumePromptTrigger + " repo_root=" + transaction.RepoRoot + "; expected_task_id=" + transaction.TaskID + ".",
		"authority: " + strings.Join([]string{autoResumeAuthorityPermission, autoResumeAuthorityContinuation, autoResumeAuthorityGLMScope}, "; ") + ".",
	}
	if transaction.RunControl != "" {
		lines = append(lines, "run_control="+transaction.RunControl)
	}
	return strings.Join(lines, "\n")
}

func validateAutoResumeAuthority(authority AutoResumeAuthority) error {
	if authority.Permission != autoResumeAuthorityPermission {
		return fmt.Errorf("authority permission text is missing or altered")
	}
	if authority.Continuation != autoResumeAuthorityContinuation {
		return fmt.Errorf("authority continuation text is missing or altered")
	}
	if authority.GLMScope != autoResumeAuthorityGLMScope {
		return fmt.Errorf("authority GLM scope is missing, altered, or confuses the Git-remote-write-only prohibition with a general GLM prohibition")
	}
	return nil
}

func autoResumeCreateSpec(transaction autoResumeTransaction) AutoResumeWriteSpec {
	return AutoResumeWriteSpec{
		Boundary:       externalAutomationBoundary,
		Tool:           automationUpdateTool,
		Mode:           "create",
		Name:           transaction.ExpectedAutomationID,
		TargetThreadID: transaction.ParentThreadID,
		Status:         pausedStatus,
		RRule:          placeholderHourlyRRule,
		Prompt:         buildAutoResumePrompt(transaction),
	}
}

func autoResumeUpdateSpec(transaction autoResumeTransaction) AutoResumeWriteSpec {
	resumeAt, _ := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
	return AutoResumeWriteSpec{
		Boundary:       externalAutomationBoundary,
		Tool:           automationUpdateTool,
		Mode:           "update",
		AutomationID:   transaction.ExpectedAutomationID,
		TargetThreadID: transaction.ParentThreadID,
		Status:         activeStatus,
		RRule:          "DTSTART:" + resumeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1",
		Prompt:         buildAutoResumePrompt(transaction),
	}
}

func autoResumeDeleteSpec(automationID string) *AutoResumeWriteSpec {
	if !keyPattern.MatchString(automationID) {
		return nil
	}
	return &AutoResumeWriteSpec{Boundary: externalAutomationBoundary, Tool: automationUpdateTool, Mode: "delete", AutomationID: automationID}
}

func autoResumeVerifyCommand(transaction autoResumeTransaction) []string {
	return []string{"glm-worker", "--verify-auto-resume", transaction.ExpectedAutomationID, transaction.ResumeAtRFC3339}
}

func validateAutoResumeWriteSpec(write AutoResumeWriteSpec, transaction autoResumeTransaction) error {
	if err := validateAutoResumeWriteIdentity(write, transaction); err != nil {
		return err
	}
	resumeAt, err := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
	if err != nil {
		return fmt.Errorf("transaction resume time is invalid: %w", err)
	}
	switch write.Mode {
	case "create":
		return validateAutoResumeCreateWrite(write, transaction)
	case "update":
		return validateAutoResumeUpdateWrite(write, transaction, resumeAt)
	default:
		return fmt.Errorf("write spec mode %q is not a reservation mutation", write.Mode)
	}
}

func validateAutoResumeWriteIdentity(write AutoResumeWriteSpec, transaction autoResumeTransaction) error {
	if write.Boundary != externalAutomationBoundary || write.Tool != automationUpdateTool {
		return fmt.Errorf("write spec does not use the external automation boundary and tool")
	}
	if write.TargetThreadID != transaction.ParentThreadID {
		return fmt.Errorf("write spec targets thread %q want the current parent %q", write.TargetThreadID, transaction.ParentThreadID)
	}
	if write.Prompt == "" || write.Prompt != buildAutoResumePrompt(transaction) {
		return fmt.Errorf("write spec prompt is missing or does not carry the machine authority contract")
	}
	return nil
}

func validateAutoResumeCreateWrite(write AutoResumeWriteSpec, transaction autoResumeTransaction) error {
	if write.Name != transaction.ExpectedAutomationID || write.AutomationID != "" {
		return fmt.Errorf("create spec identity does not match the expected automation %q", transaction.ExpectedAutomationID)
	}
	if write.Status != pausedStatus || write.RRule != placeholderHourlyRRule {
		return fmt.Errorf("create spec is not the PAUSED hourly placeholder")
	}
	return nil
}

func validateAutoResumeUpdateWrite(write AutoResumeWriteSpec, transaction autoResumeTransaction, resumeAt time.Time) error {
	if write.AutomationID != transaction.ExpectedAutomationID || write.Name != "" {
		return fmt.Errorf("update spec identity does not match the expected automation %q", transaction.ExpectedAutomationID)
	}
	if write.Status != activeStatus {
		return fmt.Errorf("update spec status is %q want ACTIVE", write.Status)
	}
	if reason := validateRrule(write.RRule, resumeAt.UTC().Format(dtStartLayout)); reason != "" {
		return fmt.Errorf("update spec rrule is not the UTC one-shot anchor: %s", reason)
	}
	return nil
}

func autoResumeWriteOutput(transaction autoResumeTransaction, write AutoResumeWriteSpec) (AutoResumeOutput, error) {
	if err := validateAutoResumeWriteSpec(write, transaction); err != nil {
		return AutoResumeOutput{}, err
	}
	authority := autoResumeAuthoritySpec()
	if err := validateAutoResumeAuthority(authority); err != nil {
		return AutoResumeOutput{}, err
	}
	token, transactionID := encodeSignedTransaction(transaction)
	return AutoResumeOutput{
		Version:              autoResumeTransactionVersion,
		Status:               AutoResumeStatusWriteRequired,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		TaskID:               transaction.TaskID,
		RepoRoot:             transaction.RepoRoot,
		ParentThreadID:       transaction.ParentThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		ResumeAtRFC3339:      transaction.ResumeAtRFC3339,
		Attempt:              transaction.Attempt,
		Authority:            &authority,
		VerifyCommand:        autoResumeVerifyCommand(transaction),
		Write:                &write,
		Token:                token,
	}, nil
}

func advanceAutoResumeCreate(transaction autoResumeTransaction, transactionID string, facts automationResponseFacts, responseReason string) AutoResumeOutput {
	if responseReason == "" {
		responseReason = validateAutomationResponseFacts(facts, "create", pausedStatus, transaction.ExpectedAutomationID)
	}
	if responseReason != "" {
		output := autoResumeFailureOutput(transaction, transactionID, responseReason)
		if facts.AutomationID != "" {
			output.Cleanup = autoResumeDeleteSpec(facts.AutomationID)
		}
		return output
	}
	transaction.Stage = stageUpdateOneShot
	transaction.CreatedByTransaction = true
	transaction.Attempt = 1
	output, err := autoResumeWriteOutput(transaction, autoResumeUpdateSpec(transaction))
	if err != nil {
		return autoResumeFailureOutput(transaction, transactionID, err.Error())
	}
	return output
}

func advanceAutoResumeUpdate(transaction autoResumeTransaction, transactionID string, facts automationResponseFacts, responseReason, automationsDir, dbPath string, readDB DBReader) AutoResumeOutput {
	return advanceUpdateTransaction(
		transaction, transactionID, facts, responseReason,
		autoResumeVerificationParams(transaction, automationsDir, dbPath),
		readDB,
		autoResumeUpdateFailure,
		autoResumeVerifiedOutput,
	)
}

func autoResumeVerificationParams(transaction autoResumeTransaction, automationsDir, dbPath string) Params {
	return Params{
		AutomationKey:    transaction.ExpectedAutomationID,
		ExpectedRFC3339:  transaction.ResumeAtRFC3339,
		ExpectedThreadID: transaction.ParentThreadID,
		AutomationsDir:   automationsDir,
		DBPath:           dbPath,
	}
}

func autoResumeUpdateFailure(transaction autoResumeTransaction, transactionID, reason string, retryable bool) AutoResumeOutput {
	if retryable && transaction.Attempt < 2 {
		transaction.Attempt++
		output, err := autoResumeWriteOutput(transaction, autoResumeUpdateSpec(transaction))
		if err != nil {
			return autoResumeFailureOutput(transaction, transactionID, err.Error())
		}
		output.Reason = reason
		return output
	}
	output := autoResumeFailureOutput(transaction, transactionID, reason)
	if transaction.CreatedByTransaction {
		output.Cleanup = autoResumeDeleteSpec(transaction.ExpectedAutomationID)
	}
	return output
}

func autoResumeFailureOutput(transaction autoResumeTransaction, transactionID, reason string) AutoResumeOutput {
	authority := autoResumeAuthoritySpec()
	return AutoResumeOutput{
		Version:              autoResumeTransactionVersion,
		Status:               AutoResumeStatusFailed,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		TaskID:               transaction.TaskID,
		RepoRoot:             transaction.RepoRoot,
		ParentThreadID:       transaction.ParentThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		ResumeAtRFC3339:      transaction.ResumeAtRFC3339,
		Attempt:              transaction.Attempt,
		Authority:            &authority,
		Reason:               reason,
	}
}

func autoResumeRefusalReason(responseReason string, facts automationResponseFacts) string {
	if responseReason != automationIsErrorFallbackReason || facts.Message == "" {
		return responseReason
	}
	return responseReason + ": " + boundedAutoResumeRefusal(facts.Message)
}

func boundedAutoResumeRefusal(message string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(message))
	runes := []rune(cleaned)
	if len(runes) <= autoResumeRefusalReasonLimit {
		return string(runes)
	}
	return string(runes[:autoResumeRefusalReasonLimit]) + autoResumeRefusalTruncationMark
}

func autoResumeVerifiedOutput(transaction autoResumeTransaction, transactionID string, verification Result) AutoResumeOutput {
	return AutoResumeOutput{
		Version:              autoResumeTransactionVersion,
		Status:               AutoResumeStatusVerified,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		TaskID:               transaction.TaskID,
		RepoRoot:             transaction.RepoRoot,
		ParentThreadID:       transaction.ParentThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		ResumeAtRFC3339:      transaction.ResumeAtRFC3339,
		Attempt:              transaction.Attempt,
		VerifyCommand:        autoResumeVerifyCommand(transaction),
		Verification:         &verification,
	}
}

func decodeAutoResumeTransaction(token string) (autoResumeTransaction, string, error) {
	payload, digest, err := decodeSignedTransaction(token)
	if err != nil {
		return autoResumeTransaction{}, "", err
	}
	var transaction autoResumeTransaction
	if err := json.Unmarshal(payload, &transaction); err != nil {
		return autoResumeTransaction{}, "", fmt.Errorf("decode transaction payload: %w", err)
	}
	if err := validateAutoResumeTransaction(transaction); err != nil {
		return autoResumeTransaction{}, "", err
	}
	return transaction, digest, nil
}

func validateAutoResumeTransaction(transaction autoResumeTransaction) error {
	if err := validateAutoResumeTransactionIdentity(transaction); err != nil {
		return err
	}
	return validateAutoResumeTransactionSchedule(transaction)
}

func validateAutoResumeTransactionIdentity(transaction autoResumeTransaction) error {
	if transaction.Version != autoResumeTransactionVersion || !uuidPattern.MatchString(transaction.ParentThreadID) {
		return fmt.Errorf("invalid transaction identity")
	}
	if !autoResumeKeyPattern.MatchString(transaction.ExpectedAutomationID) {
		return fmt.Errorf("transaction automation identity mismatch")
	}
	if transaction.TaskID == "" || transaction.RepoRoot == "" {
		return fmt.Errorf("transaction task evidence is incomplete")
	}
	if !transactionNoncePattern.MatchString(transaction.Nonce) {
		return fmt.Errorf("invalid transaction nonce")
	}
	if transaction.Stage != stageCreatePlaceholder && transaction.Stage != stageUpdateOneShot {
		return fmt.Errorf("invalid transaction stage %q", transaction.Stage)
	}
	if transaction.CoalesceDecision != DecisionCreateGLMWake {
		return fmt.Errorf("transaction coalesce decision %q did not admit a reservation", transaction.CoalesceDecision)
	}
	if transaction.Attempt < 1 || transaction.Attempt > 2 {
		return fmt.Errorf("invalid transaction attempt %d", transaction.Attempt)
	}
	return nil
}

func validateAutoResumeTransactionSchedule(transaction autoResumeTransaction) error {
	resetAt, err := time.Parse(time.RFC3339, transaction.ResetAtRFC3339)
	if err != nil {
		return fmt.Errorf("invalid transaction reset time: %w", err)
	}
	resumeAt, err := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
	if err != nil {
		return fmt.Errorf("invalid transaction resume time: %w", err)
	}
	if !resumeAt.After(resetAt) {
		return fmt.Errorf("transaction resume time is not after the reset time")
	}
	return nil
}
