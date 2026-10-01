package app

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
)

func TestRunEntryControllerSemanticUsesExplicitLiveAuthority(t *testing.T) {
	root := t.TempDir()
	runControllerActivationGit(t, root, "init", "-q")
	runControllerActivationGit(t, root, "config", "user.email", "controller-semantic@example.invalid")
	runControllerActivationGit(t, root, "config", "user.name", "Controller Semantic Test")
	writeAppTestFile(t, root, repositoryharness.MarkerPath, repositoryharness.MarkerContent)
	writeAppTestFile(t, root, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n")
	writeAppTestFile(t, root, "IMPLEMENTATION_TASKS/root.md", "# root\n\n## Contract\n\ncontroller semantic task\n\n## Dependencies\n\nnone\n")
	runControllerActivationGit(t, root, "add", ".")
	runControllerActivationGit(t, root, "commit", "-q", "-m", "base")

	stateBase := filepath.Join(t.TempDir(), "state", "sessions")
	hash := config.RepoHashFor(root)
	cfg := config.AppConfig{RepoRoot: root, RepoHash: hash, RepoShort: hash[:12], StateBase: stateBase}
	loadConfig := func() (config.AppConfig, error) { return cfg, nil }

	var activation bytes.Buffer
	if err := runEntry(
		[]string{"--authority", "controller-activate"},
		loadConfig,
		nil,
		bytes.NewReader(nil),
		&activation,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}

	observe := controllerSemanticCommand{
		Action: controllerSemanticObserveLive,
		Observation: &controller.FindingObservationInput{
			Producer:   "controller-semantic-test",
			ProofClass: controller.FindingProofUnverified,
			ProblemKey: "controller-semantic-problem",
		},
	}
	observed := runControllerSemanticTestCommand(t, loadConfig, observe)
	if observed.Finding == nil || observed.Finding.FindingID == "" {
		t.Fatalf("semantic observation output is incomplete: %#v", observed)
	}

	resolve := controllerSemanticCommand{
		Action:    controllerSemanticResolve,
		FindingID: observed.Finding.FindingID,
		Decision:  &controller.FindingDecision{Kind: controller.FindingDecisionAmbiguous},
	}
	resolved := runControllerSemanticTestCommand(t, loadConfig, resolve)
	if resolved.Result == nil || resolved.Result.Intent != controller.FindingIntentAwaitingDisposition {
		t.Fatalf("semantic resolution output is incomplete: %#v", resolved)
	}
}

func runControllerSemanticTestCommand(
	t *testing.T,
	loadConfig func() (config.AppConfig, error),
	command controllerSemanticCommand,
) controllerSemanticOutput {
	t.Helper()
	payload, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runEntry(
		[]string{"--authority", "controller-semantic"},
		loadConfig,
		nil,
		bytes.NewReader(payload),
		&stdout,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	var output controllerSemanticOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	return output
}
