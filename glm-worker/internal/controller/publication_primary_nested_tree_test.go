package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRootPromotionReplacesNestedDirectory(t *testing.T) {
	t.Parallel()
	for _, symlink := range []bool{false, true} {
		name := "file"
		if symlink {
			name = "symlink"
		}
		t.Run(name, func(t *testing.T) {
			outside := t.TempDir()
			writeSuspensionTestFile(t, outside, "target", "outside remains\n")
			target := filepath.Join(outside, "target")
			fixture, _, accepted, candidate := nestedPrimaryPromotionFixture(t, symlink, target)
			unrelated := filepath.Join(fixture.store.identity.PrimaryRoot, "unrelated", "empty")
			if err := os.MkdirAll(unrelated, 0o755); err != nil {
				t.Fatal(err)
			}
			if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
				ExpectedGeneration: accepted.Head.ControllerGeneration,
				CandidateID:        candidate.CandidateID,
			}); err != nil {
				t.Fatalf("nested directory promotion: %v", err)
			}
			assertNestedPrimaryPromotion(t, fixture, candidate, symlink, target)
			if info, err := os.Lstat(unrelated); err != nil || !info.IsDir() {
				t.Fatalf("unrelated empty directory changed: %v, %v", info, err)
			}
		})
	}
}

func TestRootPromotionRecoversNestedReplacement(t *testing.T) {
	t.Parallel()
	for _, symlink := range []bool{false, true} {
		for removed := 1; removed <= 5; removed++ {
			t.Run(fmt.Sprintf("symlink=%t/removed=%d", symlink, removed), func(t *testing.T) {
				outside := t.TempDir()
				writeSuspensionTestFile(t, outside, "target", "outside remains\n")
				target := filepath.Join(outside, "target")
				fixture, policy, accepted, candidate := nestedPrimaryPromotionFixture(t, symlink, target)
				op := prepareNestedPrimaryPromotion(t, fixture, policy, accepted, candidate)
				root := fixture.store.identity.PrimaryRoot
				paths := []string{"replacement/nested/deeper/old.txt", "replacement/nested/deeper", "replacement/nested", "replacement"}
				for _, path := range paths[:min(removed, len(paths))] {
					if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
						t.Fatal(err)
					}
				}
				if removed == 5 {
					entries, err := sourceImportTreeEntries(root, candidate.TreeOID)
					if err != nil {
						t.Fatal(err)
					}
					for _, entry := range entries {
						if entry.path == "replacement" {
							if err := applySourceTreeEntries(root, nil, []workspaceTreeEntry{entry}); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
				if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
					t.Fatalf("nested replacement recovery: %v", err)
				}
				assertNestedPrimaryPromotion(t, fixture, candidate, symlink, target)
			})
		}
	}
}

func prepareNestedPrimaryPromotion(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, accepted ExecutionOperationResult, candidate AcceptedCandidate) ExecutionOperation {
	t.Helper()
	candidate.State = candidatePromoted
	op, err := fixture.store.planCandidateRevision(accepted.Head, candidate, *accepted.CandidateRef, publicationPromote)
	if err != nil {
		t.Fatal(err)
	}
	op.Publication.LocalOld = candidate.BaseOID
	op.Publication.RemoteOID = observeTestRemote(t, fixture.store.identity.PrimaryRoot, policy)
	if err := fixture.store.planPrimaryPromotion(&op, candidate); err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, accepted.Head); err != nil {
		t.Fatal(err)
	}
	return op
}

func TestRootPromotionNestedRecoveryPreservesForeignPaths(t *testing.T) {
	t.Parallel()
	for _, mutation := range []string{"nonempty directory", "empty directory", "nested empty directory", "ancestor symlink", "unknown leaf", "owned directory replaced by file"} {
		t.Run(mutation, func(t *testing.T) {
			outside := t.TempDir()
			writeSuspensionTestFile(t, outside, "target", "outside remains\n")
			fixture, policy, accepted, candidate := nestedPrimaryPromotionFixture(t, false, filepath.Join(outside, "target"))
			op := prepareNestedPrimaryPromotion(t, fixture, policy, accepted, candidate)
			root := fixture.store.identity.PrimaryRoot
			foreign := "replacement/foreign"
			switch mutation {
			case "nonempty directory":
				foreign += "/child"
				writeSuspensionTestFile(t, root, foreign, "foreign remains\n")
			case "empty directory", "nested empty directory":
				if mutation == "nested empty directory" {
					foreign = "replacement/nested/deeper/foreign"
				}
				if err := os.Mkdir(filepath.Join(root, foreign), 0o755); err != nil {
					t.Fatal(err)
				}
			case "ancestor symlink", "owned directory replaced by file":
				for _, path := range []string{"replacement/nested/deeper/old.txt", "replacement/nested/deeper"} {
					if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
						t.Fatal(err)
					}
				}
				foreign = "replacement/nested/deeper"
				if mutation == "ancestor symlink" {
					if err := os.Symlink(outside, filepath.Join(root, filepath.FromSlash(foreign))); err != nil {
						t.Fatal(err)
					}
				} else {
					writeSuspensionTestFile(t, root, foreign, "foreign remains\n")
				}
			case "unknown leaf":
				foreign = "replacement/nested/deeper/old.txt"
				writeSuspensionTestFile(t, root, foreign, "foreign remains\n")
			}
			if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
				t.Fatal("recovery accepted an unattributed path")
			}
			if mutation == "empty directory" || mutation == "nested empty directory" || mutation == "nonempty directory" {
				assertNestedPrimarySourceUnchanged(t, root)
			}
			path := filepath.Join(root, filepath.FromSlash(foreign))
			switch mutation {
			case "empty directory", "nested empty directory":
				if info, err := os.Lstat(path); err != nil || !info.IsDir() {
					t.Fatalf("foreign empty directory changed: %v, %v", info, err)
				}
			case "ancestor symlink":
				if got, err := os.Readlink(path); err != nil || got != outside {
					t.Fatalf("foreign symlink changed: %q, %v", got, err)
				}
			default:
				if got, err := os.ReadFile(path); err != nil || string(got) != "foreign remains\n" {
					t.Fatalf("foreign content changed: %q, %v", got, err)
				}
			}
			if got, err := os.ReadFile(filepath.Join(outside, "target")); err != nil || string(got) != "outside remains\n" {
				t.Fatalf("outside content changed: %q, %v", got, err)
			}
			if controllerGitOutput(t, root, "rev-parse", policy.LocalRef) != candidate.BaseOID {
				t.Fatal("rejected recovery moved the publication ref")
			}
			if controllerGitOutput(t, root, "write-tree") != op.Publication.SourceTrees.IndexTree {
				t.Fatal("rejected recovery changed the index")
			}
		})
	}
}

func TestRootPromotionRejectsForeignEmptyDirectoryBeforeSourceMutation(t *testing.T) {
	t.Parallel()
	fixture, policy, accepted, candidate := nestedPrimaryPromotionFixture(t, false, "")
	root := fixture.store.identity.PrimaryRoot
	foreign := filepath.Join(root, "replacement", "foreign")
	if err := os.Mkdir(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	index := controllerGitOutput(t, root, "write-tree")
	if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID:        candidate.CandidateID,
	}); err == nil {
		t.Fatal("promotion accepted an unattributed empty directory")
	}
	assertNestedPrimarySourceUnchanged(t, root)
	if info, err := os.Lstat(foreign); err != nil || !info.IsDir() {
		t.Fatalf("foreign empty directory changed: %v, %v", info, err)
	}
	if controllerGitOutput(t, root, "rev-parse", policy.LocalRef) != candidate.BaseOID || controllerGitOutput(t, root, "write-tree") != index {
		t.Fatal("rejected promotion changed the publication ref or index")
	}
}

func TestRootPromotionFileToDirectoryRecoveryRejectsForeignEmptyDirectoryBeforeMutation(t *testing.T) {
	t.Parallel()
	fixture, policy, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t, "replacement", "old file\n", func(root string) {
		if err := os.Remove(filepath.Join(root, "replacement")); err != nil {
			t.Fatal(err)
		}
		writeSuspensionTestFile(t, root, "replacement/nested/target", "accepted target\n")
	})
	root := fixture.store.identity.PrimaryRoot
	writeSuspensionTestFile(t, root, "replacement", "old file\n")
	op := prepareNestedPrimaryPromotion(t, fixture, policy, accepted, candidate)
	if err := os.Remove(filepath.Join(root, "replacement")); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "replacement", "foreign")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("recovery accepted an unattributed empty directory")
	}
	if info, err := os.Lstat(foreign); err != nil || !info.IsDir() {
		t.Fatalf("foreign empty directory changed: %v, %v", info, err)
	}
	if _, err := os.Lstat(filepath.Join(root, "replacement", "nested")); !os.IsNotExist(err) {
		t.Fatalf("rejected recovery wrote a target directory: %v", err)
	}
	if controllerGitOutput(t, root, "rev-parse", policy.LocalRef) != candidate.BaseOID || controllerGitOutput(t, root, "write-tree") != op.Publication.SourceTrees.IndexTree {
		t.Fatal("rejected recovery changed the publication ref or index")
	}
}

func assertNestedPrimarySourceUnchanged(t *testing.T, root string) {
	t.Helper()
	if got, err := os.ReadFile(filepath.Join(root, "replacement", "nested", "deeper", "old.txt")); err != nil || string(got) != "old child\n" {
		t.Fatalf("rejected promotion changed the old leaf: %q, %v", got, err)
	}
	for _, path := range []string{"replacement", "replacement/nested", "replacement/nested/deeper"} {
		if info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path))); err != nil || !info.IsDir() {
			t.Fatalf("rejected promotion changed old directory %q: %v, %v", path, info, err)
		}
	}
}

func TestSourceTreeWriterRejectsInvalidReplacementPathsBeforeMutation(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/replacement/child", "../escape", "replacement/../escape", ".git/config"} {
		t.Run(path, func(t *testing.T) {
			repo := newControllerRepo(t)
			writeSuspensionTestFile(t, repo, "preserved", "old source remains\n")
			runControllerGit(t, repo, "add", "preserved")
			runControllerGit(t, repo, "commit", "-qm", "before")
			tree := controllerGitOutput(t, repo, "rev-parse", "HEAD^{tree}")
			before, err := sourceImportTreeEntries(repo, tree)
			if err != nil {
				t.Fatal(err)
			}
			target := sourceImportEntryMap(before)["preserved"]
			target.path = path
			if err := applySourceTreeEntries(repo, before, []workspaceTreeEntry{target}); err == nil {
				t.Fatal("invalid replacement path was accepted")
			}
			if got, err := os.ReadFile(filepath.Join(repo, "preserved")); err != nil || string(got) != "old source remains\n" {
				t.Fatalf("invalid path rejection changed source: %q, %v", got, err)
			}
		})
	}
}

func nestedPrimaryPromotionFixture(t *testing.T, symlink bool, target string) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	fixture, policy, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t, "replacement/nested/deeper/old.txt", "old child\n", func(root string) {
		for _, path := range []string{"replacement/nested/deeper/old.txt", "replacement/nested/deeper", "replacement/nested", "replacement"} {
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				t.Fatal(err)
			}
		}
		if symlink {
			if err := os.Symlink(target, filepath.Join(root, "replacement")); err != nil {
				t.Fatal(err)
			}
			return
		}
		writeSuspensionTestFile(t, root, "replacement", "accepted replacement\n")
	})
	writeSuspensionTestFile(t, fixture.store.identity.PrimaryRoot, "replacement/nested/deeper/old.txt", "old child\n")
	return fixture, policy, accepted, candidate
}

func assertNestedPrimaryPromotion(t *testing.T, fixture findingAcceptanceFixture, candidate AcceptedCandidate, symlink bool, target string) {
	t.Helper()
	path := filepath.Join(fixture.store.identity.PrimaryRoot, "replacement")
	if symlink {
		got, err := os.Readlink(path)
		if err != nil || got != target {
			t.Fatalf("replacement symlink = %q, %v", got, err)
		}
	} else if got, err := os.ReadFile(path); err != nil || string(got) != "accepted replacement\n" {
		t.Fatalf("replacement content = %q, %v", got, err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "outside remains\n" {
		t.Fatalf("external target changed: %q, %v", got, err)
	}
	if err := fixture.store.verifyPrimaryPublicationTree(candidate); err != nil {
		t.Fatalf("primary worktree/index differ from accepted tree: %v", err)
	}
}
