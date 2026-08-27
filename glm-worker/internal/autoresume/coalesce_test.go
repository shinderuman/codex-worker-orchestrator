package autoresume

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeCoalesceTOML(t *testing.T, path, id, name, status, targetThreadID, prompt, dtstart string) {
	t.Helper()
	content := "version = 1\n" +
		"id = \"" + id + "\"\n" +
		"kind = \"heartbeat\"\n" +
		"name = \"" + name + "\"\n" +
		"prompt = \"" + prompt + "\"\n" +
		"status = \"" + status + "\"\n" +
		"rrule = \"DTSTART:" + dtstart + "\\nRRULE:FREQ=DAILY;COUNT=1\"\n" +
		"target_thread_id = \"" + targetThreadID + "\"\n" +
		"created_at = 1\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func coalescePrompt(parentThreadID string) string {
	return "親実装task " + parentThreadID + "へ固定文「作業を続けろ」を1回送信する"
}

func fixedDBReader(rows map[string]DBRow, failure error) DBReader {
	return func(dbPath, key string) (DBRow, error) {
		if failure != nil {
			return DBRow{}, failure
		}
		row, ok := rows[key]
		if !ok {
			return DBRow{}, ErrRowNotFound
		}
		return row, nil
	}
}

func dbRowAt(dtstart string) DBRow {
	at, err := time.Parse(dtStartLayout, dtstart)
	if err != nil {
		panic(err)
	}
	return DBRow{ID: "", Status: "ACTIVE", Rrule: "DTSTART:" + dtstart + "\nRRULE:FREQ=DAILY;COUNT=1", NextRunAt: at.UnixMilli(), HasNextRun: true}
}

func TestCheckCoalesceRejectsInvalidArguments(t *testing.T) {
	params := CoalesceParams{
		ParentThreadID:  "01a0244a-4ee4-7e71-b2e1-dec3bdda2120",
		ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		AutomationsDir:  t.TempDir(),
		DBPath:          "unused",
	}
	badThread := params
	badThread.ParentThreadID = "bad thread!"
	if _, err := CheckCoalesce(badThread, fixedDBReader(nil, nil)); err == nil || !strings.Contains(err.Error(), "invalid parent thread ID") {
		t.Fatalf("bad thread error = %v", err)
	}
	badTime := params
	badTime.ResumeAtRFC3339 = "2026-08-26 15:17:55"
	if _, err := CheckCoalesce(badTime, fixedDBReader(nil, nil)); err == nil || !strings.Contains(err.Error(), "invalid resume time") {
		t.Fatalf("bad time error = %v", err)
	}
}

func TestCheckCoalesceMissingAutomationsDirCreatesGLMWake(t *testing.T) {
	params := CoalesceParams{
		ParentThreadID:  "01a0244a-4ee4-7e71-b2e1-dec3bdda2120",
		ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		AutomationsDir:  filepath.Join(t.TempDir(), "absent-automations"),
		DBPath:          "unused",
	}
	result, err := CheckCoalesce(params, fixedDBReader(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCreateGLMWake || result.Reason != "no codex wake automation targets the parent thread" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCheckCoalesceEntityProblemsFailTowardGLMWake(t *testing.T) {
	parent := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	wakeDir := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	resumeAt := "2026-08-26T15:17:55Z"

	cases := []struct {
		name          string
		prepare       func(t *testing.T, dir string)
		wantReasonSub string
	}{
		{
			name: "unreadable automation file",
			prepare: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, wakeDir), 0o755); err != nil {
					t.Fatal(err)
				}
			},
			wantReasonSub: "wake automation entity unreadable",
		},
		{
			name: "invalid toml",
			prepare: func(t *testing.T, dir string) {
				path := filepath.Join(dir, wakeDir, "automation.toml")
				writeCoalesceTOML(t, path, wakeDir, wakeDir, "ACTIVE", strings.TrimPrefix(wakeDir, "codex-5h-wake-"), coalescePrompt(parent), "20260826T152059")
				if err := os.WriteFile(path, []byte("not toml at all\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			wantReasonSub: "wake automation TOML invalid",
		},
		{
			name: "id does not match directory",
			prepare: func(t *testing.T, dir string) {
				writeCoalesceTOML(t, filepath.Join(dir, wakeDir, "automation.toml"), "codex-5h-wake-other", "codex-5h-wake-other", "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", coalescePrompt(parent), "20260826T152059")
			},
			wantReasonSub: "does not match its directory",
		},
		{
			name: "name does not match directory",
			prepare: func(t *testing.T, dir string) {
				writeCoalesceTOML(t, filepath.Join(dir, wakeDir, "automation.toml"), wakeDir, "codex-5h-wake-renamed", "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", coalescePrompt(parent), "20260826T152059")
			},
			wantReasonSub: "does not match its directory",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			c.prepare(t, dir)
			result, err := CheckCoalesce(CoalesceParams{
				ParentThreadID:  parent,
				ResumeAtRFC3339: resumeAt,
				AutomationsDir:  dir,
				DBPath:          "unused",
			}, fixedDBReader(nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != DecisionCreateGLMWake || !strings.Contains(result.Reason, c.wantReasonSub) {
				t.Fatalf("result = %+v want reason containing %q", result, c.wantReasonSub)
			}
		})
	}
}

func TestCheckCoalesceCandidateProblemsFailTowardGLMWake(t *testing.T) {
	parent := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	wakeID := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	resumeAt := "2026-08-26T15:17:55Z"

	cases := []struct {
		name       string
		status     string
		target     string
		db         DBRow
		rowMissing bool
		dbFailure  error
		wantReason string
	}{
		{
			name:       "paused toml status",
			status:     "PAUSED",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			db:         dbRowAt("20260826T152059"),
			wantReason: `wake automation status is "PAUSED" want ACTIVE`,
		},
		{
			name:       "id does not bind to target thread",
			status:     "ACTIVE",
			target:     "0aaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			db:         dbRowAt("20260826T152059"),
			wantReason: `wake automation id "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490" does not bind to target_thread_id "0aaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"`,
		},
		{
			name:       "db row not found",
			status:     "ACTIVE",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			rowMissing: true,
			wantReason: "wake automation row not found in the scheduler database",
		},
		{
			name:       "db unreadable",
			status:     "ACTIVE",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			dbFailure:  errors.New("sqlite3: no such table"),
			wantReason: "wake schedule verification unavailable: sqlite3: no such table",
		},
		{
			name:       "db status paused",
			status:     "ACTIVE",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			db:         DBRow{Status: "PAUSED"},
			wantReason: `wake scheduler status is "PAUSED" want ACTIVE`,
		},
		{
			name:       "next run null",
			status:     "ACTIVE",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			db:         DBRow{Status: "ACTIVE"},
			wantReason: "wake next_run_at is NULL",
		},
		{
			name:       "rrule drifts from db next run",
			status:     "ACTIVE",
			target:     "01a03a9e-10a0-7f11-801c-f04e5dbd5490",
			db:         dbRowAt("20260826T151755"),
			wantReason: `wake rrule does not match the one-shot next_run_at: DTSTART line: got "DTSTART:20260826T152059" want "DTSTART:20260826T151755"`,
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			writeCoalesceTOML(t, filepath.Join(dir, wakeID, "automation.toml"), wakeID, wakeID, c.status, c.target, coalescePrompt(parent), "20260826T152059")
			row := c.db
			row.ID = wakeID
			var rows map[string]DBRow
			if !c.rowMissing {
				rows = map[string]DBRow{wakeID: row}
			}
			result, err := CheckCoalesce(CoalesceParams{
				ParentThreadID:  parent,
				ResumeAtRFC3339: resumeAt,
				AutomationsDir:  dir,
				DBPath:          "unused",
			}, fixedDBReader(rows, c.dbFailure))
			if err != nil {
				t.Fatal(err)
			}
			if result.Decision != DecisionCreateGLMWake || result.Reason != c.wantReason {
				t.Fatalf("result = %+v want reason %q", result, c.wantReason)
			}
		})
	}
}

func TestCheckCoalesceCoalescesActiveWakeWithinWindow(t *testing.T) {
	parent := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	wakeID := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	dir := t.TempDir()
	writeCoalesceTOML(t, filepath.Join(dir, wakeID, "automation.toml"), wakeID, wakeID, "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", coalescePrompt(parent), "20260826T152059")
	writeCoalesceTOML(t, filepath.Join(dir, "greptile-review-2", "automation.toml"), "greptile-review-2", "Greptile日次外部review", "ACTIVE", "01a03ac1-92ad-7b10-a447-07c4c89b5c9b", "親実装task thread "+parent+" へ正規化結果を1回だけ送る", "20260826T001500")
	writeCoalesceTOML(t, filepath.Join(dir, "glm-worker-resume-appshort1234-abcd1234", "automation.toml"), "glm-worker-resume-appshort1234-abcd1234", "glm-worker-resume-appshort1234-abcd1234", "ACTIVE", parent, "glm-worker --resume", "20260826T151955")

	result, err := CheckCoalesce(CoalesceParams{
		ParentThreadID:  parent,
		ResumeAtRFC3339: "2026-08-26T15:17:55Z",
		AutomationsDir:  dir,
		DBPath:          "unused",
	}, fixedDBReader(map[string]DBRow{
		wakeID: dbRowAt("20260826T152059"),
	}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != DecisionCoalesce || result.Reason != "" {
		t.Fatalf("result = %+v", result)
	}
	if result.WakeAutomationID != wakeID || result.WakeThread != "01a03a9e-10a0-7f11-801c-f04e5dbd5490" {
		t.Fatalf("wake identity = %+v", result)
	}
	if result.WakeNextRunUTC != "2026-08-26T15:20:59Z" || result.AddedWaitSeconds != 184 {
		t.Fatalf("wake schedule = %+v", result)
	}
	if result.ResumeAtUTC != "2026-08-26T15:17:55Z" || result.ParentThread != parent {
		t.Fatalf("echo fields = %+v", result)
	}
}
