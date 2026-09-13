package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const parentEvidenceLedgerFailureTestPath = "parent-evidence-ledger.json"

func TestParentStatusLedgerSaveFailureFailsClosed(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	restore := blockParentEvidenceLedgerSave(t, fixture.st)

	var failedOutput bytes.Buffer
	err := printStatusLeased(fixture.st, &failedOutput)
	if err == nil || !strings.Contains(err.Error(), "delivery claim") {
		t.Fatalf("ledger save failure err=%v output=%s", err, failedOutput.String())
	}
	if failedOutput.Len() == 0 {
		t.Fatal("ledger save failure must occur after the model-visible status output is written")
	}
	restore()

	var retryOutput bytes.Buffer
	if err := printStatusLeased(fixture.st, &retryOutput); err != nil {
		t.Fatalf("retry after failed claim save: %v", err)
	}
	if retryOutput.Len() == 0 {
		t.Fatal("retry after failed claim save produced no status output")
	}

	var duplicate *DuplicateParentProjectionError
	if err := printStatusLeased(fixture.st, &bytes.Buffer{}); err == nil || !errors.As(err, &duplicate) {
		t.Fatalf("same-lease duplicate err=%v", err)
	}

	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	if err := printStatusLeased(fixture.st, &bytes.Buffer{}); err != nil {
		t.Fatalf("fresh lease status projection: %v", err)
	}
}

func TestEvidenceBatchLedgerSaveFailureFailsClosed(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	manifestPath := filepath.Join(t.TempDir(), "evidence-manifest.json")
	manifest, err := json.Marshal(parentEvidenceManifest{
		Version: parentEvidenceManifestVersion,
		Reason:  "ledger failure",
		Status:  &parentEvidenceStatusRequest{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	restore := blockParentEvidenceLedgerSave(t, fixture.st)

	var failedOutput bytes.Buffer
	err = printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, fixture.cfg, fixture.st, &failedOutput)
	if err == nil || !strings.Contains(err.Error(), "delivery claim") {
		t.Fatalf("ledger save failure err=%v output=%s", err, failedOutput.String())
	}
	if failedOutput.Len() == 0 {
		t.Fatal("ledger save failure must occur after the model-visible batch output is written")
	}
	var failed parentEvidenceOutput
	if err := jsonUnmarshalParentEvidence(failedOutput.Bytes(), &failed); err != nil {
		t.Fatal(err)
	}
	if len(failed.Parts) != 1 || failed.Parts[0].Digest == "" {
		t.Fatalf("failed batch output = %#v", failed.Parts)
	}
	digest := failed.Parts[0].Digest
	restore()

	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, digest); err != nil || delivered {
		t.Fatalf("failed batch claim delivered=%v err=%v", delivered, err)
	}

	var retryOutput bytes.Buffer
	if err := printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, fixture.cfg, fixture.st, &retryOutput); err != nil {
		t.Fatalf("retry after failed batch claim save: %v", err)
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, digest); err != nil || !delivered {
		t.Fatalf("successful batch claim delivered=%v err=%v", delivered, err)
	}

	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	if err := printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, fixture.cfg, fixture.st, &bytes.Buffer{}); err != nil {
		t.Fatalf("fresh lease batch projection: %v", err)
	}
}

func TestRepoSearchLedgerSaveFailureFailsClosed(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	request := repoSearchRequest{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 4096}
	restore := blockParentEvidenceLedgerSave(t, fixture.st)

	var failedOutput bytes.Buffer
	err := printRepoSearch(request, fixture.cfg, fixture.st, &failedOutput)
	if err == nil || !strings.Contains(err.Error(), "delivery claim") {
		t.Fatalf("ledger save failure err=%v output=%s", err, failedOutput.String())
	}
	if failedOutput.Len() == 0 {
		t.Fatal("ledger save failure must occur after the model-visible repo-search output is written")
	}
	restore()

	if err := printRepoSearch(request, fixture.cfg, fixture.st, &bytes.Buffer{}); err != nil {
		t.Fatalf("retry after failed repo-search claim save: %v", err)
	}
	var duplicate *DuplicateParentProjectionError
	if err := printRepoSearch(request, fixture.cfg, fixture.st, &bytes.Buffer{}); err == nil || !errors.As(err, &duplicate) {
		t.Fatalf("same-lease repo-search duplicate err=%v", err)
	}

	if err := fixture.st.AdvanceParentEvidenceLease(); err != nil {
		t.Fatal(err)
	}
	if err := printRepoSearch(request, fixture.cfg, fixture.st, &bytes.Buffer{}); err != nil {
		t.Fatalf("fresh lease repo-search projection: %v", err)
	}
}

func blockParentEvidenceLedgerSave(t *testing.T, st *state.StateStore) func() {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("read-only directory fixture requires Unix permission semantics")
	}

	stateDir := filepath.Dir(st.Path(parentEvidenceLedgerFailureTestPath))
	for _, path := range []string{
		st.Path(state.ParentEvidenceLedgerLockFile),
		st.Path(parentEvidenceTelemetryFile),
	} {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(stateDir, 0o700) })

	return func() {
		t.Helper()
		if err := os.Chmod(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func jsonUnmarshalParentEvidence(data []byte, output *parentEvidenceOutput) error {
	if err := json.Unmarshal(data, output); err != nil {
		return fmt.Errorf("evidence output is not valid JSON: %w: %s", err, string(data))
	}
	return nil
}
