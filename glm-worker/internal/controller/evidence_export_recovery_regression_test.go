package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestExportEvidenceExplicitTaskIgnoresUnpublishedSeals(t *testing.T) {
	for _, sameTask := range []bool{false, true} {
		name := "unpublished-task"
		if sameTask {
			name = "unpublished-seal"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newEvidenceExportFixture(t, func(f *evidenceExportFixture) { f.withSealedRuntime = true })
			pending, err := f.store.loadAttempt(evidenceExportSealedAttemptID)
			if err != nil {
				t.Fatal(err)
			}
			pending.AttemptID = "unpublished-unrelated-attempt"
			pending.AttemptSealID = "unpublished-seal"
			if !sameTask {
				pending.SemanticTaskRef = SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/unpublished-other.md", ContractDigest: "other-contract"}
			}
			if err := f.store.writeAttempt(pending); err != nil {
				t.Fatal(err)
			}
			_, result, err := f.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
			if err != nil {
				t.Fatalf("unrelated unpublished seal blocked explicit export: %v", err)
			}
			if result.Target.AttemptID != evidenceExportSealedAttemptID || result.Target.RuntimeTaskID != evidenceExportSealedRuntimeTask {
				t.Fatalf("export selected another task: %#v", result.Target)
			}
			if _, _, err := f.store.ExportEvidence(EvidenceExportRequest{TaskID: "unknown-task-id"}); err == nil {
				t.Fatal("unpublished seal made an unknown Task ID valid")
			}
		})
	}
}

func TestExportEvidenceRetainsUnpublishedTaskAuthority(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		name := "exact-contract"
		if mismatch {
			name = "contract-mismatch"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, body := newUnpublishedTaskAuthorityExportFixture(t, mismatch)
			export, _, err := f.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
			if err != nil {
				t.Fatal(err)
			}
			entry := exportEntryByPath(t, export.Manifest, "attempt/authority.md")
			if mismatch {
				if entry.Unreadable == "" || entry.SHA256 != "" {
					t.Fatalf("mismatched Task content was presented as authority: %#v", entry)
				}
				return
			}
			got := exportFileByPath(t, export, "attempt/authority.md")
			if !bytes.Equal(got, body) || entry.SHA256 != f.seal.SemanticTaskRef.ContractDigest {
				t.Fatalf("export lost the exact original Task: %#v", entry)
			}
			var analysis EvidenceAnalysisIndex
			if err := json.Unmarshal(exportFileByPath(t, export, export.Manifest.AnalysisIndex.Path), &analysis); err != nil {
				t.Fatal(err)
			}
			if analysis.Instructions.Task == nil || analysis.Instructions.Task.Entry != "attempt/authority.md" {
				t.Fatalf("analysis did not identify sealed Task authority: %#v", analysis.Instructions.Task)
			}
		})
	}
}

func newUnpublishedTaskAuthorityExportFixture(t *testing.T, mismatch bool) (*evidenceExportFixture, []byte) {
	t.Helper()
	f := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipSealedEvidence = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	source := newControllerRepo(t)
	base := controllerGitOutput(t, source, "rev-parse", "HEAD")
	baseTree := controllerGitOutput(t, source, "rev-parse", "HEAD^{tree}")
	taskPath := "IMPLEMENTATION_TASKS/unpublished-task.md"
	body := []byte("# Task\n\n## Original instruction\n\nKeep this original requirement.\n\n## Contract\n\nPreserve exact authority.\n\n## Acceptance criteria\n\nExport after retirement.\n")
	writeSuspensionTestFile(t, source, taskPath, string(body))
	runControllerGit(t, source, "add", taskPath)
	runControllerGit(t, source, "commit", "-qm", "unpublished Task source")
	task := SemanticTaskRef{TaskPath: taskPath, ContractDigest: digestBytes(body)}
	if mismatch {
		task.ContractDigest = digestBytes([]byte("another requirement"))
	}
	_, seal := storeBundleAttemptSealForTask(t, f.store, source, evidenceExportSealedAttemptID, task, "episode-1", 1)
	archive, roots, err := f.store.CaptureGitObjectArchive(source, "unpublished-task-source", []string{base, baseTree, seal.CurrentWorktreeTree})
	if err != nil {
		t.Fatal(err)
	}
	seal.ExecutionBaseOID = base
	seal.BaselineIndexTree = baseTree
	seal.BaselineWorktreeTree = baseTree
	seal.GitObjectArchive, seal.GitObjectArchiveRoots = archive, roots
	ref, seal := f.attachSealedRuntimeEvidence(t, seal)
	taskRef := storeBundleTaskRevision(t, f.store, task, 1, nil, []EvidenceObjectRef{ref}, nil)
	episodeRef := storeBundleEpisodeRevision(t, f.store, seal, taskRef, 1, 1, nil, []EvidenceObjectRef{ref}, nil)
	publishBundleAuthority(t, f.store, 1, "snapshot-1", nil, nil, []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(task), RevisionRef: taskRef}}, []EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}})
	f.seal, f.sealRef = seal, ref
	f.writeSealedAttemptRecord(t, seal)
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(f.repo, taskPath)); !os.IsNotExist(err) {
		t.Fatalf("Task authority exists in the export workspace: %v", err)
	}
	return f, body
}
