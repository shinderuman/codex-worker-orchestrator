package autoresume

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexlimit"
)

const (
	testCodexWakeThread = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	testOtherWakeThread = "01a05f46-47aa-77d2-912c-0d6b078cb856"
)

func TestBuildCodexWakeTransactionCreatesPlaceholderFromCanonicalReset(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(3 * time.Hour)
	output, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	wantID := CodexWakeAutomationKey(testCodexWakeThread)
	if output.Status != CodexWakeStatusWriteRequired || output.Stage != codexWakeStageCreate || output.ExpectedAutomationID != wantID {
		t.Fatalf("output = %#v", output)
	}
	if output.WakeAtRFC3339 != reset.Add(2*time.Minute).Format(time.RFC3339) || output.Token == "" || output.TransactionID == "" {
		t.Fatalf("timing/token = %#v", output)
	}
	if output.Write == nil || output.Write.Mode != "create" || output.Write.Name != wantID || output.Write.TargetThreadID != testCodexWakeThread || output.Write.Status != codexWakePaused || output.Write.RRule != codexWakePlaceholderRRule {
		t.Fatalf("write = %#v", output.Write)
	}
}

func TestBuildCodexWakeTransactionReusesOnlyExactTargetAutomation(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	dir := t.TempDir()
	key := CodexWakeAutomationKey(testCodexWakeThread)
	writeCodexWakeFixture(t, dir, key, testCodexWakeThread, "PAUSED", "RRULE:FREQ=HOURLY")
	output, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if output.Stage != codexWakeStageUpdate || output.Write == nil || output.Write.AutomationID != key || output.Write.Status != codexWakeActive {
		t.Fatalf("output = %#v", output)
	}
	wantRRule := "DTSTART:" + reset.Add(2*time.Minute).Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
	if output.Write.RRule != wantRRule {
		t.Fatalf("rrule = %q want %q", output.Write.RRule, wantRRule)
	}
}

func TestBuildCodexWakeTransactionRejectsUnsafeIdentityOrResetBeforeWrite(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	mismatched := testCodexWakeSnapshot(future)
	wrongRFC := future.Add(time.Minute).Format(time.RFC3339)
	mismatched.FiveHour.ResetsAtRFC3339 = &wrongRFC
	missing := testCodexWakeSnapshot(future)
	missing.FiveHour.ResetsAt = nil
	cases := []struct {
		name     string
		snapshot codexlimit.Snapshot
		thread   string
	}{
		{name: "invalid thread", snapshot: testCodexWakeSnapshot(future), thread: "not-a-thread"},
		{name: "missing reset", snapshot: missing, thread: testCodexWakeThread},
		{name: "mismatched reset", snapshot: mismatched, thread: testCodexWakeThread},
		{name: "stale reset", snapshot: testCodexWakeSnapshot(now), thread: testCodexWakeThread},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildCodexWakeTransaction(tc.snapshot, tc.thread, "", t.TempDir(), now); err == nil {
				t.Fatal("expected fail-closed error")
			}
		})
	}
}

func TestBuildCodexWakeTransactionRejectsWrongOrMultipleAutomationIdentity(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	t.Run("wrong id", func(t *testing.T) {
		dir := t.TempDir()
		writeCodexWakeFixture(t, dir, "other-wake", testCodexWakeThread, "ACTIVE", "RRULE:FREQ=HOURLY")
		if _, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now); err == nil {
			t.Fatal("wrong automation ID was accepted")
		}
	})
	t.Run("multiple", func(t *testing.T) {
		dir := t.TempDir()
		writeCodexWakeFixture(t, dir, CodexWakeAutomationKey(testCodexWakeThread), testCodexWakeThread, "ACTIVE", "RRULE:FREQ=HOURLY")
		writeCodexWakeFixture(t, dir, "other-wake", testCodexWakeThread, "ACTIVE", "RRULE:FREQ=HOURLY")
		if _, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now); err == nil {
			t.Fatal("multiple target automations were accepted")
		}
	})
}

func TestBuildCodexWakeTransactionBindsWakeInvocationToExactFiredID(t *testing.T) {
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	if _, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "wrong-id", t.TempDir(), now); err == nil {
		t.Fatal("wrong fired automation ID was accepted")
	}
	key := CodexWakeAutomationKey(testCodexWakeThread)
	output, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, key, t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	transaction, _, err := decodeCodexWakeTransaction(output.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !transaction.WakeInvocation || output.Write == nil || output.Write.AutomationID != key || output.Write.Mode != "update" {
		t.Fatalf("wake output = %#v transaction=%#v", output, transaction)
	}
}

func TestCodexWakeCreateResponseAdvancesOnlyExactSuccessfulEntity(t *testing.T) {
	plan := newCreateCodexWakePlan(t)
	response := codexWakeResponseJSON(t, false, map[string]any{
		"automation_id": plan.ExpectedAutomationID,
		"mode":          "create",
		"status":        "PAUSED",
		"message":       "Automation created successfully",
	})
	output := AdvanceCodexWakeTransaction(plan.Token, response, t.TempDir(), "unused", func(string, string) (DBRow, error) {
		return DBRow{}, ErrRowNotFound
	})
	if output.Status != CodexWakeStatusWriteRequired || output.Stage != codexWakeStageUpdate || output.Attempt != 1 || output.Cleanup != nil {
		t.Fatalf("output = %#v", output)
	}
	if output.Write == nil || output.Write.AutomationID != plan.ExpectedAutomationID || output.Write.Status != codexWakeActive {
		t.Fatalf("write = %#v", output.Write)
	}
}

func TestCodexWakeCreateResponseWrongIDLimitsCleanupToReturnedEntity(t *testing.T) {
	plan := newCreateCodexWakePlan(t)
	returned := "unexpected-created-id"
	response := codexWakeResponseJSON(t, false, map[string]any{
		"automation_id": returned,
		"mode":          "create",
		"status":        "PAUSED",
		"message":       "Automation created successfully",
	})
	output := AdvanceCodexWakeTransaction(plan.Token, response, t.TempDir(), "unused", func(string, string) (DBRow, error) {
		return DBRow{}, ErrRowNotFound
	})
	if output.Status != CodexWakeStatusFailed || output.Cleanup == nil || output.Cleanup.Mode != "delete" || output.Cleanup.AutomationID != returned {
		t.Fatalf("output = %#v", output)
	}
}

func TestCodexWakeResponseRejectsMalformedAndFailureSemantics(t *testing.T) {
	plan := newCreateCodexWakePlan(t)
	cases := []struct {
		name string
		raw  []byte
	}{
		{name: "malformed", raw: []byte(`{"isError":`)},
		{name: "missing isError", raw: []byte(`{"content":[]}`)},
		{name: "isError true", raw: codexWakeResponseJSON(t, true, map[string]any{"automation_id": plan.ExpectedAutomationID, "mode": "create", "status": "PAUSED", "message": "created"})},
		{name: "suggestion", raw: []byte(`{"isError":false,"content":[{"type":"text","text":"Rendered suggestion"}]}`)},
		{name: "wrong status", raw: codexWakeResponseJSON(t, false, map[string]any{"automation_id": plan.ExpectedAutomationID, "mode": "create", "status": "ACTIVE", "message": "created"})},
		{name: "explicit failure message", raw: codexWakeResponseJSON(t, false, map[string]any{"automation_id": plan.ExpectedAutomationID, "mode": "create", "status": "PAUSED", "message": "create failed"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			output := AdvanceCodexWakeTransaction(plan.Token, tc.raw, t.TempDir(), "unused", func(string, string) (DBRow, error) {
				return DBRow{}, ErrRowNotFound
			})
			if output.Status != CodexWakeStatusFailed {
				t.Fatalf("output = %#v", output)
			}
		})
	}
}

func TestCodexWakeUpdateRequiresSavedStateMatch(t *testing.T) {
	update := newUpdateCodexWakePlan(t, false)
	wakeAt, err := time.Parse(time.RFC3339, update.WakeAtRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	rrule := "DTSTART:" + wakeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
	dir := t.TempDir()
	writeCodexWakeFixture(t, dir, update.ExpectedAutomationID, testCodexWakeThread, "ACTIVE", rrule)
	response := codexWakeResponseJSON(t, false, map[string]any{
		"automation_id": update.ExpectedAutomationID,
		"mode":          "update",
		"status":        "ACTIVE",
		"message":       "Automation updated successfully",
	})
	output := AdvanceCodexWakeTransaction(update.Token, response, dir, "db", func(_ string, key string) (DBRow, error) {
		return DBRow{ID: key, Status: "ACTIVE", Rrule: rrule, NextRunAt: wakeAt.UnixMilli(), HasNextRun: true}, nil
	})
	if output.Status != CodexWakeStatusVerified || output.Verification == nil || output.Verification.Outcome != Pass || output.Cleanup != nil {
		t.Fatalf("output = %#v", output)
	}
}

func TestCodexWakeUpdateRetriesOnceThenCleansOnlyOwnedPlaceholder(t *testing.T) {
	update := newUpdateCodexWakePlan(t, true)
	bad := codexWakeResponseJSON(t, false, map[string]any{
		"automation_id": update.ExpectedAutomationID,
		"mode":          "update",
		"status":        "PAUSED",
		"message":       "Automation updated successfully",
	})
	first := AdvanceCodexWakeTransaction(update.Token, bad, t.TempDir(), "unused", func(string, string) (DBRow, error) {
		return DBRow{}, ErrRowNotFound
	})
	if first.Status != CodexWakeStatusWriteRequired || first.Attempt != 2 || first.Write == nil || first.Write.AutomationID != update.ExpectedAutomationID {
		t.Fatalf("first = %#v", first)
	}
	second := AdvanceCodexWakeTransaction(first.Token, bad, t.TempDir(), "unused", func(string, string) (DBRow, error) {
		return DBRow{}, ErrRowNotFound
	})
	if second.Status != CodexWakeStatusFailed || second.Cleanup == nil || second.Cleanup.Mode != "delete" || second.Cleanup.AutomationID != update.ExpectedAutomationID {
		t.Fatalf("second = %#v", second)
	}
}

func TestCodexWakeVerificationUnavailableIsNotSuccess(t *testing.T) {
	update := newUpdateCodexWakePlan(t, true)
	wakeAt, err := time.Parse(time.RFC3339, update.WakeAtRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	rrule := "DTSTART:" + wakeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
	dir := t.TempDir()
	writeCodexWakeFixture(t, dir, update.ExpectedAutomationID, testCodexWakeThread, "ACTIVE", rrule)
	response := codexWakeResponseJSON(t, false, map[string]any{"automation_id": update.ExpectedAutomationID, "mode": "update", "status": "ACTIVE", "message": "updated successfully"})
	output := AdvanceCodexWakeTransaction(update.Token, response, dir, "db", func(string, string) (DBRow, error) {
		return DBRow{}, ErrDBUnreadable
	})
	if output.Status != CodexWakeStatusFailed || output.Verification != nil || output.Cleanup == nil || output.Cleanup.AutomationID != update.ExpectedAutomationID {
		t.Fatalf("output = %#v", output)
	}
}

func TestCodexWakeInvocationFailurePausesOnlyFiredAutomation(t *testing.T) {
	update := newUpdateCodexWakePlan(t, false)
	transaction, _, err := decodeCodexWakeTransaction(update.Token)
	if err != nil {
		t.Fatal(err)
	}
	transaction.WakeInvocation = true
	transaction.Attempt = 2
	update = codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction))
	bad := codexWakeResponseJSON(t, false, map[string]any{"automation_id": update.ExpectedAutomationID, "mode": "update", "status": "PAUSED", "message": "updated successfully"})
	output := AdvanceCodexWakeTransaction(update.Token, bad, t.TempDir(), "unused", func(string, string) (DBRow, error) {
		return DBRow{}, ErrRowNotFound
	})
	if output.Status != CodexWakeStatusFailed || output.Cleanup == nil || output.Cleanup.Mode != "update" || output.Cleanup.Status != "PAUSED" || output.Cleanup.AutomationID != update.ExpectedAutomationID {
		t.Fatalf("output = %#v", output)
	}
}

func newCreateCodexWakePlan(t *testing.T) CodexWakeOutput {
	t.Helper()
	now := time.Date(2026, 9, 11, 2, 0, 0, 0, time.UTC)
	output, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(now.Add(time.Hour)), testCodexWakeThread, "", t.TempDir()+"/missing", now)
	if err != nil {
		t.Fatal(err)
	}
	return output
}

func newUpdateCodexWakePlan(t *testing.T, created bool) CodexWakeOutput {
	t.Helper()
	plan := newCreateCodexWakePlan(t)
	transaction, _, err := decodeCodexWakeTransaction(plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	transaction.Stage = codexWakeStageUpdate
	transaction.CreatedByTransaction = created
	return codexWakeWriteOutput(transaction, codexWakeUpdateSpec(transaction))
}

func testCodexWakeSnapshot(reset time.Time) codexlimit.Snapshot {
	epoch := reset.Unix()
	rfc3339 := reset.UTC().Format(time.RFC3339)
	return codexlimit.Snapshot{FiveHour: codexlimit.Window{WindowDurationMins: 300, ResetsAt: &epoch, ResetsAtRFC3339: &rfc3339}}
}

func writeCodexWakeFixture(t *testing.T, dir, id, thread, status, rrule string) {
	t.Helper()
	entityDir := filepath.Join(dir, id)
	if err := os.MkdirAll(entityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("id = %q\nname = %q\nstatus = %q\nrrule = %q\ntarget_thread_id = %q\nprompt = %q\n", id, id, status, rrule, thread, "wake parent")
	if err := os.WriteFile(filepath.Join(entityDir, "automation.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func codexWakeResponseJSON(t *testing.T, isError bool, payload map[string]any) []byte {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"isError": isError,
		"content": []map[string]any{{"type": "text", "text": string(payloadBytes)}},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
