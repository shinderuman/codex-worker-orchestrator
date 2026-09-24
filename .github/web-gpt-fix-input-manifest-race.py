from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if text.count(old) != 1:
        raise SystemExit(f"{path}: replacement count is {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    '''\tbefore, err := captureFixManifest(root, paths)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tworkspace, err := os.MkdirTemp("", "harnesslint-fix-*")
''',
    '''\tbefore, err := captureFixManifest(root, paths)
\tif err != nil {
\t\treturn Report{}, err
\t}
\tif err := verifyFixInputSnapshot(root, input, before); err != nil {
\t\treturn Report{}, err
\t}
\tworkspace, err := os.MkdirTemp("", "harnesslint-fix-*")
''',
)

replace_once(
    "glm-worker/internal/harnesslint/fix_isolation.go",
    '''func runIsolatedFixWorkspace(workspace string, before fixManifest, execute isolatedFixRun) (Report, fixManifest, []string, error) {
''',
    '''func verifyFixInputSnapshot(root string, input state.GitSnapshot, before fixManifest) error {
\tcurrent, err := state.CaptureGitSnapshot(root)
\tif err != nil {
\t\treturn err
\t}
\tif !state.EqualGitSnapshot(input, current) {
\t\treturn fmt.Errorf("quality fixer input changed during manifest capture")
\t}
\tif err := verifyFixManifest(root, before); err != nil {
\t\treturn fmt.Errorf("quality fixer input manifest does not match captured snapshot: %w", err)
\t}
\treturn nil
}

func runIsolatedFixWorkspace(workspace string, before fixManifest, execute isolatedFixRun) (Report, fixManifest, []string, error) {
''',
)

p = Path("glm-worker/internal/harnesslint/fix_isolation_test.go")
text = p.read_text()
replace_import = '''\t"strings"\n\t"testing"\n)\n'''
replace_import_with = '''\t"strings"\n\t"testing"\n\n\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"\n)\n'''
if text.count(replace_import) != 1:
    raise SystemExit("test import marker missing")
text = text.replace(replace_import, replace_import_with, 1)
marker = "func TestRunWithIsolatedFixesRejectsConcurrentLiveChange(t *testing.T) {"
insert = '''func TestVerifyFixInputSnapshotRejectsChangeBetweenSnapshotAndManifest(t *testing.T) {
\troot := newFixIsolationRepo(t)
\tfixture := filepath.Join(root, "fixture.go")
\tif err := os.WriteFile(fixture, []byte("before\\n"), 0o644); err != nil {
\t\tt.Fatal(err)
\t}
\tgitAddFixIsolationRepo(t, root)
\tinput, err := state.CaptureGitSnapshot(root)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tpaths, err := repositoryPaths(root)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := os.WriteFile(fixture, []byte("external\\n"), 0o644); err != nil {
\t\tt.Fatal(err)
\t}
\tbefore, err := captureFixManifest(root, paths)
\tif err != nil {
\t\tt.Fatal(err)
\t}
\tif err := verifyFixInputSnapshot(root, input, before); err == nil || !strings.Contains(err.Error(), "input changed during manifest capture") {
\t\tt.Fatalf("snapshot-to-manifest race was not rejected: %v", err)
\t}
}

'''
if text.count(marker) != 1:
    raise SystemExit("concurrent change test marker missing")
p.write_text(text.replace(marker, insert + marker, 1))
