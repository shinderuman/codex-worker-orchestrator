package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func execLookPathGit() (string, error) {
	return exec.LookPath("git")
}

func TestParentEvidenceReplayReducesModelVisibleBytesAndReturns(t *testing.T) {
	if _, err := execLookPathGit(); err != nil {
		t.Skipf("git commandがないためreplay testをskipします: %v", err)
	}
	fixture := newParentEvidenceFixture(t)

	fullManifest := func() parentEvidenceManifest {
		return parentEvidenceManifest{
			Version: 1,
			Reason:  "incident replay",
			Authority: []parentEvidenceAuthorityRequest{
				{Kind: "rules", BudgetBytes: 8192},
				{Kind: "plan", BudgetBytes: 8192},
				{Kind: "active", BudgetBytes: 8192},
			},
			Handoff:   &parentEvidenceHandoffRequest{},
			Status:    &parentEvidenceStatusRequest{},
			Telemetry: &parentEvidenceTelemetryRequest{},
			Search: []parentEvidenceSearchRequest{
				{Question: "park lifecycle owner", Scopes: []string{"docs"}, BudgetBytes: 4096},
			},
		}
	}

	naive := runParentEvidence(t, fixture, fullManifest())
	naiveBytes := len(naive.Raw)
	var handoffValue parentHandoffOutput
	if err := json.Unmarshal(naive.Output.Parts[3].Handoff, &handoffValue); err != nil {
		t.Fatal(err)
	}
	_, handoffBytes := parentEvidenceDigest(handoffValue)
	naiveBytes += 2 * handoffBytes

	known := parentEvidenceManifest{
		Version: 1,
		Reason:  "bounded incident replay with known digests",
		Authority: []parentEvidenceAuthorityRequest{
			{Kind: "rules", KnownContentSHA256: naive.Output.Parts[0].Digest, BudgetBytes: 8192},
			{Kind: "plan", KnownContentSHA256: naive.Output.Parts[1].Digest, BudgetBytes: 8192},
			{Kind: "active", KnownContentSHA256: naive.Output.Parts[2].Digest, BudgetBytes: 8192},
		},
		Handoff:   &parentEvidenceHandoffRequest{KnownDigest: naive.Output.Parts[3].Digest},
		Telemetry: &parentEvidenceTelemetryRequest{},
	}
	bounded := runParentEvidence(t, fixture, known)
	boundedBytes := len(bounded.Raw)

	var rejected bytes.Buffer
	duplicateErr := printParentHandoffLeased(fixture.st, &rejected)
	var duplicate *DuplicateParentProjectionError
	if !errors.As(duplicateErr, &duplicate) {
		t.Fatalf("duplicate poll was not rejected: %v stdout = %s", duplicateErr, rejected.String())
	}
	boundedBytes += rejected.Len()

	if boundedBytes >= naiveBytes/2 {
		t.Fatalf("bounded replay bytes = %d did not fall below half of naive = %d", boundedBytes, naiveBytes)
	}
	if bytes.Contains([]byte(bounded.Raw), []byte("authority bootstrap")) || bytes.Contains([]byte(bounded.Raw), []byte("required_action")) {
		t.Fatalf("bounded replay re-emits known bodies: %s", bounded.Raw)
	}

	if bounded.Output.TaskID != naive.Output.TaskID || bounded.Output.TaskStatus != naive.Output.TaskStatus {
		t.Fatalf("bounded replay lost decision context: %#v", bounded.Output)
	}
	for index := 0; index < 3; index++ {
		if bounded.Output.Parts[index].Authority.SnapshotSHA256 != naive.Output.Parts[index].Authority.SnapshotSHA256 {
			t.Fatalf("authority snapshot digest diverged on part %d", index)
		}
	}
	if bounded.Output.Parts[3].Digest != naive.Output.Parts[3].Digest {
		t.Fatalf("handoff digest diverged between replays")
	}

	records, err := fixture.st.ReadParentEvidence()
	if err != nil {
		t.Fatal(err)
	}
	summary := state.SummarizeParentEvidence(records)
	if summary.UnchangedCount < 4 {
		t.Fatalf("unchanged projections = %d, want at least 4: %#v", summary.UnchangedCount, summary)
	}
	if summary.DuplicateRejections < 1 {
		t.Fatalf("duplicate rejections = %d, want at least 1: %#v", summary.DuplicateRejections, summary)
	}
	if summary.ProjectedTokenProxy <= 0 {
		t.Fatalf("projected token proxy missing: %#v", summary)
	}
	if !strings.Contains(duplicate.Error(), parentEvidenceBatchCommand) {
		t.Fatalf("duplicate rejection does not point at the batch entry: %q", duplicate.Error())
	}
}
