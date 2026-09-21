package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/authoritybootstrapcmd"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestRunEntryAuthorityBootstrapBypassesConfigAndReturnsAtomicProjection(t *testing.T) {
	root := t.TempDir()
	if output, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	if output, err := exec.Command("git", "-C", root, "add", "--", repositoryharness.MarkerPath).CombinedOutput(); err != nil {
		t.Fatalf("git add marker: %v: %s", err, output)
	}
	writeAppTestFile(t, root, "IMPLEMENTATION_RULES.md", "rules-body\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/current.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/current.md", "task-body\n")

	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	var stdout bytes.Buffer
	err = runEntry(
		[]string{"--authority", "bootstrap"},
		func() (config.AppConfig, error) {
			t.Fatal("authority bootstrap loaded repository config")
			return config.AppConfig{}, nil
		},
		nil,
		bytes.NewReader(nil),
		&stdout,
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateSingleMachineJSONObject(stdout.Bytes()); err != nil {
		t.Fatalf("authority bootstrap stdout violates machine contract: %v", err)
	}
	var output authoritybootstrapcmd.BootstrapOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.AuthoritySnapshotSHA256 == "" || output.ActiveTask != "IMPLEMENTATION_TASKS/current.md" {
		t.Fatalf("authority bootstrap output = %#v", output)
	}
	if output.Rules.Content != "rules-body\n" || output.Active.Content != "task-body\n" {
		t.Fatalf("authority bootstrap parts = %#v", output)
	}
}
