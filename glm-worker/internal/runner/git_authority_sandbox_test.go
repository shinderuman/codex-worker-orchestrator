package runner

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestGitAuthoritySandboxSettingsProtectMetadataAndOwnedTemp(t *testing.T) {
	root := newGitAuthorityRepo(t)
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()

	policy := guard.bashSandboxPolicy()
	if policy == nil {
		t.Fatal("sandbox policy is nil")
	}
	if len(policy.allowWrite) != 1 || policy.allowWrite[0] != guard.workerTempDir {
		t.Fatalf("allowWrite = %#v", policy.allowWrite)
	}
	if guard.workerTempDir == guard.tempDir || filepath.Dir(guard.workerTempDir) != guard.tempDir {
		t.Fatalf("worker temp root = %q guard temp = %q", guard.workerTempDir, guard.tempDir)
	}
	for _, want := range guard.metadataPaths {
		if !containsSandboxString(policy.denyWrite, want) {
			t.Fatalf("denyWrite = %#v missing %q", policy.denyWrite, want)
		}
	}

	encoded, err := isolationSettings(t.TempDir(), policy)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		Sandbox struct {
			Enabled                  bool     `json:"enabled"`
			FailIfUnavailable        bool     `json:"failIfUnavailable"`
			AutoAllowBashIfSandboxed bool     `json:"autoAllowBashIfSandboxed"`
			AllowUnsandboxedCommands bool     `json:"allowUnsandboxedCommands"`
			ExcludedCommands         []string `json:"excludedCommands"`
			Filesystem               struct {
				AllowWrite []string `json:"allowWrite"`
				DenyWrite  []string `json:"denyWrite"`
			} `json:"filesystem"`
			Network struct {
				AllowedDomains      []string `json:"allowedDomains"`
				StrictAllowlist     bool     `json:"strictAllowlist"`
				AllowUnixSockets    []string `json:"allowUnixSockets"`
				AllowAllUnixSockets bool     `json:"allowAllUnixSockets"`
				AllowLocalBinding   bool     `json:"allowLocalBinding"`
			} `json:"network"`
		} `json:"sandbox"`
	}
	if err := json.Unmarshal([]byte(encoded), &settings); err != nil {
		t.Fatal(err)
	}
	if !settings.Sandbox.Enabled || !settings.Sandbox.FailIfUnavailable || !settings.Sandbox.AutoAllowBashIfSandboxed {
		t.Fatalf("sandbox = %#v", settings.Sandbox)
	}
	if settings.Sandbox.AllowUnsandboxedCommands || len(settings.Sandbox.ExcludedCommands) != 0 {
		t.Fatalf("sandbox escape = %#v", settings.Sandbox)
	}
	if len(settings.Sandbox.Filesystem.AllowWrite) != 1 || settings.Sandbox.Filesystem.AllowWrite[0] != filepath.ToSlash(guard.workerTempDir) {
		t.Fatalf("filesystem allowWrite = %#v", settings.Sandbox.Filesystem.AllowWrite)
	}
	if len(settings.Sandbox.Filesystem.DenyWrite) != len(policy.denyWrite) {
		t.Fatalf("filesystem denyWrite = %#v", settings.Sandbox.Filesystem.DenyWrite)
	}
	if len(settings.Sandbox.Network.AllowedDomains) != 0 || !settings.Sandbox.Network.StrictAllowlist || len(settings.Sandbox.Network.AllowUnixSockets) != 0 || settings.Sandbox.Network.AllowAllUnixSockets || settings.Sandbox.Network.AllowLocalBinding {
		t.Fatalf("network = %#v", settings.Sandbox.Network)
	}
}

func TestGitAuthoritySandboxProtectsWorktreeMarkerAndCommonMetadata(t *testing.T) {
	root := newGitAuthorityRepo(t)
	worktree := filepath.Join(t.TempDir(), "worktree")
	runGitAuthorityCommand(t, root, "worktree", "add", "-q", "-b", "sandbox-worktree", worktree)
	guard, err := prepareGitAuthorityGuard(worktree)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()

	gitDir, err := gitAuthorityPathOutput(guard.realGit, worktree, "rev-parse", "--absolute-git-dir")
	if err != nil {
		t.Fatal(err)
	}
	commonDir, err := gitAuthorityPathOutput(guard.realGit, worktree, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	gitMarker := filepath.Join(worktree, ".git")
	if gitDir == commonDir {
		t.Fatalf("worktree git dir unexpectedly equals common dir: %q", gitDir)
	}
	for _, want := range []string{gitDir, commonDir, gitMarker} {
		if !containsSandboxString(guard.metadataPaths, want) {
			t.Fatalf("metadataPaths = %#v missing %q", guard.metadataPaths, want)
		}
	}
}

func containsSandboxString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
