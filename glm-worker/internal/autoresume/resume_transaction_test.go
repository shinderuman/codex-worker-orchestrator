package autoresume

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const (
	testResumeTaskID = "12345678-aaaa-bbbb-cccc-dddddddddddd"
	testResumeKey    = "glm-worker-resume-testrepo12-12345678"
	testResumeThread = "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	testResumeRepo   = "/repos/codex-config"
)

func testResumeParams(automationsDir string, resetAtRFC3339, resumeAtRFC3339 string) AutoResumePlanParams {
	return AutoResumePlanParams{
		TaskID:          testResumeTaskID,
		RepoRoot:        testResumeRepo,
		ParentThreadID:  testResumeThread,
		AutomationKey:   testResumeKey,
		ResetAtRFC3339:  resetAtRFC3339,
		ResumeAtRFC3339: resumeAtRFC3339,
		AutomationsDir:  automationsDir,
		DBPath:          "unused",
		Now:             time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC),
	}
}

func testResumeTimes() (string, string) {
	return "2026-09-20T12:00:00Z", "2026-09-20T12:02:00Z"
}

func testResumePlanOutput(t *testing.T, dir string) AutoResumeOutput {
	t.Helper()
	resetAt, resumeAt := testResumeTimes()
	output, err := BuildAutoResumeTransaction(testResumeParams(dir, resetAt, resumeAt), fixedDBReader(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != AutoResumeStatusWriteRequired {
		t.Fatalf("plan status = %s reason = %s", output.Status, output.Reason)
	}
	return output
}

func testResumeToolResponse(t *testing.T, mode, status, automationID, message string, isError *bool) string {
	t.Helper()
	facts, err := json.Marshal(map[string]string{
		"automation_id": automationID,
		"mode":          mode,
		"status":        status,
		"message":       message,
	})
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"content": []map[string]string{{"type": "text", "text": string(facts)}},
	}
	if isError != nil {
		envelope["isError"] = *isError
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func writeResumeTOML(t *testing.T, dir, key, status, targetThreadID, rrule string) {
	t.Helper()
	content := "version = 1\n" +
		"id = \"" + key + "\"\n" +
		"kind = \"heartbeat\"\n" +
		"name = \"" + key + "\"\n" +
		"prompt = \"resume\"\n" +
		"status = \"" + status + "\"\n" +
		"rrule = \"" + strings.ReplaceAll(rrule, "\n", "\\n") + "\"\n" +
		"target_thread_id = \"" + targetThreadID + "\"\n" +
		"created_at = 1\n"
	path := filepath.Join(dir, key, "automation.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAutoResumeTransactionCreatesPlaceholder(t *testing.T) {
	output := testResumePlanOutput(t, t.TempDir())
	if output.Stage != stageCreatePlaceholder {
		t.Fatalf("stage = %s", output.Stage)
	}
	if output.Coalesce == nil || output.Coalesce.Decision != DecisionCreateGLMWake {
		t.Fatalf("coalesce = %#v", output.Coalesce)
	}
	if output.Write == nil || output.Write.Mode != "create" || output.Write.Name != testResumeKey ||
		output.Write.TargetThreadID != testResumeThread || output.Write.Status != "PAUSED" || output.Write.RRule != "RRULE:FREQ=HOURLY" {
		t.Fatalf("create write = %#v", output.Write)
	}
	if output.Write.Prompt == "" {
		t.Fatal("create write must carry the authority prompt")
	}
	if output.Authority == nil {
		t.Fatal("plan output must carry the authority object")
	}
	if output.Token == "" || output.TransactionID == "" {
		t.Fatalf("transaction token missing: %#v", output)
	}
}

func TestBuildAutoResumeTransactionPromptContract(t *testing.T) {
	resetAt, resumeAt := testResumeTimes()
	transaction := autoResumeTransaction{
		TaskID:               testResumeTaskID,
		RepoRoot:             testResumeRepo,
		ParentThreadID:       testResumeThread,
		ExpectedAutomationID: testResumeKey,
		ResetAtRFC3339:       resetAt,
		ResumeAtRFC3339:      resumeAt,
	}
	want := autoResumePromptTrigger + " repo_root=" + testResumeRepo + "; expected_task_id=" + testResumeTaskID + ".\n" +
		"authority: " + strings.Join([]string{autoResumeAuthorityPermission, autoResumeAuthorityContinuation, autoResumeAuthorityGLMScope}, "; ") + "."
	if got := buildAutoResumePrompt(transaction); got != want {
		t.Fatalf("prompt = %q want %q", got, want)
	}
	transaction.RunControl = "現在のACTIVE完了後は次taskを開始せず停止"
	if got := buildAutoResumePrompt(transaction); got != want+"\nrun_control="+transaction.RunControl {
		t.Fatalf("run-control prompt = %q", got)
	}
	if !strings.Contains(autoResumeAuthorityPermission, "permanently authorizes") ||
		!strings.Contains(autoResumeAuthorityPermission, "IMPLEMENTATION_RULES.md") {
		t.Fatalf("permission text = %q", autoResumeAuthorityPermission)
	}
	if !strings.Contains(autoResumeAuthorityContinuation, "already-authorized current task") ||
		!strings.Contains(autoResumeAuthorityContinuation, "same checkout") {
		t.Fatalf("continuation text = %q", autoResumeAuthorityContinuation)
	}
	if !strings.Contains(autoResumeAuthorityGLMScope, "Git remote write only") ||
		!strings.Contains(autoResumeAuthorityGLMScope, "not from GLM execution") {
		t.Fatalf("glm scope text = %q", autoResumeAuthorityGLMScope)
	}
}

func TestBuildAutoResumeTransactionCoalesced(t *testing.T) {
	wakeID := "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	dir := t.TempDir()
	writeCoalesceTOML(t, filepath.Join(dir, wakeID, "automation.toml"), wakeID, wakeID, "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", coalescePrompt(testResumeThread), "20260920T120300")
	params := testResumeParams(dir, "2026-09-20T12:00:00Z", "2026-09-20T12:02:00Z")
	output, err := BuildAutoResumeTransaction(params, fixedDBReader(map[string]DBRow{wakeID: dbRowAt("20260920T120300")}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if output.Status != AutoResumeStatusCoalesced || output.Write != nil || output.Token != "" {
		t.Fatalf("output = %#v", output)
	}
	if output.Coalesce == nil || output.Coalesce.Decision != DecisionCoalesce || output.Coalesce.WakeAutomationID != wakeID {
		t.Fatalf("coalesce = %#v", output.Coalesce)
	}
}

func TestBuildAutoResumeTransactionUpdatesExistingAutomation(t *testing.T) {
	resetAt, resumeAt := testResumeTimes()
	dir := t.TempDir()
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	output, err := BuildAutoResumeTransaction(testResumeParams(dir, resetAt, resumeAt), fixedDBReader(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if output.Stage != stageUpdateOneShot || output.Write == nil || output.Write.Mode != "update" || output.Write.AutomationID != testResumeKey {
		t.Fatalf("output = %#v", output)
	}
	if output.Write.Status != "ACTIVE" || output.Write.RRule != "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1" {
		t.Fatalf("update write = %#v", output.Write)
	}
}

func TestBuildAutoResumeTransactionRejectsExistingAutomationForOtherThread(t *testing.T) {
	resetAt, resumeAt := testResumeTimes()
	dir := t.TempDir()
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", "01a03a9e-10a0-7f11-801c-f04e5dbd5490", "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	_, err := BuildAutoResumeTransaction(testResumeParams(dir, resetAt, resumeAt), fixedDBReader(nil, nil))
	if err == nil || !strings.Contains(err.Error(), "targets thread") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildAutoResumeTransactionRejectsInvalidEvidence(t *testing.T) {
	resetAt, resumeAt := testResumeTimes()
	cases := []struct {
		name   string
		params AutoResumePlanParams
		want   string
	}{
		{
			name:   "reset already past",
			params: withResumeTimes(testResumeParams(t.TempDir(), "", ""), "2026-09-20T09:00:00Z", "2026-09-20T09:02:00Z"),
			want:   "not in the future",
		},
		{
			name:   "resume before reset",
			params: withResumeTimes(testResumeParams(t.TempDir(), "", ""), "2026-09-20T12:00:00Z", "2026-09-20T11:58:00Z"),
			want:   "not after the reset time",
		},
		{
			name:   "key outside resume grammar",
			params: withResumeKey(testResumeParams(t.TempDir(), resetAt, resumeAt), "codex-5h-wake-01a03a9e-10a0-7f11-801c-f04e5dbd5490"),
			want:   "invalid auto-resume automation key",
		},
		{
			name:   "missing task evidence",
			params: withResumeTask(testResumeParams(t.TempDir(), resetAt, resumeAt), ""),
			want:   "task evidence is incomplete",
		},
		{
			name:   "invalid parent thread",
			params: withResumeThread(testResumeParams(t.TempDir(), resetAt, resumeAt), "not-a-thread"),
			want:   "invalid parent thread ID",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			_, err := BuildAutoResumeTransaction(c.params, fixedDBReader(nil, nil))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error = %v want %q", err, c.want)
			}
		})
	}
}

func TestAutoResumeUpdateSpecUTCAnchorAcrossOffsets(t *testing.T) {
	cases := []struct {
		name       string
		resetAt    string
		resumeAt   string
		wantDT     string
		wantVerify string
	}{
		{
			name:       "UTC Z suffix",
			resetAt:    "2026-09-20T12:00:00Z",
			resumeAt:   "2026-09-20T12:02:00Z",
			wantDT:     "20260920T120200",
			wantVerify: "2026-09-20T12:02:00Z",
		},
		{
			name:       "JST positive offset",
			resetAt:    "2026-09-20T21:00:00+09:00",
			resumeAt:   "2026-09-20T21:02:00+09:00",
			wantDT:     "20260920T120200",
			wantVerify: "2026-09-20T21:02:00+09:00",
		},
		{
			name:       "JST offset crossing the UTC date",
			resetAt:    "2026-09-21T09:00:00+09:00",
			resumeAt:   "2026-09-21T09:02:00+09:00",
			wantDT:     "20260921T000200",
			wantVerify: "2026-09-21T09:02:00+09:00",
		},
		{
			name:       "CST Z.ai reset timezone",
			resetAt:    "2026-09-20T20:00:00+08:00",
			resumeAt:   "2026-09-20T20:02:00+08:00",
			wantDT:     "20260920T120200",
			wantVerify: "2026-09-20T20:02:00+08:00",
		},
		{
			name:       "negative offset",
			resetAt:    "2026-09-20T07:00:00-05:00",
			resumeAt:   "2026-09-20T07:02:00-05:00",
			wantDT:     "20260920T120200",
			wantVerify: "2026-09-20T07:02:00-05:00",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			output, err := BuildAutoResumeTransaction(testResumeParams(dir, c.resetAt, c.resumeAt), fixedDBReader(nil, nil))
			if err != nil {
				t.Fatal(err)
			}
			created := AdvanceAutoResumeTransaction(output.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
			if created.Status != AutoResumeStatusWriteRequired || created.Write == nil || created.Write.Mode != "update" {
				t.Fatalf("created = %#v", created)
			}
			wantRRule := "DTSTART:" + c.wantDT + "\nRRULE:FREQ=DAILY;COUNT=1"
			if created.Write.RRule != wantRRule {
				t.Fatalf("rrule = %q want %q", created.Write.RRule, wantRRule)
			}
			wantCommand := []string{"glm-worker", "--verify-auto-resume", testResumeKey, c.wantVerify}
			if strings.Join(created.VerifyCommand, " ") != strings.Join(wantCommand, " ") {
				t.Fatalf("verify command = %v want %v", created.VerifyCommand, wantCommand)
			}
			if !strings.Contains(created.Write.Prompt, "expected_task_id="+testResumeTaskID) {
				t.Fatalf("prompt = %q", created.Write.Prompt)
			}
		})
	}
}

func TestAdvanceAutoResumeTransactionVerifiesSavedEntity(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	created := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	if created.Status != AutoResumeStatusWriteRequired {
		t.Fatalf("created = %#v", created)
	}
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	row := dbRowAt("20260920T120200")
	row.ID = testResumeKey
	verified := AdvanceAutoResumeTransaction(created.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(map[string]DBRow{testResumeKey: row}, nil))
	if verified.Status != AutoResumeStatusVerified || verified.Token != "" {
		t.Fatalf("verified = %#v", verified)
	}
	if verified.Verification == nil || verified.Verification.Outcome != Pass {
		t.Fatalf("verification = %#v", verified.Verification)
	}
}

func TestAdvanceAutoResumeTransactionRejectsWrongAutomationID(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", "glm-worker-resume-other0000-99999999", "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "automation ID mismatch") {
		t.Fatalf("output = %#v", output)
	}
	if output.Cleanup == nil || output.Cleanup.AutomationID != "glm-worker-resume-other0000-99999999" {
		t.Fatalf("cleanup = %#v", output.Cleanup)
	}
}

func TestAdvanceAutoResumeTransactionRejectsExternalRefusal(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "automation create was refused", boolPtr(true))), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "isError") {
		t.Fatalf("output = %#v", output)
	}
}

func TestAdvanceAutoResumeExternalRefusalCarriesBoundedEvidence(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	message := "external safety review refused " + strings.Repeat("a", 300)
	payload := testResumeToolResponse(t, "create", "PAUSED", testResumeKey, message, boolPtr(true))
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(payload), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed {
		t.Fatalf("status = %s", output.Status)
	}
	if !strings.HasPrefix(output.Reason, automationIsErrorFallbackReason+": ") {
		t.Fatalf("reason = %q", output.Reason)
	}
	bounded := strings.TrimSuffix(strings.TrimPrefix(output.Reason, automationIsErrorFallbackReason+": "), autoResumeRefusalTruncationMark)
	if !strings.HasSuffix(output.Reason, autoResumeRefusalTruncationMark) ||
		utf8.RuneCountInString(bounded) != autoResumeRefusalReasonLimit ||
		!strings.Contains(bounded, "external safety review refused ") {
		t.Fatalf("bounded refusal = %q", output.Reason)
	}
	if strings.ContainsAny(output.Reason, "\n\t\x00") {
		t.Fatalf("refusal reason must stay single-line: %q", output.Reason)
	}
	if output.Authority == nil || validateAutoResumeAuthority(*output.Authority) != nil {
		t.Fatalf("failed output authority = %#v", output.Authority)
	}
	if output.Cleanup == nil || output.Cleanup.Mode != "delete" || output.Cleanup.AutomationID != testResumeKey {
		t.Fatalf("cleanup = %#v", output.Cleanup)
	}
}

func TestAdvanceAutoResumeRefusalReasonBoundsAndSanitizes(t *testing.T) {
	cases := []struct {
		name    string
		message string
		wantIn  string
		reject  string
	}{
		{
			name:    "control characters are replaced",
			message: "refused\nby\tsafety\x00review",
			wantIn:  "refused by safety review",
			reject:  "\n",
		},
		{
			name:    "short refusal is preserved verbatim",
			message: "automation create was refused",
			wantIn:  "automation create was refused",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			got := autoResumeRefusalReason(automationIsErrorFallbackReason, automationResponseFacts{Message: c.message})
			if !strings.Contains(got, c.wantIn) {
				t.Fatalf("reason = %q want %q", got, c.wantIn)
			}
			if c.reject != "" && strings.Contains(got, c.reject) {
				t.Fatalf("reason = %q must not contain %q", got, c.reject)
			}
		})
	}
}

func TestAdvanceAutoResumeRefusalWithoutMachineJSONStaysGeneric(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	payload := `{"isError":true,"content":[{"type":"text","text":"External safety review denied this request"}]}`
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(payload), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed {
		t.Fatalf("status = %s", output.Status)
	}
	if !strings.Contains(output.Reason, "is not machine JSON") {
		t.Fatalf("reason = %q", output.Reason)
	}
	if strings.Contains(output.Reason, "denied this request") {
		t.Fatalf("non-JSON refusal text must not be echoed: %q", output.Reason)
	}
	if output.Authority == nil || validateAutoResumeAuthority(*output.Authority) != nil {
		t.Fatalf("failed output authority = %#v", output.Authority)
	}
	if output.Cleanup != nil {
		t.Fatalf("cleanup = %#v", output.Cleanup)
	}
}

func TestAdvanceAutoResumeAcceptsObservedTwoBlockResponse(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	created := AdvanceAutoResumeTransaction(
		plan.Token,
		[]byte(testResumeObservedResponse(t, "Created automation in the app.", map[string]string{
			"automationId": testResumeKey,
			"mode":         "create",
			"status":       "PAUSED",
		}, false)),
		dir, "unused", fixedDBReader(nil, nil))
	if created.Status != AutoResumeStatusWriteRequired || created.Stage != stageUpdateOneShot || created.Write == nil {
		t.Fatalf("created = %#v", created)
	}

	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	row := dbRowAt("20260920T120200")
	row.ID = testResumeKey
	verified := AdvanceAutoResumeTransaction(
		created.Token,
		[]byte(testResumeObservedResponse(t, "Updated automation in the app.", map[string]string{
			"automationId": testResumeKey,
			"mode":         "update",
			"status":       "ACTIVE",
		}, false)),
		dir, "unused", fixedDBReader(map[string]DBRow{testResumeKey: row}, nil))
	if verified.Status != AutoResumeStatusVerified || verified.Verification == nil || verified.Verification.Outcome != Pass {
		t.Fatalf("verified = %#v", verified)
	}
}

func TestAdvanceAutoResumeTwoBlockResponseFailures(t *testing.T) {
	created := "Created automation in the app."
	facts := testResumeFactsJSON(t, map[string]string{"automationId": testResumeKey, "mode": "create", "status": "PAUSED"})
	cases := []struct {
		name    string
		payload string
		wantIn  string
	}{
		{
			name:    "prose only without machine JSON",
			payload: testResumeMultiBlockResponse(t, false, created),
			wantIn:  "is not machine JSON",
		},
		{
			name:    "duplicate machine JSON blocks",
			payload: testResumeMultiBlockResponse(t, false, created, facts, facts),
			wantIn:  "ambiguous machine JSON payloads",
		},
		{
			name:    "machine JSON missing the automation ID",
			payload: testResumeMultiBlockResponse(t, false, created, testResumeFactsJSON(t, map[string]string{"mode": "create", "status": "PAUSED"})),
			wantIn:  "missing automation ID",
		},
		{
			name:    "machine JSON without any message source",
			payload: testResumeMultiBlockResponse(t, false, facts),
			wantIn:  "missing an unambiguous success or failure message",
		},
		{
			name:    "machine message plus extra prose payload",
			payload: testResumeMultiBlockResponse(t, false, created, testResumeFactsJSON(t, map[string]string{"automationId": testResumeKey, "mode": "create", "status": "PAUSED", "message": "created successfully"})),
			wantIn:  "extra non-JSON payloads",
		},
		{
			name:    "machine message with two prose payloads",
			payload: testResumeMultiBlockResponse(t, false, "created successfully", created, testResumeFactsJSON(t, map[string]string{"automationId": testResumeKey, "mode": "create", "status": "PAUSED", "message": "created successfully"})),
			wantIn:  "ambiguous non-JSON payloads",
		},
		{
			name:    "message-less machine JSON with two prose payloads",
			payload: testResumeMultiBlockResponse(t, false, created, created, facts),
			wantIn:  "ambiguous non-JSON payloads",
		},
		{
			name:    "observed facts bound to the wrong mode",
			payload: testResumeMultiBlockResponse(t, false, created, testResumeFactsJSON(t, map[string]string{"automationId": testResumeKey, "mode": "update", "status": "ACTIVE"})),
			wantIn:  "automation mode mismatch",
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			plan := testResumePlanOutput(t, dir)
			output := AdvanceAutoResumeTransaction(plan.Token, []byte(c.payload), dir, "unused", fixedDBReader(nil, nil))
			if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, c.wantIn) {
				t.Fatalf("output = %#v want reason %q", output, c.wantIn)
			}
			if output.Authority == nil || validateAutoResumeAuthority(*output.Authority) != nil {
				t.Fatalf("failed output authority = %#v", output.Authority)
			}
		})
	}
}

func TestAdvanceAutoResumeObservedTwoBlockRefusalCarriesBoundedReason(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	payload := testResumeObservedResponse(t, "External safety review refused this automation", map[string]string{
		"automationId": testResumeKey,
		"mode":         "create",
		"status":       "PAUSED",
	}, true)
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(payload), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed {
		t.Fatalf("status = %s", output.Status)
	}
	if !strings.Contains(output.Reason, automationIsErrorFallbackReason+": External safety review refused this automation") {
		t.Fatalf("reason = %q", output.Reason)
	}
	if output.Authority == nil || validateAutoResumeAuthority(*output.Authority) != nil {
		t.Fatalf("failed output authority = %#v", output.Authority)
	}
}

func testResumeObservedResponse(t *testing.T, prose string, facts map[string]string, isError bool) string {
	t.Helper()
	return testResumeMultiBlockResponse(t, isError, prose, testResumeFactsJSON(t, facts))
}

func testResumeMultiBlockResponse(t *testing.T, isError bool, blocks ...string) string {
	t.Helper()
	content := make([]map[string]string, 0, len(blocks))
	for _, block := range blocks {
		content = append(content, map[string]string{"type": "text", "text": block})
	}
	envelope, err := json.Marshal(map[string]any{"isError": isError, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return string(envelope)
}

func testResumeFactsJSON(t *testing.T, facts map[string]string) string {
	t.Helper()
	encoded, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestAdvanceAutoResumeTransactionRejectsAmbiguousResponse(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(`{"content":[{"type":"text","text":"{}"}]}`), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "isError") {
		t.Fatalf("output = %#v", output)
	}
}

func TestAdvanceAutoResumeTransactionRejectsSuggestionResponse(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	output := AdvanceAutoResumeTransaction(plan.Token, []byte(`{"isError":false,"content":[{"type":"text","text":"Rendered suggestion card"}]}`), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "suggestion") {
		t.Fatalf("output = %#v", output)
	}
}

func TestAdvanceAutoResumeTransactionRetriesVerifyFailureOnceThenCleansUp(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	created := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:19990101T000000\nRRULE:FREQ=DAILY;COUNT=1")
	row := dbRowAt("19990101T000000")
	row.ID = testResumeKey
	retry := AdvanceAutoResumeTransaction(created.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(map[string]DBRow{testResumeKey: row}, nil))
	if retry.Status != AutoResumeStatusWriteRequired || retry.Attempt != 2 || !strings.Contains(retry.Reason, "saved-state verification failed") {
		t.Fatalf("retry = %#v", retry)
	}
	failed := AdvanceAutoResumeTransaction(retry.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(map[string]DBRow{testResumeKey: row}, nil))
	if failed.Status != AutoResumeStatusFailed || !strings.Contains(failed.Reason, "saved-state verification failed") {
		t.Fatalf("failed = %#v", failed)
	}
	if failed.Cleanup == nil || failed.Cleanup.Mode != "delete" || failed.Cleanup.AutomationID != testResumeKey {
		t.Fatalf("cleanup = %#v", failed.Cleanup)
	}
}

func TestAdvanceAutoResumeTransactionExistingAutomationFailureKeepsAutomation(t *testing.T) {
	dir := t.TempDir()
	resetAt, resumeAt := testResumeTimes()
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	plan, err := BuildAutoResumeTransaction(testResumeParams(dir, resetAt, resumeAt), fixedDBReader(nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Stage != stageUpdateOneShot {
		t.Fatalf("stage = %s", plan.Stage)
	}
	retry := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	if retry.Status != AutoResumeStatusWriteRequired {
		t.Fatalf("retry = %#v", retry)
	}
	failed := AdvanceAutoResumeTransaction(retry.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	if failed.Status != AutoResumeStatusFailed || failed.Cleanup != nil {
		t.Fatalf("failed = %#v", failed)
	}
}

func TestAdvanceAutoResumeTransactionRejectsNonUTCSchedulesAsVerified(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	created := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	rrules := []string{
		"DTSTART;TZID=Asia/Tokyo:20260920T210200\nRRULE:FREQ=DAILY;COUNT=1",
		"RRULE:FREQ=DAILY;BYHOUR=14;COUNT=1",
		"DTSTART:20260920T120200\nRRULE:FREQ=HOURLY",
	}
	for _, rrule := range rrules {
		rrule := rrule
		t.Run(rrule, func(t *testing.T) {
			writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, rrule)
			row := DBRow{ID: testResumeKey, Status: "ACTIVE", Rrule: rrule, NextRunAt: time.Date(2026, 9, 20, 12, 2, 0, 0, time.UTC).UnixMilli(), HasNextRun: true}
			retry := AdvanceAutoResumeTransaction(created.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(map[string]DBRow{testResumeKey: row}, nil))
			if retry.Status != AutoResumeStatusWriteRequired || !strings.Contains(retry.Reason, "saved-state verification failed") {
				t.Fatalf("retry = %#v", retry)
			}
		})
	}
}

func TestAdvanceAutoResumeTransactionVerificationUnavailableFailsClosed(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	created := AdvanceAutoResumeTransaction(plan.Token, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	writeResumeTOML(t, dir, testResumeKey, "ACTIVE", testResumeThread, "DTSTART:20260920T120200\nRRULE:FREQ=DAILY;COUNT=1")
	output := AdvanceAutoResumeTransaction(created.Token, []byte(testResumeToolResponse(t, "update", "ACTIVE", testResumeKey, "updated successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, ErrSqlite3NotFound))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "verification unavailable") {
		t.Fatalf("output = %#v", output)
	}
	if output.Attempt != 1 {
		t.Fatalf("unavailable verification must not retry: %#v", output)
	}
}

func TestAdvanceAutoResumeTransactionRejectsForgedCoalesceDecision(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	forged := reSignAutoResumeTransaction(t, plan.Token, map[string]any{"coalesce_decision": DecisionCoalesce})
	output := AdvanceAutoResumeTransaction(forged, []byte(testResumeToolResponse(t, "create", "PAUSED", testResumeKey, "created successfully", boolPtr(false))), dir, "unused", fixedDBReader(nil, nil))
	if output.Status != AutoResumeStatusFailed || !strings.Contains(output.Reason, "coalesce decision") {
		t.Fatalf("output = %#v", output)
	}
}

func TestAutoResumeAuthorityValidationFailsClosed(t *testing.T) {
	cases := []struct {
		name      string
		authority AutoResumeAuthority
	}{
		{name: "empty permission", authority: AutoResumeAuthority{Continuation: autoResumeAuthorityContinuation, GLMScope: autoResumeAuthorityGLMScope}},
		{name: "empty continuation", authority: AutoResumeAuthority{Permission: autoResumeAuthorityPermission, GLMScope: autoResumeAuthorityGLMScope}},
		{name: "empty glm scope", authority: AutoResumeAuthority{Permission: autoResumeAuthorityPermission, Continuation: autoResumeAuthorityContinuation}},
		{
			name: "scope confusion to a general GLM prohibition",
			authority: AutoResumeAuthority{
				Permission:   autoResumeAuthorityPermission,
				Continuation: autoResumeAuthorityContinuation,
				GLMScope:     "GLM execution is prohibited",
			},
		},
		{
			name: "permission without the permanent grant",
			authority: AutoResumeAuthority{
				Permission:   "IMPLEMENTATION_RULES.md mentions automations",
				Continuation: autoResumeAuthorityContinuation,
				GLMScope:     autoResumeAuthorityGLMScope,
			},
		},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateAutoResumeAuthority(c.authority); err == nil {
				t.Fatalf("authority = %#v accepted", c.authority)
			}
		})
	}
	if err := validateAutoResumeAuthority(autoResumeAuthoritySpec()); err != nil {
		t.Fatalf("canonical authority rejected: %v", err)
	}
}

func TestAutoResumeWriteSpecValidationFailsClosed(t *testing.T) {
	dir := t.TempDir()
	plan := testResumePlanOutput(t, dir)
	transaction := autoResumeTransaction{
		TaskID:               testResumeTaskID,
		RepoRoot:             testResumeRepo,
		ParentThreadID:       testResumeThread,
		ExpectedAutomationID: testResumeKey,
		ResetAtRFC3339:       plan.ResetAtRFC3339,
		ResumeAtRFC3339:      plan.ResumeAtRFC3339,
		Nonce:                strings.Repeat("a", 32),
		CoalesceDecision:     DecisionCreateGLMWake,
		Attempt:              1,
	}
	base := autoResumeUpdateSpec(transaction)
	cases := []struct {
		name   string
		mutate func(AutoResumeWriteSpec) AutoResumeWriteSpec
	}{
		{name: "prompt dropped", mutate: func(s AutoResumeWriteSpec) AutoResumeWriteSpec { s.Prompt = ""; return s }},
		{name: "prompt without authority", mutate: func(s AutoResumeWriteSpec) AutoResumeWriteSpec { s.Prompt = "GLM 5h auto-resume trigger."; return s }},
		{name: "wrong target thread", mutate: func(s AutoResumeWriteSpec) AutoResumeWriteSpec {
			s.TargetThreadID = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
			return s
		}},
		{name: "TZID rrule", mutate: func(s AutoResumeWriteSpec) AutoResumeWriteSpec {
			s.RRule = "DTSTART;TZID=Asia/Tokyo:20260920T210200\nRRULE:FREQ=DAILY;COUNT=1"
			return s
		}},
		{name: "PAUSED update", mutate: func(s AutoResumeWriteSpec) AutoResumeWriteSpec { s.Status = "PAUSED"; return s }},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			if err := validateAutoResumeWriteSpec(c.mutate(base), transaction); err == nil {
				t.Fatalf("mutated spec accepted")
			}
		})
	}
	if err := validateAutoResumeWriteSpec(base, transaction); err != nil {
		t.Fatalf("canonical update spec rejected: %v", err)
	}
	if err := validateAutoResumeWriteSpec(autoResumeCreateSpec(transaction), transaction); err != nil {
		t.Fatalf("canonical create spec rejected: %v", err)
	}
}

func reSignAutoResumeTransaction(t *testing.T, token string, overrides map[string]any) string {
	t.Helper()
	encoded, _, ok := strings.Cut(token, ".")
	if !ok {
		t.Fatal("token has no checksum")
	}
	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		t.Fatal(err)
	}
	for key, override := range overrides {
		value[key] = override
	}
	forged, _ := encodeSignedTransaction(value)
	return forged
}

func boolPtr(value bool) *bool {
	return &value
}

func withResumeTimes(params AutoResumePlanParams, resetAt, resumeAt string) AutoResumePlanParams {
	params.ResetAtRFC3339 = resetAt
	params.ResumeAtRFC3339 = resumeAt
	return params
}

func withResumeKey(params AutoResumePlanParams, key string) AutoResumePlanParams {
	params.AutomationKey = key
	return params
}

func withResumeTask(params AutoResumePlanParams, taskID string) AutoResumePlanParams {
	params.TaskID = taskID
	return params
}

func withResumeThread(params AutoResumePlanParams, threadID string) AutoResumePlanParams {
	params.ParentThreadID = threadID
	return params
}
