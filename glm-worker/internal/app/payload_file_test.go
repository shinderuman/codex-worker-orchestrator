package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func writePayloadFile(t *testing.T, payload string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "parent-action.txt")
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseCommandFilePayloadModes(t *testing.T) {
	decision, err := ParseCommand([]string{"--decision-file", "/tmp/decision.txt"})
	if err != nil || decision.Mode != ModeDecision || decision.PayloadFile != "/tmp/decision.txt" {
		t.Fatalf("decision = %#v, err = %v", decision, err)
	}
	fix, err := ParseCommand([]string{"--fix-file", "/tmp/fix.txt", "--origin", "codex-review", "--accepted-scope", "current-diff"})
	if err != nil || fix.Mode != ModeFix || fix.PayloadFile != "/tmp/fix.txt" || fix.Origin != "codex-review" || fix.AcceptedScope != "current-diff" {
		t.Fatalf("fix = %#v, err = %v", fix, err)
	}
	for _, args := range [][]string{{"--decision-file"}, {"--decision-file", ""}, {"--decision-file", "/tmp/x", "--origin", "codex-review"}, {"--fix-file"}, {"--fix-file", "/tmp/x", "--origin", "invalid"}} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("invalid args accepted: %#v", args)
		}
	}
}

func TestRunDecisionFilePreservesPayloadBytes(t *testing.T) {
	cfg := newAppConfig(t)
	prepareWaitingDecisionState(t, cfg)
	payload := stdinTestPayload() + "\x00tail"
	path := writePayloadFile(t, payload)
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("decision applied")}, {structured: needsSolReviewPacketApp()}}}
	if err := runStdinPayload(t, cfg, r, []string{"--decision-file", path}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	st := state.AttachStateStore(cfg)
	persisted, err := os.ReadFile(st.Path("last-decision"))
	if err != nil {
		t.Fatal(err)
	}
	if string(persisted) != payload+"\n" {
		t.Fatalf("last-decision mismatch: %q", persisted)
	}
	decision := promptSection(t, r.prompts[0], "\nSOL_DECISION:\n", "\n\n直前の同一タスクの調査文脈を利用し")
	if decision != payload {
		t.Fatalf("decision payload mismatch: %q", decision)
	}
}

func TestRunFixFilePreservesPayloadBytes(t *testing.T) {
	cfg := newAppConfig(t)
	prepareWaitingSolReviewState(t, cfg)
	payload := stdinTestPayload() + "\x00tail"
	path := writePayloadFile(t, payload)
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("fix applied")}, {structured: needsSolReviewPacketApp()}}}
	if err := runStdinPayload(t, cfg, r, []string{"--fix-file", path, "--origin", "codex-review"}, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}
	feedback := promptSection(t, r.prompts[0], "\nREVIEW_FEEDBACK:\n", "\n\n同一タスクの既存文脈を利用し")
	if feedback != payload {
		t.Fatalf("fix payload mismatch: %q", feedback)
	}
}

func TestRunDecisionFileFailsClosedBeforeStateChange(t *testing.T) {
	cfg := newAppConfig(t)
	prepareWaitingDecisionState(t, cfg)
	r := &fakeRunner{}
	before := snapshotStateFiles(t, cfg)
	missing := filepath.Join(t.TempDir(), "missing.txt")
	err := runStdinPayload(t, cfg, r, []string{"--decision-file", missing}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "payload file read failed") {
		t.Fatalf("missing file should fail closed: %v", err)
	}
	assertFailClosedStateUnchanged(t, cfg, before, r, state.TaskStatusWaitingDecision)
	empty := writePayloadFile(t, "")
	err = runStdinPayload(t, cfg, r, []string{"--decision-file", empty}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "payload file is empty") {
		t.Fatalf("empty file should fail closed: %v", err)
	}
	assertFailClosedStateUnchanged(t, cfg, before, r, state.TaskStatusWaitingDecision)
}
