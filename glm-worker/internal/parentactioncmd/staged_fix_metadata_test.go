package parentactioncmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
)

func TestPrepareFixBindsValidatedMetadataIntoNextCommand(t *testing.T) {
	repoRoot := t.TempDir()
	var output bytes.Buffer
	options := []string{"--origin", "glm-reviewer", "--cause", "cross-cutting-invariant"}
	if err := prepare(repoRoot, append([]string{"prepare", "fix"}, options...), &output); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Token       string   `json:"token"`
		NextCommand []string `json:"next_command"`
	}
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"glm-parent-action", "fix", prepared.Token}, options...)
	if !reflect.DeepEqual(prepared.NextCommand, want) {
		t.Fatalf("next_command = %#v want %#v", prepared.NextCommand, want)
	}
}

func TestPrepareFixWithoutMetadataKeepsExistingPath(t *testing.T) {
	repoRoot := t.TempDir()
	var output bytes.Buffer
	if err := prepare(repoRoot, []string{"prepare", "fix"}, &output); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Token       string   `json:"token"`
		NextCommand []string `json:"next_command"`
	}
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	want := []string{"glm-parent-action", "fix", prepared.Token}
	if !reflect.DeepEqual(prepared.NextCommand, want) {
		t.Fatalf("next_command = %#v want %#v", prepared.NextCommand, want)
	}
}

func TestPrepareFixPreservesAcceptedScopeInNextCommand(t *testing.T) {
	repoRoot := t.TempDir()
	var output bytes.Buffer
	options := []string{"--accepted-scope", "current-diff", "--origin", "glm-reviewer", "--cause", "cross-cutting-invariant"}
	if err := prepare(repoRoot, append([]string{"prepare", "fix"}, options...), &output); err != nil {
		t.Fatal(err)
	}
	var prepared struct {
		Token       string   `json:"token"`
		NextCommand []string `json:"next_command"`
	}
	if err := json.Unmarshal(output.Bytes(), &prepared); err != nil {
		t.Fatal(err)
	}
	want := append([]string{"glm-parent-action", "fix", prepared.Token}, options...)
	if !reflect.DeepEqual(prepared.NextCommand, want) {
		t.Fatalf("next_command = %#v want %#v", prepared.NextCommand, want)
	}
}

func TestPrepareFixRejectsInvalidMetadataBeforeStaging(t *testing.T) {
	for _, options := range [][]string{
		{"--origin", "invented"},
		{"--cause", "invented"},
		{"--accepted-scope", "other"},
	} {
		repoRoot := t.TempDir()
		var output bytes.Buffer
		if err := prepare(repoRoot, append([]string{"prepare", "fix"}, options...), &output); err == nil {
			t.Fatalf("invalid metadata was accepted: %#v", options)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, parentaction.StageDirName)); !os.IsNotExist(err) {
			t.Fatalf("invalid metadata created staging state for %#v: %v", options, err)
		}
	}
}

func TestExecuteStagedFixForwardsOriginAndCauseLosslessly(t *testing.T) {
	cfg, _ := newParentActionTestState(t)
	payload := []byte("apply reviewer finding")
	token := prepareParentPayload(t, cfg, string(parentaction.ActionFix), payload)
	descriptor, ok := parentaction.LookupPayloadAction(string(parentaction.ActionFix))
	if !ok {
		t.Fatal("fix payload descriptor missing")
	}
	marker := writeParentActionWorkerStubWithCheck(t, cfg,
		`test "$1" = "--fix-stdin" && test "$5" = "--origin" && test "$6" = "glm-reviewer" && test "$7" = "--cause" && test "$8" = "cross-cutting-invariant"`)
	if err := executePayloadAction(
		cfg.RepoRoot,
		descriptor,
		[]string{token, "--origin", "glm-reviewer", "--cause", "cross-cutting-invariant"},
		os.Stdout,
		os.Stderr,
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("staged fix metadata did not reach glm-worker")
	}
}
