package uninstallcmd

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/cliinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/settingsmerge"
)

func TestUninstallRejectsUntrustedSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "owned")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "user-alias")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if err := rejectSymlinkComponents(filepath.Join(alias, "target")); err == nil ||
		!strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("untrusted symlink was accepted: %v", err)
	}
}

func TestUninstallAcceptsCanonicalMacOSVarAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires the actual macOS /var -> /private/var alias")
	}
	realTemp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(realTemp, "/private/var/folders/") {
		t.Fatalf("Darwin acceptance requires the actual /var/folders temp directory; got %q", realTemp)
	}
	varTemp := "/var" + strings.TrimPrefix(realTemp, "/private/var")
	root, err := os.MkdirTemp(varTemp, "uninstall-var-alias-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CLAUDE_SETTINGS_FILE", filepath.Join(root, ".claude", "settings.json"))

	if err := rejectSymlinkComponents(root); err != nil {
		t.Fatalf("standard /var alias rejected: %v", err)
	}
	canonical := "/private/var" + strings.TrimPrefix(root, "/var")
	if err := rejectSymlinkComponents(canonical); err != nil {
		t.Fatalf("physical /private/var path rejected: %v", err)
	}
	repo := createConcurrentRetireFixture(t)
	buildDir := filepath.Join(root, "build")
	for _, name := range []string{
		"glm-worker", "glm-parent-action", "glm-publication-guard",
		"glm-codex-context", "commentlint", "harnesslint",
	} {
		writeConcurrentRetireFile(t, filepath.Join(buildDir, name), []byte(name+"-fixture"), 0o755)
	}
	install := func(binDir, codexDir, claudePath string) {
		t.Helper()
		if _, err := cliinstall.Install(buildDir, binDir); err != nil {
			t.Fatalf("install CLI: %v", err)
		}
		if err := codexinstall.Install(repo, codexDir, io.Discard); err != nil {
			t.Fatalf("install Codex: %v", err)
		}
		if _, err := settingsmerge.MergeFiles(claudePath,
			filepath.Join(repo, "claude", "settings-managed.json"), ""); err != nil {
			t.Fatalf("install Claude: %v", err)
		}
		runConcurrentRetireCommand(t, "sh", filepath.Join(repo, "scripts", "manage-pull-hook.sh"),
			"install", repo, filepath.Join(binDir, "glm-publication-guard"))
	}
	binDir := filepath.Join(root, "bin")
	codexDir := filepath.Join(root, ".codex")
	claudePath := filepath.Join(root, ".claude", "settings.json")
	install(binDir, codexDir, claudePath)
	if err := run(repo, binDir, codexDir, io.Discard); err != nil {
		t.Fatalf("uninstall using /var alias: %v", err)
	}
	if err := run(repo, binDir, codexDir, io.Discard); err != nil {
		t.Fatalf("repeat uninstall using /var alias: %v", err)
	}
	assertConcurrentRetireAbsent(t, filepath.Join(binDir, "glm-worker"))
	assertConcurrentRetireAbsent(t, filepath.Join(codexDir, "codex-worker-orchestrator", "install-state.json"))

	physicalBin := filepath.Join(canonical, "bin")
	physicalCodex := filepath.Join(canonical, ".codex")
	physicalClaude := filepath.Join(canonical, ".claude", "settings.json")
	t.Setenv("CLAUDE_SETTINGS_FILE", physicalClaude)
	install(physicalBin, physicalCodex, physicalClaude)
	if err := run(repo, physicalBin, physicalCodex, io.Discard); err != nil {
		t.Fatalf("uninstall using /private/var physical path: %v", err)
	}
	install(physicalBin, physicalCodex, physicalClaude)
	assertConcurrentRetirePresent(t, filepath.Join(physicalBin, "glm-publication-guard"))
	assertConcurrentRetirePresent(t, filepath.Join(physicalCodex, "codex-worker-orchestrator", "install-state.json"))

	unsafe := filepath.Join(root, "user-alias")
	if err := os.Symlink(physicalBin, unsafe); err != nil {
		t.Fatal(err)
	}
	if err := run(repo, unsafe, physicalCodex, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("untrusted alias was accepted by uninstall: %v", err)
	}
	assertConcurrentRetirePresent(t, filepath.Join(physicalBin, "glm-publication-guard"))
}

func TestUninstallRejectsUserAliasToPrivateVar(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("requires macOS /private/var")
	}
	alias := filepath.Join(t.TempDir(), "fake-var")
	if err := os.Symlink("/private/var", alias); err != nil {
		t.Fatal(err)
	}
	if err := rejectSymlinkComponents(filepath.Join(alias, "folders")); err == nil ||
		!strings.Contains(err.Error(), "refusing symlink path component") {
		t.Fatalf("user alias to the same physical directory was accepted: %v", err)
	}
}
