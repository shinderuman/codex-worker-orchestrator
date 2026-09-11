from pathlib import Path

path = Path("install.sh")
text = path.read_text()
old = 'codex_dir="${CODEX_CONFIG_DIR:-$HOME/.codex}"'
new = 'codex_dir="${CODEX_CONFIG_DIR:-${CODEX_HOME:-$HOME/.codex}}"'
if old not in text:
    raise SystemExit("install codex_dir anchor missing")
path.write_text(text.replace(old, new, 1))

path = Path("glm-worker/internal/config/config.go")
text = path.read_text()
old = '''\tstateHome := envOrDefault("GLM_WORKER_HOME", filepath.Join(home, ".glm-worker"))
\tpromptDir := envOrDefault("GLM_WORKER_PROMPT_DIR", filepath.Join(home, ".codex", "glm-worker", "prompts"))
\tcodexConfigDir := envOrDefault("CODEX_CONFIG_DIR", filepath.Join(home, ".codex"))
'''
new = '''\tstateHome := envOrDefault("GLM_WORKER_HOME", filepath.Join(home, ".glm-worker"))
\tcodexConfigDir := envOrDefault("CODEX_CONFIG_DIR", envOrDefault("CODEX_HOME", filepath.Join(home, ".codex")))
\tpromptDir := envOrDefault("GLM_WORKER_PROMPT_DIR", filepath.Join(codexConfigDir, "glm-worker", "prompts"))
'''
if old not in text:
    raise SystemExit("config Codex path anchor missing")
path.write_text(text.replace(old, new, 1))

path = Path("glm-worker/internal/config/config_test.go")
text = path.read_text()
anchor = '''func TestLoadDefaultsRepoSearchEnabled(t *testing.T) {'''
test = '''func TestLoadUsesCodexHomeForCodexPaths(t *testing.T) {
\trepository := filepath.Join(t.TempDir(), "repository")
\tif err := os.MkdirAll(repository, 0o700); err != nil {
\t\tt.Fatal(err)
\t}
\tcommand := exec.Command("git", "init", "--quiet", repository)
\tif output, err := command.CombinedOutput(); err != nil {
\t\tt.Fatalf("git init: %v: %s", err, output)
\t}
\tpreviousDirectory, err := os.Getwd()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := os.Chdir(repository); err != nil {
\t\tt.Fatal(err)
\t}
\tt.Cleanup(func() { _ = os.Chdir(previousDirectory) })

\tt.Setenv("HOME", t.TempDir())
\tcodexHome := filepath.Join(t.TempDir(), "codex-home")
\tt.Setenv("CODEX_HOME", codexHome)
\tt.Setenv("CODEX_CONFIG_DIR", "")
\tt.Setenv("GLM_WORKER_PROMPT_DIR", "")
\tloaded, err := Load()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif loaded.CodexConfigDir != codexHome {
\t\tt.Fatalf("CodexConfigDir = %q, want %q", loaded.CodexConfigDir, codexHome)
\t}
\twantPromptDir := filepath.Join(codexHome, "glm-worker", "prompts")
\tif loaded.PromptDir != wantPromptDir {
\t\tt.Fatalf("PromptDir = %q, want %q", loaded.PromptDir, wantPromptDir)
\t}
}

func TestLoadCodexConfigDirOverridesCodexHome(t *testing.T) {
\trepository := filepath.Join(t.TempDir(), "repository")
\tif err := os.MkdirAll(repository, 0o700); err != nil {
\t\tt.Fatal(err)
\t}
\tcommand := exec.Command("git", "init", "--quiet", repository)
\tif output, err := command.CombinedOutput(); err != nil {
\t\tt.Fatalf("git init: %v: %s", err, output)
\t}
\tpreviousDirectory, err := os.Getwd()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := os.Chdir(repository); err != nil {
\t\tt.Fatal(err)
\t}
\tt.Cleanup(func() { _ = os.Chdir(previousDirectory) })

\tt.Setenv("HOME", t.TempDir())
\tt.Setenv("CODEX_HOME", filepath.Join(t.TempDir(), "codex-home"))
\toverride := filepath.Join(t.TempDir(), "codex-config")
\tt.Setenv("CODEX_CONFIG_DIR", override)
\tloaded, err := Load()
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif loaded.CodexConfigDir != override {
\t\tt.Fatalf("CodexConfigDir = %q, want %q", loaded.CodexConfigDir, override)
\t}
}

'''
if anchor not in text:
    raise SystemExit("config test anchor missing")
path.write_text(text.replace(anchor, test + anchor, 1))

path = Path("tests/install_smoke.sh")
text = path.read_text()
old = '''\t\tCODEX_CONFIG_DIR="$home/.codex" \\
'''
new = '''\t\tCODEX_HOME="$home/.codex" \\
'''
if old not in text:
    raise SystemExit("smoke CODEX_CONFIG_DIR anchor missing")
path.write_text(text.replace(old, new, 1))
