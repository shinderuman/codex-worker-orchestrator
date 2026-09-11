from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f'anchor missing: {path}: {old[:100]!r}')
    p.write_text(text.replace(old, new, 1))

# A path-only legacy manifest came from the old installer, which also managed this config key.
path = 'glm-worker/internal/codexinstall/install_test.go'
replace_once(path,
'''\twriteTestFile(t, target, legacyContent)
\twriteTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("instructions/test.md\\n"))
\trunInstall(t, repo, codexDir)
''',
'''\twriteTestFile(t, target, legacyContent)
\twriteTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("instructions/test.md\\n"))
\twriteTestFile(t, filepath.Join(codexDir, "config.toml"), []byte(managedConfigKey+" = 21600000\\n"))
\trunInstall(t, repo, codexDir)
''')
replace_once(path,
'''\twriteTestFile(t, userTarget, userContent)
\twriteTestFile(t, filepath.Join(userDir, legacyManifestName), []byte("instructions/test.md\\n"))
\tvar stdout bytes.Buffer
''',
'''\twriteTestFile(t, userTarget, userContent)
\twriteTestFile(t, filepath.Join(userDir, legacyManifestName), []byte("instructions/test.md\\n"))
\twriteTestFile(t, filepath.Join(userDir, "config.toml"), []byte(managedConfigKey+" = 21600000\\n"))
\tvar stdout bytes.Buffer
''')

# Reject symlinked user config instead of following it.
replace_once('glm-worker/internal/codexinstall/config.go', 'info, err := os.Stat(path)', 'info, err := os.Lstat(path)')

# Reject a symlinked/non-regular legacy manifest.
state = Path('glm-worker/internal/codexinstall/state.go')
text = state.read_text()
old = '''func loadLegacyManifest(codexDir string) (legacyManifest, error) {
\tpath := filepath.Join(codexDir, legacyManifestName)
\tdata, err := os.ReadFile(path)
\tif errors.Is(err, os.ErrNotExist) {
\t\treturn legacyManifest{Paths: map[string]bool{}}, nil
\t}
\tif err != nil {
\t\treturn legacyManifest{}, fmt.Errorf("read legacy Codex managed-file manifest: %w", err)
\t}
'''
new = '''func loadLegacyManifest(codexDir string) (legacyManifest, error) {
\tpath := filepath.Join(codexDir, legacyManifestName)
\tinfo, err := os.Lstat(path)
\tif errors.Is(err, os.ErrNotExist) {
\t\treturn legacyManifest{Paths: map[string]bool{}}, nil
\t}
\tif err != nil {
\t\treturn legacyManifest{}, fmt.Errorf("stat legacy Codex managed-file manifest: %w", err)
\t}
\tif !info.Mode().IsRegular() {
\t\treturn legacyManifest{}, fmt.Errorf("legacy Codex managed-file manifest is not a regular file")
\t}
\tdata, err := os.ReadFile(path)
\tif err != nil {
\t\treturn legacyManifest{}, fmt.Errorf("read legacy Codex managed-file manifest: %w", err)
\t}
'''
if old not in text:
    raise SystemExit('legacy manifest anchor missing')
state.write_text(text.replace(old, new, 1))

# Add regression coverage for both symlink surfaces.
test = Path(path)
text = test.read_text()
anchor = 'func TestInstallRejectsSymlinkedManagedAncestor(t *testing.T) {\n'
new_tests = '''func TestInstallRejectsSymlinkedUserConfig(t *testing.T) {
\trepo := initInstallFixtureRepo(t)
\tcodexDir := t.TempDir()
\ttargetDir := t.TempDir()
\ttarget := filepath.Join(targetDir, "config.toml")
\toriginal := []byte("local_key = \\\"keep\\\"\\n")
\twriteTestFile(t, target, original)
\tif err := os.Symlink(target, filepath.Join(codexDir, "config.toml")); err != nil {
\t\tt.Fatal(err)
\t}
\tvar stdout bytes.Buffer
\tif err := Install(repo, codexDir, &stdout); err == nil {
\t\tt.Fatal("expected symlinked user config rejection")
\t}
\tassertFileBytes(t, target, original)
}

func TestInstallRejectsSymlinkedLegacyManifest(t *testing.T) {
\trepo := initInstallFixtureRepo(t)
\tcodexDir := t.TempDir()
\ttarget := filepath.Join(t.TempDir(), "manifest")
\twriteTestFile(t, target, []byte("instructions/test.md\\n"))
\tif err := os.Symlink(target, filepath.Join(codexDir, legacyManifestName)); err != nil {
\t\tt.Fatal(err)
\t}
\tvar stdout bytes.Buffer
\tif err := Install(repo, codexDir, &stdout); err == nil {
\t\tt.Fatal("expected symlinked legacy manifest rejection")
\t}
}

'''
if anchor not in text:
    raise SystemExit('symlink test anchor missing')
test.write_text(text.replace(anchor, new_tests + anchor, 1))
