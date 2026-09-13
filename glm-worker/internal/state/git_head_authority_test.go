package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestResolveGitHeadAuthorityClassifiesSupportedStates(t *testing.T) {
	repository := newResolveRepoHeadTestRepo(t)

	unborn, err := ResolveGitHeadAuthority("git", repository)
	if err != nil {
		t.Fatal(err)
	}
	if !unborn.Unborn || unborn.Detached || unborn.Head != "" || !strings.HasPrefix(unborn.SymbolicHead, "refs/heads/") {
		t.Fatalf("unborn authority = %#v", unborn)
	}

	commitResolveRepoHeadTestFile(t, repository)
	committed, err := ResolveGitHeadAuthority("git", repository)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Unborn || committed.Detached || committed.Head == "" || committed.SymbolicHead == "" {
		t.Fatalf("committed authority = %#v", committed)
	}

	if out, err := exec.Command("git", "-C", repository, "checkout", "--detach", "--quiet").CombinedOutput(); err != nil {
		t.Fatalf("git checkout --detach: %v: %s", err, out)
	}
	detached, err := ResolveGitHeadAuthority("git", repository)
	if err != nil {
		t.Fatal(err)
	}
	if detached.Unborn || !detached.Detached || detached.Head == "" || detached.SymbolicHead != "" {
		t.Fatalf("detached authority = %#v", detached)
	}
}

func TestResolveGitHeadAuthorityRejectsUnexpectedRevParseFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is Unix-oriented")
	}
	repository := newResolveRepoHeadTestRepo(t)
	commitResolveRepoHeadTestFile(t, repository)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	wrapper := filepath.Join(t.TempDir(), "git")
	script := "#!/bin/sh\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = 'HEAD^{commit}' ]; then exit 2; fi\n" +
		"done\n" +
		"exec \"" + realGit + "\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveGitHeadAuthority(wrapper, repository); err == nil {
		t.Fatal("unexpected rev-parse failure was downgraded to optional HEAD absence")
	}
}

func TestSnapshotAndBaselineRejectBrokenHeadAuthority(t *testing.T) {
	repository := newResolveRepoHeadTestRepo(t)
	refsHeads := filepath.Join(repository, ".git", "refs", "heads")
	if err := os.MkdirAll(refsHeads, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(refsHeads, "broken"), []byte("feedfacefeedfacefeedfacefeedfacefeedface\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git", "HEAD"), []byte("ref: refs/heads/broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := CaptureGitSnapshot(repository); err == nil {
		t.Fatal("snapshot accepted broken HEAD authority")
	}

	st := &StateStore{dir: t.TempDir()}
	for _, name := range []string{"baseline-head", "baseline-status", "baseline-worktree.patch", "baseline-index.patch", baselineUntrackedFile} {
		if err := st.Write(name, "stale"); err != nil {
			t.Fatal(err)
		}
	}
	if err := CaptureGitBaseline(config.AppConfig{RepoRoot: repository}, st); err == nil {
		t.Fatal("baseline accepted broken HEAD authority")
	}
	if evidence := st.BaselineEvidence(); evidence != nil {
		t.Fatalf("failed baseline left consumable evidence: %#v", evidence)
	}
}
