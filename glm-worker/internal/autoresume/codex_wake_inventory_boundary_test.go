package autoresume

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexWakeInventoryIgnoresUnrelatedAutomationShape(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	cases := []struct {
		name string
		body string
	}{
		{
			name: "other thread with unrelated display name",
			body: fmt.Sprintf(
				"id = %q\nname = %q\nstatus = %q\nrrule = %q\nprompt = %q\ntarget_thread_id = %q\n",
				"daily-check",
				"Daily checks",
				"ACTIVE",
				"RRULE:FREQ=DAILY",
				"Check build results",
				testOtherWakeThread,
			),
		},
		{
			name: "targetless cron",
			body: "id = \"daily-check\"\nname = \"Daily checks\"\nstatus = \"ACTIVE\"\nrrule = \"RRULE:FREQ=DAILY\"\nprompt = \"Check build results\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAutomationInventoryFixture(t, dir, "daily-check", tc.body)
			output, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now)
			if err != nil {
				t.Fatal(err)
			}
			if output.Stage != stageCreatePlaceholder || output.Write == nil || output.Write.Mode != "create" {
				t.Fatalf("unrelated automation changed wake plan: %#v", output)
			}
		})
	}
}

func TestCodexWakeInventoryStrictlyValidatesMatchingCandidate(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	dir := t.TempDir()
	key := CodexWakeAutomationKey(testCodexWakeThread)
	body := fmt.Sprintf(
		"id = %q\nname = %q\nstatus = %q\nrrule = %q\ntarget_thread_id = %q\n",
		key,
		"wrong-wake-name",
		"PAUSED",
		"RRULE:FREQ=HOURLY",
		testCodexWakeThread,
	)
	writeAutomationInventoryFixture(t, dir, key, body)
	if _, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now); err == nil {
		t.Fatal("matching wake candidate with invalid name was accepted")
	}
}

func TestCodexWakeInventoryRejectsUnreadableIdentity(t *testing.T) {
	now := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	reset := now.Add(time.Hour)
	dir := t.TempDir()
	writeAutomationInventoryFixture(t, dir, "broken", "id = \"broken\"\ntarget_thread_id = \"unterminated\n")
	if _, err := BuildCodexWakeTransaction(testCodexWakeSnapshot(reset), testCodexWakeThread, "", dir, now); err == nil {
		t.Fatal("unreadable automation inventory was accepted")
	}
}

func writeAutomationInventoryFixture(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "automation.toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
