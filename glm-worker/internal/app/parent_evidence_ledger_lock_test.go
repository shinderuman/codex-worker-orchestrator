package app

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type blockingParentEvidenceWriter struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}
type failingParentEvidenceWriter struct{}

func (w *blockingParentEvidenceWriter) Write(data []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(data), nil
}

func (failingParentEvidenceWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout render failure")
}

func TestParentEvidenceLedgerLockSerializesConcurrentReads(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	writer := &blockingParentEvidenceWriter{started: make(chan struct{}), release: make(chan struct{})}
	firstErr := make(chan error, 1)
	go func() { firstErr <- printStatusLeased(fixture.st, writer) }()
	<-writer.started

	secondErr := make(chan error, 1)
	go func() { secondErr <- printStatusLeased(fixture.st, io.Discard) }()
	select {
	case err := <-secondErr:
		t.Fatalf("second read finished while the first read still held the ledger lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	close(writer.release)
	if err := <-firstErr; err != nil {
		t.Fatal(err)
	}
	err := <-secondErr
	var duplicate *DuplicateParentProjectionError
	if !errors.As(err, &duplicate) {
		t.Fatalf("second read error = %v, want duplicate rejection after serialization", err)
	}
}

func TestParentEvidenceRenderFailureLeavesNoLedgerClaim(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	if err := printStatusLeased(fixture.st, failingParentEvidenceWriter{}); err == nil {
		t.Fatal("render failure must surface")
	}
	digest := parentStatusReadDigest(fixture.st)
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, digest); err != nil || delivered {
		t.Fatalf("failed render left a ledger claim: delivered=%v err=%v", delivered, err)
	}
	if err := printStatusLeased(fixture.st, io.Discard); err != nil {
		t.Fatalf("retry after render failure: %v", err)
	}
}

func TestEvidenceBatchDegradesPartsAlreadyDeliveredByStandaloneRead(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	if err := printStatusLeased(fixture.st, io.Discard); err != nil {
		t.Fatal(err)
	}

	result := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "batch after standalone status read",
		Status:  &parentEvidenceStatusRequest{},
		Search: []parentEvidenceSearchRequest{
			{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 4096},
		},
	})
	var statusPart *parentEvidencePart
	for index := range result.Output.Parts {
		if result.Output.Parts[index].Kind == "status" {
			statusPart = &result.Output.Parts[index]
		}
	}
	if statusPart == nil {
		t.Fatalf("status part missing: %s", result.Raw)
	}
	if len(statusPart.StatusRead) != 0 {
		t.Fatal("already delivered status body was projected again")
	}
	if statusPart.Reason != parentEvidenceUnchangedReason {
		t.Fatalf("degraded status reason = %q", statusPart.Reason)
	}

	err := printRepoSearch(repoSearchRequest{
		Question:    "park lifecycle owner",
		Scopes:      []string{"docs"},
		BudgetBytes: 4096,
	}, fixture.cfg, fixture.st, io.Discard)
	var duplicate *DuplicateParentProjectionError
	if !errors.As(err, &duplicate) {
		t.Fatalf("standalone repeat after evidence batch = %v, want duplicate rejection", err)
	}
}

func TestEvidenceBatchMergesLedgerWithoutLosingStandaloneClaims(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	saveParentEvidenceLedger(fixture.st, state.ParentEvidenceSurfaceSearch, "standalone-digest", state.ParentEvidenceOriginStandalone, "")

	result := runParentEvidence(t, fixture, parentEvidenceManifest{
		Version: 1,
		Reason:  "batch claim merge",
		Status:  &parentEvidenceStatusRequest{},
	})
	if result.Output.Status == parentEvidenceStatusError {
		t.Fatalf("batch failed: %s", result.Raw)
	}
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceSearch, "standalone-digest"); err != nil || !delivered {
		t.Fatalf("batch save lost the standalone claim: delivered=%v err=%v", delivered, err)
	}
	digest := parentStatusReadDigest(fixture.st)
	if _, delivered, err := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, digest); err != nil || !delivered {
		t.Fatalf("batch claim missing after commit: delivered=%v err=%v", delivered, err)
	}
}

func TestEvidenceBatchRenderFailureLeavesNoLedgerClaim(t *testing.T) {
	fixture := newParentEvidenceFixture(t)
	manifest := parentEvidenceManifest{
		Version: 1,
		Reason:  "batch render failure",
		Status:  &parentEvidenceStatusRequest{},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(t.TempDir(), "evidence-manifest.json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	err = printParentEvidence(Command{Mode: ModeEvidence, EvidenceManifest: manifestPath}, fixture.cfg, fixture.st, failingParentEvidenceWriter{})
	if err == nil {
		t.Fatal("batch render failure must surface")
	}
	digest := parentStatusReadDigest(fixture.st)
	if _, delivered, derr := fixture.st.ParentEvidenceDelivered(state.ParentEvidenceSurfaceStatus, digest); derr != nil || delivered {
		t.Fatalf("failed batch render left ledger claims: delivered=%v err=%v", delivered, derr)
	}
}
