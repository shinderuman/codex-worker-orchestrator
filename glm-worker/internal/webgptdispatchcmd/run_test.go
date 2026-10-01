package webgptdispatchcmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunMailboxEmitsValidatedOutputsAndEvidence(t *testing.T) {
	tempDir := t.TempDir()
	bodyPath := filepath.Join(tempDir, "body.json")
	outputPath := filepath.Join(tempDir, "github-output")
	resultPath := filepath.Join(tempDir, "result.json")
	body := `{"version":1,"request_id":"req-1","operation":"validate","target_branch":"web-gpt/example","expected_head_sha":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","validation_mode":"go-package-test","package_scope":"./internal/workflow"}`
	if err := os.WriteFile(bodyPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"mailbox", bodyPath, "1236", "shinderuman",
		strings.Repeat("b", 40), outputPath, resultPath,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("mailbox command failed: code=%d stderr=%q", code, stderr.String())
	}
	outputs, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{
		"request_id=req-1\n",
		"operation=validate\n",
		"target_branch=web-gpt/example\n",
		"expected_head_sha=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n",
		"validation_mode=go-package-test\n",
		"package_scope=./internal/workflow\n",
	} {
		if !strings.Contains(string(outputs), token) {
			t.Fatalf("missing github output %q in %q", token, outputs)
		}
	}
	var result transportResult
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "accepted" || result.MailboxIssue != 1236 || result.RequestID != "req-1" || result.ControlSHA != strings.Repeat("b", 40) {
		t.Fatalf("unexpected transport result: %+v", result)
	}
}

func TestRunMailboxRejectsUnauthorizedActorBeforeOutputs(t *testing.T) {
	tempDir := t.TempDir()
	resultPath := filepath.Join(tempDir, "result.json")
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"mailbox", filepath.Join(tempDir, "missing-body"), "1236", "someone-else",
		strings.Repeat("b", 40), filepath.Join(tempDir, "github-output"), resultPath,
	}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected rejection, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unauthorized_actor") {
		t.Fatalf("missing rejection reason: %q", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(tempDir, "github-output")); !os.IsNotExist(err) {
		t.Fatalf("unauthorized request must not expose operation outputs: err=%v", err)
	}
}

func TestRunFieldsUsesCanonicalRequestPolicy(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run([]string{
		"fields", "validate", "web-gpt/example", strings.Repeat("a", 40),
		"go-package-test", "../outside",
	}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "invalid_package_scope") {
		t.Fatalf("unsafe scope was not rejected: code=%d stderr=%q", code, stderr.String())
	}
}
