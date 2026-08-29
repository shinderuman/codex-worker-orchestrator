from pathlib import Path


def replace(path, old, new):
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f"replacement point not found: {path}")
    p.write_text(text.replace(old, new, 1))


replace(
    "glm-worker/internal/runner/git_authority_guard.go",
    """\ttempDir    string\n\tproxyPath  string\n\tdenyPath   string\n\tattemptLog string\n\tbefore     gitAuthoritySnapshot\n""",
    """\ttempDir       string\n\tworkerTempDir string\n\tproxyPath     string\n\tdenyPath      string\n\tattemptLog    string\n\tmetadataPaths []string\n\tbefore        gitAuthoritySnapshot\n""",
)
replace(
    "glm-worker/internal/runner/git_authority_guard.go",
    """\tguard := &gitAuthorityGuard{repoRoot: repoRoot, realGit: realGit, before: before}\n\tif !before.active {\n\t\treturn guard, nil\n\t}\n\tif err := guard.prepareProxy(); err != nil {\n""",
    """\tguard := &gitAuthorityGuard{repoRoot: repoRoot, realGit: realGit, before: before}\n\tif !before.active {\n\t\treturn guard, nil\n\t}\n\tmetadataPaths, err := resolveGitAuthorityMetadataPaths(realGit, repoRoot)\n\tif err != nil {\n\t\treturn nil, &GitAuthorityGuardError{Stage: \"resolve-metadata\", Cause: err}\n\t}\n\tguard.metadataPaths = metadataPaths\n\tif err := guard.prepareProxy(); err != nil {\n""",
)
replace(
    "glm-worker/internal/runner/git_authority_guard.go",
    """\tg.tempDir = tempDir\n\tg.proxyPath = filepath.Join(tempDir, \"git\")\n""",
    """\tg.tempDir = tempDir\n\tg.workerTempDir = filepath.Join(tempDir, \"worker\")\n\tif err := os.MkdirAll(g.workerTempDir, 0o700); err != nil {\n\t\treturn &GitAuthorityGuardError{Stage: \"prepare-command-proxy\", Cause: err}\n\t}\n\tg.proxyPath = filepath.Join(tempDir, \"git\")\n""",
)
replace(
    "glm-worker/internal/runner/git_authority_guard.go",
    "gitAuthorityProxyScript(g.realGit, g.attemptLog, g.repoRoot, g.tempDir)",
    "gitAuthorityProxyScript(g.realGit, g.attemptLog, g.repoRoot, g.workerTempDir)",
)

replace(
    "glm-worker/internal/runner/git_authority_claude_wrapper.go",
    """\tresult += \"GLM_WORKER_GIT_TEMP_ROOT=\" + shellSingleQuote(g.tempDir) + \"\\nexport GLM_WORKER_GIT_TEMP_ROOT\\n\"\n\tresult += \"TMPDIR=\" + shellSingleQuote(g.tempDir) + \"\\nexport TMPDIR\\n\"\n\tresult += \"TMP=\" + shellSingleQuote(g.tempDir) + \"\\nexport TMP\\n\"\n\tresult += \"TEMP=\" + shellSingleQuote(g.tempDir) + \"\\nexport TEMP\\n\"\n""",
    """\tresult += \"GLM_WORKER_GIT_TEMP_ROOT=\" + shellSingleQuote(g.workerTempDir) + \"\\nexport GLM_WORKER_GIT_TEMP_ROOT\\n\"\n\tresult += \"CLAUDE_CODE_TMPDIR=\" + shellSingleQuote(g.workerTempDir) + \"\\nexport CLAUDE_CODE_TMPDIR\\n\"\n\tresult += \"TMPDIR=\" + shellSingleQuote(g.workerTempDir) + \"\\nexport TMPDIR\\n\"\n\tresult += \"TMP=\" + shellSingleQuote(g.workerTempDir) + \"\\nexport TMP\\n\"\n\tresult += \"TEMP=\" + shellSingleQuote(g.workerTempDir) + \"\\nexport TEMP\\n\"\n""",
)

replace(
    "glm-worker/internal/runner/instruction_surface_runner.go",
    """\t\tcopyBase := *r.base\n\t\tcopyBase.config.ClaudeBin = wrappedClaude\n\t\tcallBase = &copyBase\n""",
    """\t\tcopyBase := *r.base\n\t\tcopyBase.config.ClaudeBin = wrappedClaude\n\t\tcopyBase.bashSandbox = gitGuard.bashSandboxPolicy()\n\t\tcallBase = &copyBase\n""",
)

replace(
    "glm-worker/internal/runner/runner.go",
    """type ClaudeRunner struct {\n\tconfig config.AppConfig\n\tstate  *state.StateStore\n\tstop   *StopController\n}\n""",
    """type ClaudeRunner struct {\n\tconfig      config.AppConfig\n\tstate       *state.StateStore\n\tstop        *StopController\n\tbashSandbox *gitBashSandboxPolicy\n}\n""",
)
replace(
    "glm-worker/internal/runner/runner.go",
    'const isolationPolicyVersion = "claude-isolation-1"',
    'const isolationPolicyVersion = "claude-isolation-2"',
)
replace(
    "glm-worker/internal/runner/runner.go",
    "isolationSettings(r.config.ClaudeConfigDir)",
    "isolationSettings(r.config.ClaudeConfigDir, r.bashSandbox)",
)
replace(
    "glm-worker/internal/runner/runner.go",
    "func isolationSettings(claudeConfigDir string) (string, error) {",
    "func isolationSettings(claudeConfigDir string, sandbox *gitBashSandboxPolicy) (string, error) {",
)
needle = """\tsettings := map[string]any{\n\t\t\"claudeMdExcludes\": []string{\n\t\t\t\"**/CLAUDE.md\",\n\t\t\t\"**/CLAUDE.local.md\",\n\t\t\tfilepath.Join(configDir, \"CLAUDE.md\"),\n\t\t\tfilepath.Join(configDir, \"rules\", \"**\"),\n\t\t},\n\t\t\"autoMemoryEnabled\":    false,\n\t\t\"disableAllHooks\":      true,\n\t\t\"disableBundledSkills\": true,\n\t\t\"disableWorkflows\":     true,\n\t}\n"""
replacement = needle + """\tif sandbox != nil {\n\t\tsettings[\"sandbox\"] = sandbox.settings()\n\t}\n"""
replace("glm-worker/internal/runner/runner.go", needle, replacement)

replace(
    "glm-worker/internal/runner/probe.go",
    "isolationSettings(r.config.ClaudeConfigDir)",
    "isolationSettings(r.config.ClaudeConfigDir, nil)",
)

p = Path("glm-worker/internal/runner/runner_test.go")
s = p.read_text()
if "isolationSettings(configDir)" not in s:
    raise SystemExit("runner_test isolationSettings call not found")
p.write_text(s.replace("isolationSettings(configDir)", "isolationSettings(configDir, nil)"))

replace(
    "glm-worker/internal/runner/git_authority_temp_root_test.go",
    """case \"$TMPDIR\" in\n  \"$GLM_WORKER_GIT_TEMP_ROOT\"|\"$GLM_WORKER_GIT_TEMP_ROOT\"/*) ;;\n  *) exit 20 ;;\nesac\nowned=$(mktemp -d)\ngit init \"$owned/repo\" >/dev/null 2>&1\n""",
    """[ \"$CLAUDE_CODE_TMPDIR\" = \"$GLM_WORKER_GIT_TEMP_ROOT\" ] || exit 19\ncase \"$TMPDIR\" in\n  \"$GLM_WORKER_GIT_TEMP_ROOT\"|\"$GLM_WORKER_GIT_TEMP_ROOT\"/*) ;;\n  *) exit 20 ;;\nesac\nowned=$(mktemp -d)\ngit init \"$owned/repo\" >/dev/null 2>&1\nsandbox_tmp=\"$GLM_WORKER_GIT_TEMP_ROOT/claude-test\"\nmkdir -p \"$sandbox_tmp\"\nTMPDIR=\"$sandbox_tmp\" git init \"$sandbox_tmp/repo\" >/dev/null 2>&1\n""",
)

Path("glm-worker/internal/runner/git_authority_sandbox.go").write_text(r'''package runner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type gitBashSandboxPolicy struct {
	allowWrite []string
	denyWrite  []string
}

func (g *gitAuthorityGuard) bashSandboxPolicy() *gitBashSandboxPolicy {
	if g == nil || !g.before.active {
		return nil
	}
	return &gitBashSandboxPolicy{
		allowWrite: []string{g.workerTempDir},
		denyWrite:  append([]string(nil), g.metadataPaths...),
	}
}

func (p *gitBashSandboxPolicy) settings() map[string]any {
	return map[string]any{
		"enabled":                  true,
		"failIfUnavailable":        true,
		"autoAllowBashIfSandboxed": true,
		"allowUnsandboxedCommands": false,
		"excludedCommands":         []string{},
		"filesystem": map[string]any{
			"allowWrite": sandboxAbsolutePaths(p.allowWrite),
			"denyWrite":  sandboxAbsolutePaths(p.denyWrite),
		},
		"network": map[string]any{
			"allowedDomains":      []string{},
			"strictAllowlist":     true,
			"allowUnixSockets":    []string{},
			"allowAllUnixSockets": false,
			"allowLocalBinding":   false,
		},
	}
}

func resolveGitAuthorityMetadataPaths(realGit, repoRoot string) ([]string, error) {
	gitDir, err := gitAuthorityPathOutput(realGit, repoRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	commonDir, err := gitAuthorityPathOutput(realGit, repoRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	gitMarker, err := filepath.Abs(filepath.Join(repoRoot, ".git"))
	if err != nil {
		return nil, err
	}
	paths := uniqueSortedStrings([]string{gitDir, commonDir, filepath.Clean(gitMarker)})
	if len(paths) == 0 {
		return nil, fmt.Errorf("git metadata pathが空です")
	}
	return paths, nil
}

func gitAuthorityPathOutput(realGit, repoRoot string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoRoot}, args...)
	output, err := exec.Command(realGit, commandArgs...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", fmt.Errorf("git %s returned empty path", strings.Join(args, " "))
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoRoot, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func sandboxAbsolutePaths(paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		result = append(result, filepath.ToSlash(filepath.Clean(path)))
	}
	sort.Strings(result)
	return result
}
''')

Path("glm-worker/internal/runner/git_authority_sandbox_test.go").write_text(r'''package runner

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
		if !containsString(policy.denyWrite, want) {
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
		if !containsString(guard.metadataPaths, want) {
			t.Fatalf("metadataPaths = %#v missing %q", guard.metadataPaths, want)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
''')
