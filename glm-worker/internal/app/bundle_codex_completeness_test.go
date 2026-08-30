package app

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestBundleMissingParentIdentityMarksEvidenceIncomplete(t *testing.T) {
	cfg, st, _ := newCodexBundleTestState(t)
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	if output.EvidenceStatus != "incomplete" {
		t.Fatalf("evidence status = %q, want incomplete", output.EvidenceStatus)
	}
	if !slices.Contains(output.Missing, "codex-parent/parent-session") {
		t.Fatalf("missing = %#v, want codex-parent/parent-session", output.Missing)
	}

	archive := readBundleArchive(t, output.ArchivePath)
	manifest := readBundleManifest(t, archive)
	if manifest.EvidenceStatus != "incomplete" {
		t.Fatalf("manifest evidence status = %q, want incomplete", manifest.EvidenceStatus)
	}
	if !slices.Contains(manifest.Missing, "codex-parent/parent-session") {
		t.Fatalf("manifest missing = %#v, want codex-parent/parent-session", manifest.Missing)
	}
	parent := findCodexEvidence(t, manifest, codexClassParentSession)
	if parent.Status != codexStatusMissing {
		t.Fatalf("parent status = %q, want missing", parent.Status)
	}
}
