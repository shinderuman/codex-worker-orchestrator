package controller

import (
	"bytes"
	"encoding/json"
	"testing"
)

func sealedRuntimeExportFixture(t *testing.T) (EvidenceExport, EvidenceExportResult) {
	t.Helper()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	return export, result
}

func TestExportEvidenceSealedAttemptProjectsCanonicalSections(t *testing.T) {
	t.Parallel()
	export, result := sealedRuntimeExportFixture(t)
	t.Run("runtime-evidence", func(t *testing.T) {
		for _, check := range []struct {
			name   string
			valid  bool
			actual any
		}{
			{"target", result.Target.Kind == evidenceExportTargetTaskID && result.Target.AttemptID == evidenceExportSealedAttemptID, result.Target},
			{"binding", result.Target.RuntimeTaskID == evidenceExportSealedRuntimeTask && result.Target.SelectionBasis == evidenceExportBasisSessionAssociation, result.Target},
			{"coverage", result.RuntimeStatus == evidenceExportRuntimeAbsent && result.Coverage == evidenceExportCoverageAttemptOnly, result},
			{"attempt-section", result.AttemptSectionStatus == evidenceExportAttemptPresent && export.Manifest.Attempt.RuntimeTaskID == evidenceExportSealedRuntimeTask && export.Manifest.Attempt.RuntimeEvidence, []any{result.AttemptSectionStatus, export.Manifest.Attempt}},
			{"worker-state", string(exportFileByPath(t, export, "attempt/state/worker.id")) == "session-sealed-worker\n", exportFileByPath(t, export, "attempt/state/worker.id")},
		} {
			t.Run(check.name, func(t *testing.T) {
				if !check.valid {
					t.Fatalf("%s = %#v", check.name, check.actual)
				}
			})
		}
		for _, path := range []string{"attempt/seal.json", "attempt/session-association.json", "attempt/telemetry.jsonl", "attempt/events.jsonl", "attempt/rounds.jsonl", "attempt/state/worker.id", "attempt/transcripts/model/session-sealed-worker/0", "attempt/transcripts/parent/parent-thread-1/0", "attempt/transcripts/guardian/guardian-rollout-1"} {
			entry := exportEntryByPath(t, export.Manifest, path)
			if entry.Missing || entry.Unreadable != "" || entry.CanonicalDigest == "" || entry.Basis == "" || entry.SHA256 != digestBytes(exportFileByPath(t, export, path)) {
				t.Fatalf("sealed entry %s lacks readable bytes, canonical association or matching digest: %#v", path, entry)
			}
		}
	})
	t.Run("parent-evidence-aggregate", func(t *testing.T) {
		ledger := exportEntryByPath(t, export.Manifest, "attempt/state/parent-evidence.aggregate.json")
		var aggregate evidenceParentEvidenceAggregate
		if err := json.Unmarshal(exportFileByPath(t, export, "attempt/state/parent-evidence.aggregate.json"), &aggregate); err != nil {
			t.Fatal(err)
		}
		if ledger.CanonicalDigest == "" || ledger.OmittedPayload == "" || aggregate.Records != 6 || aggregate.GroupCount != 4 || aggregate.RepetitionsOmitted != 2 || aggregate.SourceSHA256 != ledger.CanonicalDigest {
			t.Fatalf("sealed parent evidence aggregate: entry=%#v aggregate=%#v", ledger, aggregate)
		}
		for _, file := range export.Files {
			if file.Path == "attempt/state/parent-evidence.jsonl" {
				t.Fatal("sealed export embedded the raw parent evidence ledger")
			}
		}
	})
	t.Run("git-audit-metadata-only", func(t *testing.T) {
		if export.Manifest.Git == nil || len(export.Manifest.Git.Archives) != 1 {
			t.Fatalf("sealed git audit = %#v", export.Manifest.Git)
		}
		data := exportFileByPath(t, export, "attempt/git/archives/0.json")
		entry := exportEntryByPath(t, export.Manifest, "attempt/git/archives/0.json")
		var metadata evidenceExportGitArchiveMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(`"pack":`)) || entry.SHA256 != digestBytes(data) || entry.Bytes != int64(len(data)) || metadata.PackDigest == "" || metadata.CanonicalDigest != export.Manifest.Git.Archives[0].Digest || metadata.OmittedPayload == "" {
			t.Fatalf("sealed git archive must expose matching metadata without pack bytes: entry=%#v metadata=%#v bytes=%d", entry, metadata, len(data))
		}
		for _, file := range export.Files {
			if file.Path == "sealed/task-bundle.json" {
				t.Fatal("sealed export kept the nested task bundle projection")
			}
		}
	})
	t.Run("analysis-index", func(t *testing.T) {
		var analysis EvidenceAnalysisIndex
		if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
			t.Fatal(err)
		}
		for _, run := range analysis.ObservedValidationActions {
			if run.Basis != evidenceAnalysisBasisEvents || run.Log != "" {
				t.Fatalf("sealed validation event observation = %#v", run)
			}
		}
		for _, check := range []struct {
			name   string
			valid  bool
			actual any
		}{
			{"validation", len(analysis.CanonicalValidationRuns) == 0 && len(analysis.ObservedValidationActions) == 2 && len(analysis.Retries.ValidationReruns) == 1, analysis},
			{"sessions", analysis.Sessions.Status == evidenceAnalysisStatusCollected && len(analysis.Sessions.Sessions) == 1, analysis.Sessions},
			{"parent", analysis.Parent != nil && analysis.Parent.Status == evidenceAnalysisStatusCollected && analysis.Parent.ThreadID == "parent-thread-1", analysis.Parent},
			{"guardian", len(analysis.Guardian) == 1, analysis.Guardian},
			{"model-calls", analysis.ModelCalls.Status == evidenceAnalysisStatusCollected && analysis.ModelCalls.Total == 1, analysis.ModelCalls},
			{"review-rounds", analysis.ReviewRounds.Status == evidenceAnalysisStatusCollected && analysis.ReviewRounds.Count == 1, analysis.ReviewRounds},
		} {
			t.Run(check.name, func(t *testing.T) {
				if !check.valid {
					t.Fatalf("%s = %#v", check.name, check.actual)
				}
			})
		}
	})
}

func TestExportEvidenceSealedAttemptDisclosesUnreadableArchiveWithoutPathCollision(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	attempt, err := fixture.store.loadAttempt(evidenceExportSealedAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	attempt.BaselineArchive = EvidenceObjectRef{
		Digest: digestBytes([]byte("unreadable-baseline-archive")), Kind: "git-object-archive",
		MediaType: "application/octet-stream", Length: 8, LogicalIdentity: evidenceExportSealedAttemptID + ":baseline",
	}
	if err := fixture.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if len(export.Manifest.Git.Archives) != 1 || export.Manifest.Git.Archives[0].Digest != fixture.seal.GitObjectArchive.Digest {
		t.Fatalf("sealed git audit archives = %#v", export.Manifest.Git.Archives)
	}
	unreadable := exportEntryByPath(t, export.Manifest, "attempt/git/archives/0.json")
	if unreadable.Unreadable == "" || unreadable.SHA256 != "" {
		t.Fatalf("unreadable archive marker = %#v", unreadable)
	}
	for _, file := range export.Files {
		if file.Path == "attempt/git/archives/0.json" {
			t.Fatal("unreadable archive marker archived bytes")
		}
	}
	archived := exportEntryByPath(t, export.Manifest, "attempt/git/archives/1.json")
	if archived.Unreadable != "" || archived.SHA256 != digestBytes(exportFileByPath(t, export, "attempt/git/archives/1.json")) ||
		archived.Bytes != int64(len(exportFileByPath(t, export, "attempt/git/archives/1.json"))) {
		t.Fatalf("readable archive entry = %#v", archived)
	}
	seen := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seen[entry.Path] {
			t.Fatalf("export manifest has duplicate entry path %s", entry.Path)
		}
		seen[entry.Path] = true
	}
}

func TestExportEvidenceSealedAttemptCandidateEntriesHaveUniquePaths(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
		f.candidateEvidenceRounds = 2
		f.acceptedCandidateRevisions = 2
	})
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	validationRecords := map[string]string{}
	validationArtifacts := map[string]string{}
	acceptedCandidates := 0
	seenPaths := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seenPaths[entry.Path] {
			t.Fatalf("manifest entry path is not unique: %s", entry.Path)
		}
		seenPaths[entry.Path] = true
		if entry.Missing || entry.Unreadable != "" || entry.Unattributed != "" {
			continue
		}
		data := exportFileByPath(t, export, entry.Path)
		if entry.SHA256 != digestBytes(data) || entry.Bytes != int64(len(data)) {
			t.Fatalf("entry %s digest/bytes disagree with archived bytes", entry.Path)
		}
		switch {
		case entry.CanonicalKind == evidenceKindCandidateEvidence && entry.LogicalIdentity == evidenceExportSealedAttemptID+":"+candidateEvidenceKindValidation:
			validationRecords[entry.CanonicalDigest] = entry.Path
		case entry.LogicalIdentity == evidenceExportSealedAttemptID+":validation-artifact-0" || entry.LogicalIdentity == evidenceExportSealedAttemptID+":validation-artifact-1":
			validationArtifacts[string(data)] = entry.Path
		case entry.CanonicalKind == "accepted-candidate":
			acceptedCandidates++
		}
	}
	if len(validationRecords) != 2 {
		t.Fatalf("validation candidate revisions = %d (%v)", len(validationRecords), validationRecords)
	}
	if len(validationArtifacts) != 2 {
		t.Fatalf("distinct validation artifacts = %d (%v)", len(validationArtifacts), validationArtifacts)
	}
	if acceptedCandidates != 2 {
		t.Fatalf("accepted candidate revisions = %d", acceptedCandidates)
	}
	for _, file := range export.Files {
		if file.Path != "manifest.json" && !seenPaths[file.Path] && file.Path != "analysis-index.json" {
			t.Fatalf("archive member without a manifest entry: %s", file.Path)
		}
	}
}

func TestExportEvidenceSealedAttemptRepeatsSharedLineageRefIdempotently(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
		f.sharedLineageRevisions = 2
	})
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	acceptedCandidates := 0
	seenPaths := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seenPaths[entry.Path] {
			t.Fatalf("manifest entry path is not unique: %s", entry.Path)
		}
		seenPaths[entry.Path] = true
		if entry.CanonicalKind == "accepted-candidate" {
			acceptedCandidates++
		}
	}
	if acceptedCandidates != 1 {
		t.Fatalf("shared lineage ref materialized %d times", acceptedCandidates)
	}
	if result := export.Manifest.Attempt; result.Status != evidenceExportAttemptPresent {
		t.Fatalf("shared lineage chain lost the sealed attempt section: %#v", result)
	}
}
