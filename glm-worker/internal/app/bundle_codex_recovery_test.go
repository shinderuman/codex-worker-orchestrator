package app

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBundleExplicitParentThreadIDAssociatesLegacyTaskWithoutMutatingStats(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	start := stats.StartedAt.UTC()
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		start.Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{start.Add(-time.Hour), start.Add(time.Minute)})
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-guardian-"+codexTestGuardianThreadID+".jsonl",
		start.Add(-time.Minute), codexTestGuardianThreadID, codexTestParentThreadID, "guardian_review", true,
		[]time.Time{start.Add(-time.Minute), start.Add(time.Minute)})

	t.Setenv(bundleParentThreadIDEnv, codexTestParentThreadID)
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.TaskID != taskID || output.EvidenceStatus != "complete" {
		t.Fatalf("output = %#v", output)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	manifest := readBundleManifest(t, archive)
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusIncluded || parent.AssociationBasis != codexExplicitAssociationBasis {
		t.Fatalf("parent = %#v", parent)
	}
	if !slices.Equal(parent.ThreadIDs, []string{codexTestParentThreadID}) {
		t.Fatalf("parent thread IDs = %#v", parent.ThreadIDs)
	}
	if !strings.Contains(parent.Detail, "task state was not modified") {
		t.Fatalf("parent detail = %q", parent.Detail)
	}
	guardian := findCodexEvidence(t, manifest, codexClassGuardianChild)
	if guardian.Status != codexStatusIncluded || guardian.AssociationBasis != codexExplicitAssociationBasis {
		t.Fatalf("guardian = %#v", guardian)
	}

	after, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if after.ParentCodexThreadID != "" || after.ParentCodexSessionID != "" {
		t.Fatalf("bundle recovery mutated task identity: %#v", after)
	}
}

func TestBundleExplicitParentThreadIDConflictingWithStoredIdentityFailsClosed(t *testing.T) {
	cfg, st, codexHome := newCodexBundleTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID); err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	writeCodexRollout(t, codexHome, "sessions/2026/08/30/rollout-live-"+codexTestParentThreadID+".jsonl",
		time.Now().UTC().Add(-time.Hour), codexTestParentThreadID, "", "agent_created_thread", false,
		[]time.Time{time.Now().UTC().Add(-time.Hour), time.Now().UTC()})

	t.Setenv(bundleParentThreadIDEnv, codexTestOtherThreadID)
	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	manifest := readBundleManifest(t, archive)
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusAmbiguous || !strings.Contains(parent.Detail, "conflicts with the stored parent identity") {
		t.Fatalf("parent = %#v", parent)
	}
	for name := range archive {
		if strings.HasPrefix(name, "codex-parent/rollouts/") {
			t.Fatalf("conflicting explicit identity included parent rollout: %s", name)
		}
	}
}

func TestBundleExplicitParentThreadIDRejectsMalformedValue(t *testing.T) {
	cfg, st, _ := newCodexBundleTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")
	t.Setenv(bundleParentThreadIDEnv, "not-a-thread-id")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	manifest := readBundleManifest(t, readBundleArchive(t, output.ArchivePath))
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusUnavailable || !strings.Contains(parent.Detail, "not a canonical UUID") {
		t.Fatalf("parent = %#v", parent)
	}
}
