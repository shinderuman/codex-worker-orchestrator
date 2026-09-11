package parentaction

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLiveTokensReturnsCanonicalPreparedToken(t *testing.T) {
	repoRoot := t.TempDir()
	prepared, err := Prepare(repoRoot, string(ActionDecision))
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := LiveTokens(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != prepared.Token {
		t.Fatalf("tokens = %#v", tokens)
	}
}

func TestLiveTokensFailsClosedOnCorruptCanonicalRecord(t *testing.T) {
	repoRoot := t.TempDir()
	prepared, err := Prepare(repoRoot, string(ActionDecision))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prepared.Path, []byte("corrupt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LiveTokens(repoRoot); err == nil {
		t.Fatal("corrupt canonical staging record was ignored")
	}
}

func TestLiveTokensIgnoresUnrelatedStageFiles(t *testing.T) {
	repoRoot := t.TempDir()
	stageDir := filepath.Join(repoRoot, StageDirName)
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stageDir, "notes.txt"), []byte("not a staging record"), 0o600); err != nil {
		t.Fatal(err)
	}
	tokens, err := LiveTokens(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Fatalf("tokens = %#v", tokens)
	}
}
