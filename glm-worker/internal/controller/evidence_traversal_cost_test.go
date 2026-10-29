package controller

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEvidenceTraversalCostAcrossCleanupAndBundles(t *testing.T) {
	store, source := newEvidenceTestStoreWithRepo(t)
	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "payload.bin"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, source, "add", "payload.bin")
	runControllerGit(t, source, "commit", "-qm", "large archive fixture")
	sealRef, seal := storeBundleAttemptSeal(t, store, source, "shared-attempt", "IMPLEMENTATION_TASKS/shared.md", "", 1)
	taskRef := storeBundleTaskRevision(t, store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	head, ledger := publishBundleAuthority(t, store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}}, nil)
	operations := []struct {
		name string
		run  func() error
	}{
		{name: "cleanup", run: func() error {
			_, err := store.ProveCleanupDurability(sealRef)
			return err
		}},
		{name: "task-bundle", run: func() error {
			_, err := store.BuildTaskEvidenceBundle(taskRef)
			return err
		}},
	}
	sparse := make([]uint64, len(operations))
	for index, operation := range operations {
		if err := operation.run(); err != nil {
			t.Fatal(err)
		}
		sparse[index] = evidenceOperationAllocatedBytes(t, operation.run)
	}
	for generation := uint64(2); generation <= 32; generation++ {
		taskRef = storeBundleTaskRevision(t, store, seal.SemanticTaskRef, generation, &taskRef, []EvidenceObjectRef{sealRef}, nil)
		head, ledger = publishBundleAuthority(t, store, generation, fmt.Sprintf("snapshot-%d", generation), &head, &ledger,
			[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}}, nil)
	}
	for index, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			dense := evidenceOperationAllocatedBytes(t, operation.run)
			if dense > 4*sparse[index]+(2<<20) {
				t.Fatalf("shared archive work grew with historical references: sparse=%d dense=%d", sparse[index], dense)
			}
			t.Logf("allocated bytes: sparse=%d dense=%d", sparse[index], dense)
		})
	}
	t.Run("archive-verification", func(t *testing.T) {
		const repetitions = 32
		allocated := evidenceOperationAllocatedBytes(t, func() error {
			for range repetitions {
				if _, err := store.VerifyGitObjectArchive(seal.GitObjectArchive); err != nil {
					return err
				}
			}
			return nil
		})
		budget := uint64(repetitions+8)*uint64(seal.GitObjectArchive.Length) + (2 << 20)
		if allocated > budget {
			t.Fatalf("verified archive was repeatedly decoded: allocated=%d budget=%d", allocated, budget)
		}
	})
}

func evidenceOperationAllocatedBytes(t *testing.T, operation func() error) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if err := operation(); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
