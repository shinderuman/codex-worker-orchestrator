package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootPromotionAppliesStructuralTreeTransitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		remotePath    string
		remoteContent string
		edit          func(*testing.T, string)
		verify        func(*testing.T, string)
	}{
		{
			name:          "file to directory",
			remotePath:    "external",
			remoteContent: "old external file\n",
			edit: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "external")); err != nil {
					t.Fatal(err)
				}
				writeSuspensionTestFile(t, root, "external/child", "new child\n")
			},
			verify: func(t *testing.T, root string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(root, "external/child"))
				if err != nil || string(data) != "new child\n" {
					t.Fatalf("file-to-directory promotion = %q, %v", data, err)
				}
			},
		},
		{
			name:          "directory to file",
			remotePath:    "external/child",
			remoteContent: "old child\n",
			edit: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "external/child")); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(filepath.Join(root, "external")); err != nil {
					t.Fatal(err)
				}
				writeSuspensionTestFile(t, root, "external", "new external file\n")
			},
			verify: func(t *testing.T, root string) {
				t.Helper()
				data, err := os.ReadFile(filepath.Join(root, "external"))
				if err != nil || string(data) != "new external file\n" {
					t.Fatalf("directory-to-file promotion = %q, %v", data, err)
				}
			},
		},
		{
			name:          "file to relative symlink",
			remotePath:    "external",
			remoteContent: "old external file\n",
			edit: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "external")); err != nil {
					t.Fatal(err)
				}
				writeSuspensionTestFile(t, root, "target.txt", "target untouched\n")
				if err := os.Symlink("target.txt", filepath.Join(root, "external")); err != nil {
					t.Fatal(err)
				}
			},
			verify: func(t *testing.T, root string) {
				t.Helper()
				target, err := os.Readlink(filepath.Join(root, "external"))
				if err != nil || target != "target.txt" {
					t.Fatalf("promoted symlink target = %q, %v", target, err)
				}
				data, err := os.ReadFile(filepath.Join(root, "target.txt"))
				if err != nil || string(data) != "target untouched\n" {
					t.Fatalf("promoted symlink modified target: %q, %v", data, err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, _, accepted, candidate := primaryPromotionFixtureWithTreeEdit(t, tc.remotePath, tc.remoteContent, func(root string) {
				tc.edit(t, root)
			})
			if _, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{
				ExpectedGeneration: accepted.Head.ControllerGeneration,
				CandidateID:        candidate.CandidateID,
			}); err != nil {
				t.Fatalf("structural candidate promotion: %v", err)
			}
			root := fixture.store.identity.PrimaryRoot
			tc.verify(t, root)
			if err := fixture.store.verifyPrimaryPublicationTree(candidate); err != nil {
				t.Fatalf("primary worktree/index differ from accepted candidate: %v", err)
			}
		})
	}
}
