package parentaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareConsumePreservesDecisionPayloadAndRemovesSlot(t *testing.T) {
	repo := t.TempDir()
	prepared, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Action != "decision" || !validToken(prepared.Token) {
		t.Fatalf("prepared = %#v", prepared)
	}
	if filepath.Dir(prepared.Path) != filepath.Join(repo, StageDirName) {
		t.Fatalf("staging path escaped repository slot: %q", prepared.Path)
	}
	payload := []byte("line1\n`$'\"\x00tail\n")
	writePreparedPayload(t, prepared, payload)
	got, err := Consume(repo, "decision", prepared.Token)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload mismatch: %q", got)
	}
	if _, err := os.Lstat(prepared.Path); !os.IsNotExist(err) {
		t.Fatalf("consumed staging file still exists: %v", err)
	}
}

func TestPrepareAcceptsMilestonePayloadActions(t *testing.T) {
	for _, action := range []string{"start-milestones", "revise-milestones"} {
		repo := t.TempDir()
		prepared, err := Prepare(repo, action)
		if err != nil {
			t.Fatalf("%s prepare failed: %v", action, err)
		}
		payload := []byte(`{"milestones":[{"id":"a","scope":"a","acceptance":"a"},{"id":"b","scope":"b","acceptance":"b"}]}`)
		writePreparedPayload(t, prepared, payload)
		got, err := Consume(repo, action, prepared.Token)
		if err != nil {
			t.Fatalf("%s consume failed: %v", action, err)
		}
		if string(got) != string(payload) {
			t.Fatalf("%s payload mismatch: %q", action, got)
		}
	}
}

func TestPrepareRejectsStartStaging(t *testing.T) {
	if _, err := Prepare(t.TempDir(), "start"); err == nil {
		t.Fatal("start unexpectedly uses file staging")
	}
}

func TestPrepareUsesUniqueActionBoundTokens(t *testing.T) {
	repo := t.TempDir()
	first, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	if first.Token == second.Token || first.Path == second.Path {
		t.Fatalf("prepare reused staging identity: %#v %#v", first, second)
	}
	if _, err := Consume(repo, "fix", first.Token); err == nil {
		t.Fatal("decision token was accepted as fix token")
	}
}

func TestConsumeRejectsUnwrittenOrForgedSlots(t *testing.T) {
	repo := t.TempDir()
	prepared, err := Prepare(repo, "fix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Consume(repo, "fix", prepared.Token); err == nil {
		t.Fatal("placeholder payload was accepted")
	}
	if _, err := Consume(repo, "fix", "../../etc/passwd"); err == nil {
		t.Fatal("path-like token was accepted")
	}
}

func TestConsumeRejectsRegularFileWithoutIssuedTokenBinding(t *testing.T) {
	repo := t.TempDir()
	prepared, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prepared.Path, []byte("unrelated local file content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Consume(repo, "decision", prepared.Token); err == nil {
		t.Fatal("unbound regular file was accepted")
	}
}

func TestConsumeRejectsSymlinkReplacement(t *testing.T) {
	repo := t.TempDir()
	prepared, err := Prepare(repo, "decision")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(prepared.Path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, prepared.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := Consume(repo, "decision", prepared.Token); err == nil {
		t.Fatal("symlink staging file was accepted")
	}
}

func TestPrepareRejectsSymlinkStageDirectory(t *testing.T) {
	repo := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(repo, StageDirName)); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(repo, "decision"); err == nil {
		t.Fatal("symlink staging directory was accepted")
	}
}

func writePreparedPayload(t *testing.T, prepared Prepared, payload []byte) {
	t.Helper()
	content := append([]byte(tokenHeader(prepared.Token)), payload...)
	if err := os.WriteFile(prepared.Path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}
