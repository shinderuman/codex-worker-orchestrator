package autoresume

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testFallbackTransaction(stage string, attempt int, created bool) autoResumeTransaction {
	resetAt, resumeAt := testResumeTimes()
	return autoResumeTransaction{
		Version:              autoResumeTransactionVersion,
		Stage:                stage,
		TaskID:               testResumeTaskID,
		RepoRoot:             testResumeRepo,
		ParentThreadID:       testResumeThread,
		ExpectedAutomationID: testResumeKey,
		ResetAtRFC3339:       resetAt,
		ResumeAtRFC3339:      resumeAt,
		CoalesceDecision:     DecisionCreateGLMWake,
		Nonce:                "0123456789abcdef0123456789abcdef",
		CreatedByTransaction: created,
		Attempt:              attempt,
	}
}

func testFallbackToken(t *testing.T, transaction autoResumeTransaction) string {
	t.Helper()
	token, _ := encodeSignedTransaction(transaction)
	return token
}

func writeFallbackTOML(t *testing.T, dir string, transaction autoResumeTransaction, status, rrule string) {
	t.Helper()
	path := filepath.Join(dir, transaction.ExpectedAutomationID, "automation.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(
		"version = 1\nid = %q\nkind = %q\nname = %q\nprompt = %q\nstatus = %q\nrrule = %q\ntarget_thread_id = %q\ncreated_at = 1\n",
		transaction.ExpectedAutomationID,
		"heartbeat",
		transaction.ExpectedAutomationID,
		buildAutoResumePrompt(transaction),
		status,
		rrule,
		transaction.ParentThreadID,
	)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateAutoResumeFallbackAllowsCreateBeforeExternalWrite(t *testing.T) {
	transaction := testFallbackTransaction(stageCreatePlaceholder, 1, false)
	plan, decision, err := EvaluateAutoResumeFallback(
		testFallbackToken(t, transaction),
		t.TempDir(),
		"unused",
		fixedDBReader(nil, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision != AutoResumeFallbackLocalWait {
		t.Fatalf("decision = %q want %q", decision, AutoResumeFallbackLocalWait)
	}
	if plan.Stage != stageCreatePlaceholder || plan.Attempt != 1 || plan.ExpectedAutomationID != testResumeKey {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestEvaluateAutoResumeFallbackAllowsPausedPlaceholderAfterCreate(t *testing.T) {
	transaction := testFallbackTransaction(stageUpdateOneShot, 1, true)
	dir := t.TempDir()
	writeFallbackTOML(t, dir, transaction, pausedStatus, placeholderHourlyRRule)
	rows := map[string]DBRow{
		transaction.ExpectedAutomationID: {
			ID:     transaction.ExpectedAutomationID,
			Status: pausedStatus,
			Rrule:  placeholderHourlyRRule,
		},
	}
	_, decision, err := EvaluateAutoResumeFallback(
		testFallbackToken(t, transaction),
		dir,
		"unused",
		fixedDBReader(rows, nil),
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision != AutoResumeFallbackLocalWait {
		t.Fatalf("decision = %q want %q", decision, AutoResumeFallbackLocalWait)
	}
}

func TestEvaluateAutoResumeFallbackTrustsExactActiveWake(t *testing.T) {
	cases := []struct {
		name        string
		attempt     int
		created     bool
	}{
		{name: "retry after external update", attempt: 2, created: true},
		{name: "existing automation update", attempt: 1, created: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transaction := testFallbackTransaction(stageUpdateOneShot, tc.attempt, tc.created)
			dir := t.TempDir()
			resumeAt, err := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
			if err != nil {
				t.Fatal(err)
			}
			rrule := "DTSTART:" + resumeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
			writeFallbackTOML(t, dir, transaction, activeStatus, rrule)
			rows := map[string]DBRow{
				transaction.ExpectedAutomationID: {
					ID:         transaction.ExpectedAutomationID,
					Status:     activeStatus,
					Rrule:      rrule,
					NextRunAt:  resumeAt.UnixMilli(),
					HasNextRun: true,
				},
			}
			_, decision, err := EvaluateAutoResumeFallback(
				testFallbackToken(t, transaction),
				dir,
				"unused",
				fixedDBReader(rows, nil),
			)
			if err != nil {
				t.Fatal(err)
			}
			if decision != AutoResumeFallbackExternalWake {
				t.Fatalf("decision = %q want %q", decision, AutoResumeFallbackExternalWake)
			}
		})
	}
}

func TestEvaluateAutoResumeFallbackFailsClosedOnActiveMismatch(t *testing.T) {
	transaction := testFallbackTransaction(stageUpdateOneShot, 2, true)
	dir := t.TempDir()
	resumeAt, err := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	rrule := "DTSTART:" + resumeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
	writeFallbackTOML(t, dir, transaction, activeStatus, rrule)
	rows := map[string]DBRow{
		transaction.ExpectedAutomationID: {
			ID:         transaction.ExpectedAutomationID,
			Status:     activeStatus,
			Rrule:      rrule,
			NextRunAt:  resumeAt.Add(time.Minute).UnixMilli(),
			HasNextRun: true,
		},
	}
	_, _, err = EvaluateAutoResumeFallback(
		testFallbackToken(t, transaction),
		dir,
		"unused",
		fixedDBReader(rows, nil),
	)
	if err == nil || !strings.Contains(err.Error(), "neither the exact ACTIVE one-shot nor the exact PAUSED placeholder") {
		t.Fatalf("error = %v", err)
	}
}
