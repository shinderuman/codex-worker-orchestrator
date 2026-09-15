package autoresume

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
)

type CodexWakeWriteSpec struct {
	Boundary       string `json:"boundary"`
	Tool           string `json:"tool"`
	Mode           string `json:"mode"`
	AutomationID   string `json:"automation_id,omitempty"`
	Name           string `json:"name,omitempty"`
	TargetThreadID string `json:"target_thread_id,omitempty"`
	Status         string `json:"status,omitempty"`
	RRule          string `json:"rrule,omitempty"`
}

type CodexWakeOutput struct {
	Version              int                 `json:"version"`
	Status               string              `json:"status"`
	Stage                string              `json:"stage,omitempty"`
	TransactionID        string              `json:"transaction_id,omitempty"`
	WakeThreadID         string              `json:"wake_thread_id,omitempty"`
	ExpectedAutomationID string              `json:"expected_automation_id,omitempty"`
	ResetAtRFC3339       string              `json:"reset_at_rfc3339,omitempty"`
	WakeAtRFC3339        string              `json:"wake_at_rfc3339,omitempty"`
	Attempt              int                 `json:"attempt,omitempty"`
	Write                *CodexWakeWriteSpec `json:"write,omitempty"`
	Cleanup              *CodexWakeWriteSpec `json:"cleanup,omitempty"`
	Token                string              `json:"token,omitempty"`
	Reason               string              `json:"reason,omitempty"`
	Verification         *Result             `json:"verification,omitempty"`
}

type codexWakeTransaction struct {
	Version              int    `json:"version"`
	Stage                string `json:"stage"`
	WakeThreadID         string `json:"wake_thread_id"`
	ExpectedAutomationID string `json:"expected_automation_id"`
	ResetAtRFC3339       string `json:"reset_at_rfc3339"`
	WakeAtRFC3339        string `json:"wake_at_rfc3339"`
	Nonce                string `json:"nonce"`
	CreatedByTransaction bool   `json:"created_by_transaction"`
	WakeInvocation       bool   `json:"wake_invocation"`
	Attempt              int    `json:"attempt"`
}

const (
	codexWakeTransactionVersion = 1
	codexWakeSafetyMargin       = 2 * time.Minute
	transactionNonceBytes       = 16

	CodexWakeStatusWriteRequired = "write_required"
	CodexWakeStatusVerified      = "verified"
	CodexWakeStatusFailed        = "failed"

	stageCreatePlaceholder = "create_placeholder"
	stageUpdateOneShot     = "update_one_shot"

	externalAutomationBoundary = "external-unenforceable"
	automationUpdateTool       = "automation_update"
	pausedStatus               = "PAUSED"
	placeholderHourlyRRule     = "RRULE:FREQ=HOURLY"
)

var (
	uuidPattern             = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	transactionNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func BuildCodexWakeTransaction(snapshot codexlimit.Snapshot, wakeThreadID, firedAutomationID, automationsDir string, now time.Time) (CodexWakeOutput, error) {
	if !uuidPattern.MatchString(wakeThreadID) {
		return CodexWakeOutput{}, fmt.Errorf("invalid wake thread ID: %q", wakeThreadID)
	}
	resetAt, wakeAt, err := codexWakeTimes(snapshot, now)
	if err != nil {
		return CodexWakeOutput{}, err
	}
	nonce, err := newTransactionNonce()
	if err != nil {
		return CodexWakeOutput{}, err
	}
	expectedID := CodexWakeAutomationKey(wakeThreadID)
	transaction := codexWakeTransaction{
		Version:              codexWakeTransactionVersion,
		Stage:                stageUpdateOneShot,
		WakeThreadID:         wakeThreadID,
		ExpectedAutomationID: expectedID,
		ResetAtRFC3339:       resetAt.Format(time.RFC3339),
		WakeAtRFC3339:        wakeAt.Format(time.RFC3339),
		Nonce:                nonce,
		Attempt:              1,
	}
	if firedAutomationID != "" {
		if firedAutomationID != expectedID {
			return CodexWakeOutput{}, fmt.Errorf("fired automation ID mismatch: got %q want %q", firedAutomationID, expectedID)
		}
		transaction.WakeInvocation = true
		return codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction)), nil
	}

	existing, found, err := resolveCodexWakeAutomation(automationsDir, wakeThreadID)
	if err != nil {
		return CodexWakeOutput{}, err
	}
	if found {
		if existing != expectedID {
			return CodexWakeOutput{}, fmt.Errorf("wake automation ID mismatch: got %q want %q", existing, expectedID)
		}
		return codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction)), nil
	}
	transaction.Stage = stageCreatePlaceholder
	return codexWakeWriteOutput(transaction, codexWakeCreateSpec(transaction)), nil
}

func newTransactionNonce() (string, error) {
	value := make([]byte, transactionNonceBytes)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate transaction nonce: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func codexWakeTimes(snapshot codexlimit.Snapshot, now time.Time) (time.Time, time.Time, error) {
	if snapshot.FiveHour.ResetsAt == nil || snapshot.FiveHour.ResetsAtRFC3339 == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("five-hour reset evidence is incomplete")
	}
	fromEpoch := time.Unix(*snapshot.FiveHour.ResetsAt, 0).UTC()
	fromRFC3339, err := time.Parse(time.RFC3339, *snapshot.FiveHour.ResetsAtRFC3339)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid five-hour reset RFC3339: %w", err)
	}
	if !fromEpoch.Equal(fromRFC3339.UTC()) {
		return time.Time{}, time.Time{}, fmt.Errorf("five-hour reset evidence disagrees: epoch=%s rfc3339=%s", fromEpoch.Format(time.RFC3339), fromRFC3339.UTC().Format(time.RFC3339))
	}
	if !fromEpoch.After(now.UTC()) {
		return time.Time{}, time.Time{}, fmt.Errorf("five-hour reset is not in the future: %s", fromEpoch.Format(time.RFC3339))
	}
	return fromEpoch, fromEpoch.Add(codexWakeSafetyMargin), nil
}

func resolveCodexWakeAutomation(dir, wakeThreadID string) (string, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read automation directory: %w", err)
	}
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		toml, problem := readWakeTOML(filepath.Join(dir, entry.Name(), "automation.toml"), entry.Name())
		if problem != "" {
			return "", false, fmt.Errorf("automation inventory is not safely readable: %s", problem)
		}
		if toml.TargetThreadID == wakeThreadID {
			matches = append(matches, toml.ID)
		}
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("multiple automations target wake thread %s: %d", wakeThreadID, len(matches))
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	return matches[0], true, nil
}

func codexWakeCreateSpec(transaction codexWakeTransaction) CodexWakeWriteSpec {
	return CodexWakeWriteSpec{
		Boundary:       externalAutomationBoundary,
		Tool:           automationUpdateTool,
		Mode:           "create",
		Name:           transaction.ExpectedAutomationID,
		TargetThreadID: transaction.WakeThreadID,
		Status:         pausedStatus,
		RRule:          placeholderHourlyRRule,
	}
}

func codexWakeUpdateSpec(transaction codexWakeTransaction) CodexWakeWriteSpec {
	wakeAt, _ := time.Parse(time.RFC3339, transaction.WakeAtRFC3339)
	return CodexWakeWriteSpec{
		Boundary:       externalAutomationBoundary,
		Tool:           automationUpdateTool,
		Mode:           "update",
		AutomationID:   transaction.ExpectedAutomationID,
		TargetThreadID: transaction.WakeThreadID,
		Status:         activeStatus,
		RRule:          "DTSTART:" + wakeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1",
	}
}

func codexWakeDeleteSpec(automationID string) *CodexWakeWriteSpec {
	if !keyPattern.MatchString(automationID) {
		return nil
	}
	return &CodexWakeWriteSpec{Boundary: externalAutomationBoundary, Tool: automationUpdateTool, Mode: "delete", AutomationID: automationID}
}

func codexWakePauseSpec(transaction codexWakeTransaction) *CodexWakeWriteSpec {
	return &CodexWakeWriteSpec{Boundary: externalAutomationBoundary, Tool: automationUpdateTool, Mode: "update", AutomationID: transaction.ExpectedAutomationID, Status: pausedStatus}
}

func codexWakeWriteOutput(transaction codexWakeTransaction, write CodexWakeWriteSpec) CodexWakeOutput {
	token, transactionID := encodeSignedTransaction(transaction)
	return CodexWakeOutput{
		Version:              codexWakeTransactionVersion,
		Status:               CodexWakeStatusWriteRequired,
		Stage:                transaction.Stage,
		TransactionID:        transactionID,
		WakeThreadID:         transaction.WakeThreadID,
		ExpectedAutomationID: transaction.ExpectedAutomationID,
		ResetAtRFC3339:       transaction.ResetAtRFC3339,
		WakeAtRFC3339:        transaction.WakeAtRFC3339,
		Attempt:              transaction.Attempt,
		Write:                &write,
		Token:                token,
	}
}

func decodeCodexWakeTransaction(token string) (codexWakeTransaction, string, error) {
	payload, digest, err := decodeSignedTransaction(token)
	if err != nil {
		return codexWakeTransaction{}, "", err
	}
	var transaction codexWakeTransaction
	if err := json.Unmarshal(payload, &transaction); err != nil {
		return codexWakeTransaction{}, "", fmt.Errorf("decode transaction payload: %w", err)
	}
	if err := validateCodexWakeTransaction(transaction); err != nil {
		return codexWakeTransaction{}, "", err
	}
	return transaction, digest, nil
}

func encodeSignedTransaction(payload any) (string, string) {
	data, _ := json.Marshal(payload)
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	return base64.RawURLEncoding.EncodeToString(data) + "." + digest, digest
}

func decodeSignedTransaction(token string) ([]byte, string, error) {
	encoded, digest, found := strings.Cut(token, ".")
	if !found || encoded == "" || len(digest) != sha256.Size*2 {
		return nil, "", fmt.Errorf("invalid transaction token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, "", fmt.Errorf("decode transaction token: %w", err)
	}
	sum := sha256.Sum256(payload)
	actual := hex.EncodeToString(sum[:])
	if actual != digest {
		return nil, "", fmt.Errorf("transaction token checksum mismatch")
	}
	return payload, digest, nil
}

func CodexWakeTransactionContext(token string) (string, bool, error) {
	transaction, _, err := decodeCodexWakeTransaction(token)
	if err != nil {
		return "", false, err
	}
	return transaction.WakeThreadID, transaction.WakeInvocation, nil
}

func validateCodexWakeTransaction(transaction codexWakeTransaction) error {
	if transaction.Version != codexWakeTransactionVersion || !uuidPattern.MatchString(transaction.WakeThreadID) {
		return fmt.Errorf("invalid transaction identity")
	}
	if transaction.ExpectedAutomationID != CodexWakeAutomationKey(transaction.WakeThreadID) {
		return fmt.Errorf("transaction automation identity mismatch")
	}
	if !transactionNoncePattern.MatchString(transaction.Nonce) {
		return fmt.Errorf("invalid transaction nonce")
	}
	if transaction.Stage != stageCreatePlaceholder && transaction.Stage != stageUpdateOneShot {
		return fmt.Errorf("invalid transaction stage %q", transaction.Stage)
	}
	if transaction.Attempt < 1 || transaction.Attempt > 2 {
		return fmt.Errorf("invalid transaction attempt %d", transaction.Attempt)
	}
	if _, err := time.Parse(time.RFC3339, transaction.ResetAtRFC3339); err != nil {
		return fmt.Errorf("invalid transaction reset time: %w", err)
	}
	if _, err := time.Parse(time.RFC3339, transaction.WakeAtRFC3339); err != nil {
		return fmt.Errorf("invalid transaction wake time: %w", err)
	}
	return nil
}
