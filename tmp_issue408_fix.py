from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f'anchor missing in {path}: {old[:80]!r}')
    p.write_text(text.replace(old, new, 1))


Path('glm-worker/cmd/codex-install/main.go').write_text('''package main

import (
\t"fmt"
\t"os"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstallcmd"
)

func main() {
\tif err := codexinstallcmd.Run(os.Args[1:], os.Stdout); err != nil {
\t\tfmt.Fprintln(os.Stderr, err)
\t\tos.Exit(1)
\t}
}
''')

Path('glm-worker/internal/codexinstallcmd').mkdir(parents=True, exist_ok=True)
Path('glm-worker/internal/codexinstallcmd/run.go').write_text('''package codexinstallcmd

import (
\t"flag"
\t"fmt"
\t"io"

\t"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/codexinstall"
)

func Run(args []string, stdout io.Writer) error {
\tflags := flag.NewFlagSet("codex-install", flag.ContinueOnError)
\tflags.SetOutput(io.Discard)
\trepoRoot := flags.String("repo-root", "", "")
\tcodexDir := flags.String("codex-dir", "", "")
\tif err := flags.Parse(args); err != nil {
\t\treturn usageError()
\t}
\tif *repoRoot == "" || *codexDir == "" || flags.NArg() != 0 {
\t\treturn usageError()
\t}
\treturn codexinstall.Install(*repoRoot, *codexDir, stdout)
}

func usageError() error {
\treturn fmt.Errorf("usage: codex-install --repo-root PATH --codex-dir PATH")
}
''')

replace_once(
    'glm-worker/internal/codexinstall/config.go',
    '"os"\n\t"path/filepath"',
    '"os"\n\t"os/exec"\n\t"path/filepath"',
)
replace_once(
    'glm-worker/internal/codexinstall/config.go',
    'const managedConfigKey = "background_terminal_max_timeout"\n\ntype configAssignment struct {',
    'type configAssignment struct {',
)
replace_once(
    'glm-worker/internal/codexinstall/config.go',
    'type configInstallPlan struct {\n\tPath      string\n\tMode      os.FileMode\n\tNext      []byte\n\tChanged   bool\n\tRecord    *managedConfigRecord\n\tPreserved bool\n}\n\nfunc buildConfigInstallPlan(repoRoot, codexDir string, state installState) (configInstallPlan, error) {',
    'type configInstallPlan struct {\n\tPath      string\n\tMode      os.FileMode\n\tNext      []byte\n\tChanged   bool\n\tRecord    *managedConfigRecord\n\tPreserved bool\n}\n\nconst managedConfigKey = "background_terminal_max_timeout"\n\nfunc buildConfigInstallPlan(repoRoot, codexDir string, state installState, stateExists bool, legacy legacyManifest) (configInstallPlan, error) {',
)
replace_once(
    'glm-worker/internal/codexinstall/config.go',
    'return planUnownedConfig(plan, data, current, currentFound, managedAssignment, managedFound)',
    'return planUnownedConfig(repoRoot, plan, data, current, currentFound, managedAssignment, managedFound, !stateExists && legacy.Present)',
)
old_unowned = '''func planUnownedConfig(plan configInstallPlan, data []byte, current configAssignment, currentFound bool, managed configAssignment, managedFound bool) (configInstallPlan, error) {
\tif !managedFound {
\t\treturn plan, nil
\t}
\tif currentFound {
\t\tif current.Value != managed.Value {
\t\t\treturn configInstallPlan{}, fmt.Errorf("refusing to overwrite user-owned Codex config key %s: current=%s managed=%s", managedConfigKey, current.Value, managed.Value)
\t\t}
\t\treturn plan, nil
\t}
\tnextLine := assignmentLine(managedConfigKey, managed.Value, "\\n")
\tplan.Next = append([]byte(nextLine), data...)
\tplan.Changed = true
\tplan.Record = &managedConfigRecord{Value: managed.Value, LineSHA256: digestBytes([]byte(nextLine))}
\treturn plan, nil
}
'''
new_unowned = '''func planUnownedConfig(repoRoot string, plan configInstallPlan, data []byte, current configAssignment, currentFound bool, managed configAssignment, managedFound bool, legacyInstall bool) (configInstallPlan, error) {
\tif !managedFound {
\t\treturn plan, nil
\t}
\tif legacyInstall {
\t\treturn planLegacyConfig(repoRoot, plan, data, current, currentFound, managed)
\t}
\tif currentFound {
\t\tif current.Value != managed.Value {
\t\t\treturn configInstallPlan{}, fmt.Errorf("refusing to overwrite user-owned Codex config key %s: current=%s managed=%s", managedConfigKey, current.Value, managed.Value)
\t\t}
\t\treturn plan, nil
\t}
\tnextLine := assignmentLine(managedConfigKey, managed.Value, "\\n")
\tplan.Next = append([]byte(nextLine), data...)
\tplan.Changed = true
\tplan.Record = &managedConfigRecord{Value: managed.Value, LineSHA256: digestBytes([]byte(nextLine))}
\treturn plan, nil
}

func planLegacyConfig(repoRoot string, plan configInstallPlan, data []byte, current configAssignment, currentFound bool, managed configAssignment) (configInstallPlan, error) {
\tif !currentFound {
\t\treturn configInstallPlan{}, fmt.Errorf("legacy managed Codex config key is missing; refusing silent recreation: %s", managedConfigKey)
\t}
\tmatches, err := legacyManagedConfigValueMatchesRepositoryHistory(repoRoot, current.Value)
\tif err != nil {
\t\treturn configInstallPlan{}, err
\t}
\tif !matches {
\t\treturn configInstallPlan{}, fmt.Errorf("legacy managed Codex config key no longer matches repository history: %s", managedConfigKey)
\t}
\tnextLine := assignmentLine(managedConfigKey, managed.Value, lineEnding(current.Line))
\tplan.Next = replaceAssignmentLine(data, current.Index, nextLine)
\tplan.Changed = string(plan.Next) != string(data)
\tplan.Record = &managedConfigRecord{Value: managed.Value, LineSHA256: digestBytes([]byte(nextLine))}
\treturn plan, nil
}

func legacyManagedConfigValueMatchesRepositoryHistory(repoRoot, value string) (bool, error) {
\tconst sourcePath = "codex/config-managed.toml"
\tcurrent, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(sourcePath)))
\tif err == nil {
\t\tassignment, found, parseErr := findTopLevelAssignment(current, managedConfigKey)
\t\tif parseErr != nil {
\t\t\treturn false, parseErr
\t\t}
\t\tif found && assignment.Value == value {
\t\t\treturn true, nil
\t\t}
\t}
\tcommand := exec.Command("git", "-C", repoRoot, "log", "--format=%H", "--", sourcePath)
\toutput, err := command.Output()
\tif err != nil {
\t\treturn false, fmt.Errorf("enumerate managed Codex config history: %w", err)
\t}
\tfor _, revision := range strings.Fields(string(output)) {
\t\tshow := exec.Command("git", "-C", repoRoot, "show", revision+":"+sourcePath)
\t\tdata, showErr := show.Output()
\t\tif showErr != nil {
\t\t\tcontinue
\t\t}
\t\tassignment, found, parseErr := findTopLevelAssignment(data, managedConfigKey)
\t\tif parseErr != nil {
\t\t\treturn false, parseErr
\t\t}
\t\tif found && assignment.Value == value {
\t\t\treturn true, nil
\t\t}
\t}
\treturn false, nil
}
'''
replace_once('glm-worker/internal/codexinstall/config.go', old_unowned, new_unowned)
replace_once(
    'glm-worker/internal/codexinstall/config.go',
    'return nil, 0, fmt.Errorf("Codex config is not a regular file: %s", path)',
    'return nil, 0, fmt.Errorf("codex config is not a regular file: %s", path)',
)

Path('glm-worker/internal/codexinstall/install.go').write_text('''package codexinstall

import (
\t"fmt"
\t"io"
\t"os"
\t"path/filepath"
)

type installPreparation struct {
\tcodexDir    string
\tstateExists bool
\tlegacy      legacyManifest
\tfilePlan    fileInstallPlan
\tconfigPlan  configInstallPlan
}

func Install(repoRoot, codexDir string, stdout io.Writer) error {
\tpreparation, err := prepareInstall(repoRoot, codexDir)
\tif err != nil {
\t\treturn err
\t}
\treturn applyInstall(preparation, stdout)
}

func prepareInstall(repoRoot, codexDir string) (installPreparation, error) {
\trepoRoot = filepath.Clean(repoRoot)
\tcodexDir = filepath.Clean(codexDir)
\tif repoRoot == "." || codexDir == "." {
\t\treturn installPreparation{}, fmt.Errorf("repo root and Codex directory must be explicit paths")
\t}
\tstate, stateExists, err := loadState(codexDir)
\tif err != nil {
\t\treturn installPreparation{}, err
\t}
\tlegacy, err := legacyForInstall(codexDir, stateExists)
\tif err != nil {
\t\treturn installPreparation{}, err
\t}
\tfilePlan, err := buildFileInstallPlan(repoRoot, codexDir, state, stateExists, legacy)
\tif err != nil {
\t\treturn installPreparation{}, err
\t}
\tconfigPlan, err := buildConfigInstallPlan(repoRoot, codexDir, state, stateExists, legacy)
\tif err != nil {
\t\treturn installPreparation{}, err
\t}
\treturn installPreparation{codexDir: codexDir, stateExists: stateExists, legacy: legacy, filePlan: filePlan, configPlan: configPlan}, nil
}

func legacyForInstall(codexDir string, stateExists bool) (legacyManifest, error) {
\tif stateExists {
\t\treturn legacyManifest{Paths: map[string]bool{}}, nil
\t}
\treturn loadLegacyManifest(codexDir)
}

func applyInstall(preparation installPreparation, stdout io.Writer) error {
\toutput := func(format string, args ...any) {
\t\t_, _ = fmt.Fprintf(stdout, format, args...)
\t}
\tfiles, err := applyFileInstallPlan(preparation.codexDir, preparation.filePlan, output)
\tif err != nil {
\t\treturn err
\t}
\tif err := applyConfigInstallPlan(preparation.configPlan, output); err != nil {
\t\treturn err
\t}
\tnext := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
\tif preparation.configPlan.Record != nil {
\t\tnext.Config[managedConfigKey] = *preparation.configPlan.Record
\t}
\tif err := writeState(preparation.codexDir, next); err != nil {
\t\treturn err
\t}
\tif preparation.stateExists {
\t\treturn nil
\t}
\treturn removeLegacyManifest(preparation.codexDir, preparation.legacy.Present)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
\tdirectory := filepath.Dir(path)
\tif err := os.MkdirAll(directory, 0o755); err != nil {
\t\treturn err
\t}
\tfile, err := os.CreateTemp(directory, ".codex-worker-orchestrator-install-*")
\tif err != nil {
\t\treturn err
\t}
\ttemporary := file.Name()
\tdefer func() { _ = os.Remove(temporary) }()
\tif _, err := file.Write(data); err != nil {
\t\t_ = file.Close()
\t\treturn err
\t}
\tif err := file.Chmod(mode); err != nil {
\t\t_ = file.Close()
\t\treturn err
\t}
\tif err := file.Close(); err != nil {
\t\treturn err
\t}
\treturn os.Rename(temporary, path)
}
''')

replace_once(
    'glm-worker/internal/codexinstall/files.go',
    '''func requireCurrentPathOwnership(repoRoot, codexDir string, file desiredFile, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
\ttarget := filepath.Join(codexDir, filepath.FromSlash(file.Path))
\tinfo, err := os.Lstat(target)
\tif errors.Is(err, os.ErrNotExist) {
\t\tif stateExists {
\t\t\tif _, owned := records[file.Path]; owned {
\t\t\t\treturn fmt.Errorf("managed Codex file is missing; refusing silent recreation: %s", file.Path)
\t\t\t}
\t\t} else if legacy.Paths[file.Path] {
\t\t\treturn fmt.Errorf("legacy managed Codex file is missing; refusing silent recreation: %s", file.Path)
\t\t}
\t\treturn nil
\t}
\tif err != nil {
\t\treturn fmt.Errorf("stat installed Codex file %s: %w", file.Path, err)
\t}
\tif !info.Mode().IsRegular() {
\t\treturn fmt.Errorf("refusing to overwrite non-regular Codex path %s", file.Path)
\t}
\tif record, owned := records[file.Path]; owned {
\t\tactual, err := digestFile(target)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tif actual != record.SHA256 {
\t\t\treturn fmt.Errorf("managed Codex file was modified after install; refusing to overwrite: %s", file.Path)
\t\t}
\t\treturn nil
\t}
\tif !stateExists && legacy.Paths[file.Path] {
\t\tmatches, err := managedPathMatchesRepositoryHistory(repoRoot, file.Path, target)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tif matches {
\t\t\treturn nil
\t\t}
\t\treturn fmt.Errorf("legacy managed Codex file no longer matches repository history; refusing to overwrite: %s", file.Path)
\t}
\treturn fmt.Errorf("refusing to overwrite preexisting Codex file without tool ownership: %s", file.Path)
}
''',
    '''func requireCurrentPathOwnership(repoRoot, codexDir string, file desiredFile, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
\ttarget := filepath.Join(codexDir, filepath.FromSlash(file.Path))
\tinfo, err := os.Lstat(target)
\tif errors.Is(err, os.ErrNotExist) {
\t\treturn requireMissingPathOwnership(file.Path, records, stateExists, legacy)
\t}
\tif err != nil {
\t\treturn fmt.Errorf("stat installed Codex file %s: %w", file.Path, err)
\t}
\tif !info.Mode().IsRegular() {
\t\treturn fmt.Errorf("refusing to overwrite non-regular Codex path %s", file.Path)
\t}
\treturn requireExistingPathOwnership(repoRoot, target, file.Path, records, stateExists, legacy)
}

func requireMissingPathOwnership(path string, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
\tif stateExists {
\t\tif _, owned := records[path]; owned {
\t\t\treturn fmt.Errorf("managed Codex file is missing; refusing silent recreation: %s", path)
\t\t}
\t\treturn nil
\t}
\tif legacy.Paths[path] {
\t\treturn fmt.Errorf("legacy managed Codex file is missing; refusing silent recreation: %s", path)
\t}
\treturn nil
}

func requireExistingPathOwnership(repoRoot, target, path string, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
\tif record, owned := records[path]; owned {
\t\tactual, err := digestFile(target)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tif actual != record.SHA256 {
\t\t\treturn fmt.Errorf("managed Codex file was modified after install; refusing to overwrite: %s", path)
\t\t}
\t\treturn nil
\t}
\tif !stateExists && legacy.Paths[path] {
\t\tmatches, err := managedPathMatchesRepositoryHistory(repoRoot, path, target)
\t\tif err != nil {
\t\t\treturn err
\t\t}
\t\tif matches {
\t\t\treturn nil
\t\t}
\t\treturn fmt.Errorf("legacy managed Codex file no longer matches repository history; refusing to overwrite: %s", path)
\t}
\treturn fmt.Errorf("refusing to overwrite preexisting Codex file without tool ownership: %s", path)
}
''',
)

state_path = Path('glm-worker/internal/codexinstall/state.go')
state = state_path.read_text()
const_block = '''const (
\tstateVersion       = 1
\tstateRelativePath  = "codex-worker-orchestrator/install-state.json"
\tlegacyManifestName = ".codex-config-managed-files"
)

'''
if const_block not in state:
    raise SystemExit('state const block missing')
state = state.replace(const_block, '', 1)
insert_after = '''type legacyManifest struct {
\tPresent bool
\tPaths   map[string]bool
}
'''
replacement = insert_after + '''
const (
\tstateVersion       = 1
\tstateRelativePath  = "codex-worker-orchestrator/install-state.json"
\tlegacyManifestName = ".codex-config-managed-files"
)
'''
if insert_after not in state:
    raise SystemExit('legacy type anchor missing')
state = state.replace(insert_after, replacement, 1)
state = state.replace(
    '''\tfor _, record := range state.Files {
\t\tif record.Path == "" || record.SHA256 == "" || seen[record.Path] {
\t\t\treturn installState{}, false, fmt.Errorf("invalid Codex install state file record %q", record.Path)
\t\t}
\t\tseen[record.Path] = true
\t}
\treturn state, true, nil
''',
    '''\tfor _, record := range state.Files {
\t\tif err := validateManagedRelativePath(record.Path); err != nil || record.SHA256 == "" || seen[record.Path] {
\t\t\treturn installState{}, false, fmt.Errorf("invalid Codex install state file record %q", record.Path)
\t\t}
\t\tseen[record.Path] = true
\t}
\tfor key, record := range state.Config {
\t\tif key != managedConfigKey || record.Value == "" || record.LineSHA256 == "" {
\t\t\treturn installState{}, false, fmt.Errorf("invalid Codex install state config record %q", key)
\t\t}
\t}
\treturn state, true, nil
''',
    1,
)
state = state.replace(
    '''\t\tif !supportedLegacyManagedPath(path) {
\t\t\treturn legacyManifest{}, fmt.Errorf("legacy Codex managed-file manifest contains unsupported path %q", path)
\t\t}
''',
    '''\t\tif err := validateManagedRelativePath(path); err != nil || !supportedLegacyManagedPath(path) {
\t\t\treturn legacyManifest{}, fmt.Errorf("legacy Codex managed-file manifest contains unsupported path %q", path)
\t\t}
''',
    1,
)
state += '''
func validateManagedRelativePath(path string) error {
\tif path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\\\") {
\t\treturn fmt.Errorf("invalid managed Codex relative path")
\t}
\tclean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
\tif clean == "." || clean != path || strings.HasPrefix(clean, "../") {
\t\treturn fmt.Errorf("invalid managed Codex relative path")
\t}
\treturn nil
}
'''
state_path.write_text(state)

replace_once(
    'glm-worker/internal/codexinstall/install_test.go',
    '''\terr := Install(repo, userDir, &stdout)
\tif err == nil || !strings.Contains(err.Error(), "no longer matches repository history") {
\t\tt.Fatalf("expected legacy ownership conflict, got %v", err)
\t}
''',
    '''\terr := Install(repo, userDir, &stdout)
\tif err == nil {
\t\tt.Fatal("expected legacy ownership conflict")
\t}
''',
)
insert_test_anchor = '''func runInstall(t *testing.T, repo, codexDir string) {
'''
new_tests = '''func TestInstallMigratesLegacyManagedConfigOwnership(t *testing.T) {
\trepo := initInstallFixtureRepo(t)
\tmanagedPath := filepath.Join(repo, "codex", "config-managed.toml")
\twriteTestFile(t, managedPath, []byte(managedConfigKey+" = 100\\n"))
\tcommitFixtureRepo(t, repo, "legacy managed config")
\twriteTestFile(t, managedPath, []byte(managedConfigKey+" = 200\\n"))
\tcommitFixtureRepo(t, repo, "current managed config")

\tcodexDir := t.TempDir()
\twriteTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("AGENTS.md\\n"))
\tconfigPath := filepath.Join(codexDir, "config.toml")
\twriteTestFile(t, configPath, []byte(managedConfigKey+" = 100\\nlocal_key = \\\"keep\\\"\\n"))
\trunInstall(t, repo, codexDir)
\tif !bytes.Contains(readTestFile(t, configPath), []byte(managedConfigKey+" = 200")) {
\t\tt.Fatal("legacy managed config was not upgraded")
\t}
\tstate := loadTestState(t, codexDir)
\tif state.Config[managedConfigKey].Value != "200" {
\t\tt.Fatalf("legacy managed config ownership was not migrated: %+v", state.Config)
\t}

\twriteTestFile(t, managedPath, []byte(managedConfigKey+" = 300\\n"))
\trunInstall(t, repo, codexDir)
\tif !bytes.Contains(readTestFile(t, configPath), []byte(managedConfigKey+" = 300")) {
\t\tt.Fatal("migrated managed config was not updated")
\t}
}

func TestInstallRejectsUnsafeLegacyManifestPath(t *testing.T) {
\trepo := initInstallFixtureRepo(t)
\tcodexDir := t.TempDir()
\twriteTestFile(t, filepath.Join(codexDir, legacyManifestName), []byte("instructions/../../outside\\n"))
\tvar stdout bytes.Buffer
\tif err := Install(repo, codexDir, &stdout); err == nil {
\t\tt.Fatal("expected unsafe legacy manifest path rejection")
\t}
}

'''
replace_once('glm-worker/internal/codexinstall/install_test.go', insert_test_anchor, new_tests + insert_test_anchor)

smoke = Path('tests/install_smoke.sh')
text = smoke.read_text()
text = text.replace(
    '''printf '%s\\n' 'local_key = "keep"' >"$home/.codex/config.toml"
''',
    '''printf '%s\\n' 'background_terminal_max_timeout = 21600000' 'local_key = "keep"' >"$home/.codex/config.toml"
''',
    1,
)
old_smoke = '''cp "$repo/codex/AGENTS.md" "$home/.codex/AGENTS.md"
printf '%s\\n' 'AGENTS.md' >>"$home/.codex/.codex-config-managed-files"
run_install
test ! -e "$home/.codex/AGENTS.md"
if grep -Fxq 'AGENTS.md' "$home/.codex/.codex-config-managed-files"; then
\tprintf '%s\\n' 'legacy global AGENTS.md remains installer-managed' >&2
\texit 1
fi
'''
new_smoke = '''test ! -e "$home/.codex/.codex-config-managed-files"
test -f "$home/.codex/codex-worker-orchestrator/install-state.json"
'''
if old_smoke not in text:
    raise SystemExit('smoke legacy block missing')
smoke.write_text(text.replace(old_smoke, new_smoke, 1))
