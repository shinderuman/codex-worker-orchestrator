package app

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestControllerExecutionMachineDispatchPreservesAuthorityOnInvalidInput(t *testing.T) {
	root := t.TempDir()
	runControllerActivationGit(t, root, "init", "-q")
	runControllerActivationGit(t, root, "config", "user.email", "controller-execution@example.invalid")
	runControllerActivationGit(t, root, "config", "user.name", "Controller Execution Test")
	writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\nexecution task\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, root, "add", ".")
	runControllerActivationGit(t, root, "commit", "-qm", "base")
	hash := config.RepoHashFor(root)
	cfg := config.AppConfig{RepoRoot: root, RepoHash: hash, RepoShort: hash[:12], StateBase: filepath.Join(t.TempDir(), "state", "sessions")}
	load := func() (config.AppConfig, error) { return cfg, nil }
	if err := runEntry([]string{"--authority", "controller-activate"}, load, nil, strings.NewReader(""), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{`{"action":"materialize","unknown":true}`, `{"action":"cleanup"}{}`, `{"action":"materialize","expected_generation":999}`, `{"action":"recover"}`} {
		var output bytes.Buffer
		if err := runEntry([]string{"--authority", "controller-execution"}, load, nil, strings.NewReader(payload), &output, io.Discard); err == nil {
			t.Fatalf("invalid machine command accepted: %s", payload)
		}
		if output.Len() != 0 {
			t.Fatal("failed machine command emitted success payload")
		}
	}
	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	old, _ := json.Marshal(before)
	current, _ := json.Marshal(after)
	if !bytes.Equal(old, current) {
		t.Fatal("invalid command changed controller authority")
	}
}
