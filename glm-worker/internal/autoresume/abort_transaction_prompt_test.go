package autoresume

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvaluateAutoResumeFallbackFailsClosedOnActivePromptMismatch(t *testing.T) {
	transaction := testFallbackTransaction(stageUpdateOneShot, 2, true)
	dir := t.TempDir()
	resumeAt, err := time.Parse(time.RFC3339, transaction.ResumeAtRFC3339)
	if err != nil {
		t.Fatal(err)
	}
	rrule := "DTSTART:" + resumeAt.UTC().Format(dtStartLayout) + "\nRRULE:FREQ=DAILY;COUNT=1"
	writeFallbackTOML(t, dir, transaction, activeStatus, rrule)

	path := filepath.Join(dir, transaction.ExpectedAutomationID, "automation.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wantPrompt := fmt.Sprintf("prompt = %q", buildAutoResumePrompt(transaction))
	stale := strings.Replace(string(data), wantPrompt, `prompt = "stale task prompt"`, 1)
	if stale == string(data) {
		t.Fatal("test did not replace the transaction prompt")
	}
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	rows := map[string]DBRow{
		transaction.ExpectedAutomationID: {
			ID:         transaction.ExpectedAutomationID,
			Status:     activeStatus,
			Rrule:      rrule,
			NextRunAt:  resumeAt.UnixMilli(),
			HasNextRun: true,
		},
	}
	_, _, err = EvaluateAutoResumeFallback(
		testFallbackToken(t, transaction),
		dir,
		"unused",
		fixedDBReader(rows, nil),
	)
	if err == nil || !strings.Contains(err.Error(), "ACTIVE wake prompt does not match the transaction") {
		t.Fatalf("error = %v", err)
	}
}
