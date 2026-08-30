package parentaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareConsumePreservesPayloadAndRemovesSlot(t *testing.T) {
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
	if err := os.WriteFile(prepared.Path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
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
