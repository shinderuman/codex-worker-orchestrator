package state

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestRuntimeInstallEvidenceRoundTripAndTaskReset(t *testing.T) {
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("a", 64),
		StateBase: filepath.Join(root, "state"),
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	evidence := RuntimeInstallEvidence{
		Version:           runtimeInstallEvidenceVersion,
		TaskID:            taskID,
		Head:              strings.Repeat("b", 40),
		SourceDigest:      strings.Repeat("c", 64),
		InstalledRevision: strings.Repeat("b", 40),
		SmokeResult:       ValidationResultPass,
	}
	if err := st.SaveRuntimeInstallEvidence(evidence); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.LoadRuntimeInstallEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if loaded != evidence {
		t.Fatalf("loaded evidence = %#v want %#v", loaded, evidence)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if st.Exists(runtimeInstallEvidenceFile) {
		t.Fatal("runtime install evidence survived a new task transition")
	}
}

func TestRuntimeInstallEvidenceRejectsMismatchedTaskAndRevision(t *testing.T) {
	root := t.TempDir()
	cfg := config.AppConfig{RepoRoot: root, RepoHash: strings.Repeat("d", 64), StateBase: filepath.Join(root, "state")}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := RuntimeInstallEvidence{
		Version:           runtimeInstallEvidenceVersion,
		TaskID:            taskID,
		Head:              "head-a",
		SourceDigest:      "source",
		InstalledRevision: "head-a",
		SmokeResult:       ValidationResultPass,
	}
	mismatchedTask := base
	mismatchedTask.TaskID = "00000000-0000-4000-8000-000000000000"
	if err := st.SaveRuntimeInstallEvidence(mismatchedTask); err == nil {
		t.Fatal("mismatched task evidence was accepted")
	}
	mismatchedRevision := base
	mismatchedRevision.InstalledRevision = "head-b"
	if err := st.SaveRuntimeInstallEvidence(mismatchedRevision); err == nil {
		t.Fatal("mismatched installed revision was accepted")
	}
}
