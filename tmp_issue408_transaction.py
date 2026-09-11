from pathlib import Path


def replace_once(path, old, new):
    p = Path(path)
    text = p.read_text()
    if old not in text:
        raise SystemExit(f'anchor missing in {path}: {old[:100]!r}')
    p.write_text(text.replace(old, new, 1))

# Register the new thin CLI as the process-stream owner, matching existing command entrypoints.
replace_once(
    'glm-worker/internal/harnesslint/process_stream.go',
    '\t"glm-worker/cmd/glm-codex-context/main.go":  {"Stdout": 1, "Stderr": 1},\n',
    '\t"glm-worker/cmd/glm-codex-context/main.go":  {"Stdout": 1, "Stderr": 1},\n\t"glm-worker/cmd/codex-install/main.go":        {"Stdout": 1, "Stderr": 1},\n',
)

# Split state decode/validation and reject symlinked state surfaces.
state_path = Path('glm-worker/internal/codexinstall/state.go')
state = state_path.read_text()
start = state.index('func loadState(codexDir string) (installState, bool, error) {')
end = state.index('\nfunc loadLegacyManifest', start)
new_load = r'''func loadState(codexDir string) (installState, bool, error) {
	if err := validateManagedPathAncestors(codexDir, stateRelativePath); err != nil {
		return installState{}, false, err
	}
	path := statePath(codexDir)
	data, exists, err := readOptionalRegularStateFile(path)
	if err != nil {
		return installState{}, false, err
	}
	if !exists {
		return installState{Version: stateVersion, Config: map[string]managedConfigRecord{}}, false, nil
	}
	state, err := decodeInstallState(data)
	if err != nil {
		return installState{}, false, err
	}
	return state, true, nil
}

func readOptionalRegularStateFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("stat Codex install state: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("Codex install state is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read Codex install state: %w", err)
	}
	return data, true, nil
}

func decodeInstallState(data []byte) (installState, error) {
	var state installState
	if err := json.Unmarshal(data, &state); err != nil {
		return installState{}, fmt.Errorf("decode Codex install state: %w", err)
	}
	if err := validateInstallState(&state); err != nil {
		return installState{}, err
	}
	return state, nil
}

func validateInstallState(state *installState) error {
	if state.Version != stateVersion {
		return fmt.Errorf("unsupported Codex install state version %d", state.Version)
	}
	if state.Config == nil {
		state.Config = map[string]managedConfigRecord{}
	}
	if err := validateInstallStateFiles(state.Files); err != nil {
		return err
	}
	return validateInstallStateConfig(state.Config)
}

func validateInstallStateFiles(records []managedFileRecord) error {
	seen := map[string]bool{}
	for _, record := range records {
		if err := validateManagedRelativePath(record.Path); err != nil || record.SHA256 == "" || seen[record.Path] {
			return fmt.Errorf("invalid Codex install state file record %q", record.Path)
		}
		seen[record.Path] = true
	}
	return nil
}

func validateInstallStateConfig(records map[string]managedConfigRecord) error {
	for key, record := range records {
		if key != managedConfigKey || record.Value == "" || record.LineSHA256 == "" {
			return fmt.Errorf("invalid Codex install state config record %q", key)
		}
	}
	return nil
}
'''
state = state[:start] + new_load + state[end:]
state += r'''

func validateManagedPathAncestors(codexDir, relativePath string) error {
	if err := validateManagedRelativePath(relativePath); err != nil {
		return err
	}
	parts := strings.Split(filepath.FromSlash(relativePath), string(filepath.Separator))
	current := codexDir
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("stat managed Codex path ancestor %s: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("managed Codex path ancestor is not a real directory: %s", current)
		}
	}
	return nil
}
'''
state_path.write_text(state)

# Validate every destination ancestry before any plan can mutate it.
replace_once(
    'glm-worker/internal/codexinstall/files.go',
    '''\tdesiredByPath := make(map[string]desiredFile, len(desired))
\tfor _, file := range desired {
''',
    '''\tif err := validateFilePlanAncestors(codexDir, desired, state, legacy); err != nil {
\t\treturn fileInstallPlan{}, err
\t}
\tdesiredByPath := make(map[string]desiredFile, len(desired))
\tfor _, file := range desired {
''',
)
insert = '''func requireCurrentPathOwnership(repoRoot, codexDir string, file desiredFile, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
'''
helper = r'''func validateFilePlanAncestors(codexDir string, desired []desiredFile, state installState, legacy legacyManifest) error {
	paths := map[string]bool{}
	for _, file := range desired {
		paths[file.Path] = true
	}
	for _, record := range state.Files {
		paths[record.Path] = true
	}
	for path := range legacy.Paths {
		paths[path] = true
	}
	for path := range paths {
		if err := validateManagedPathAncestors(codexDir, path); err != nil {
			return err
		}
	}
	return nil
}

'''
replace_once('glm-worker/internal/codexinstall/files.go', insert, helper + insert)

# Move mutation orchestration into a rollback-aware transaction helper.
install_path = Path('glm-worker/internal/codexinstall/install.go')
install = install_path.read_text()
start = install.index('func applyInstall(preparation installPreparation, stdout io.Writer) error {')
end = install.index('\nfunc writeAtomic', start)
install = install[:start] + '''func applyInstall(preparation installPreparation, stdout io.Writer) error {
\treturn applyInstallWithStateWriter(preparation, stdout, writeState)
}
''' + install[end:]
install_path.write_text(install)

Path('glm-worker/internal/codexinstall/transaction.go').write_text(r'''package codexinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type installBackup struct {
	Path    string
	Exists  bool
	Content []byte
	Mode    os.FileMode
}

type installStateWriter func(string, installState) error

func applyInstallWithStateWriter(preparation installPreparation, stdout io.Writer, writeStateFn installStateWriter) error {
	backups, err := captureInstallBackups(preparation)
	if err != nil {
		return err
	}
	output := func(format string, args ...any) {
		_, _ = fmt.Fprintf(stdout, format, args...)
	}
	files, err := applyFileInstallPlan(preparation.codexDir, preparation.filePlan, output)
	if err != nil {
		return rollbackInstall(backups, err)
	}
	if err := applyConfigInstallPlan(preparation.configPlan, output); err != nil {
		return rollbackInstall(backups, err)
	}
	next := installState{Version: stateVersion, Files: files, Config: map[string]managedConfigRecord{}}
	if preparation.configPlan.Record != nil {
		next.Config[managedConfigKey] = *preparation.configPlan.Record
	}
	if err := writeStateFn(preparation.codexDir, next); err != nil {
		return rollbackInstall(backups, err)
	}
	if preparation.stateExists {
		return nil
	}
	return removeLegacyManifest(preparation.codexDir, preparation.legacy.Present)
}

func captureInstallBackups(preparation installPreparation) ([]installBackup, error) {
	paths := installMutationPaths(preparation)
	backups := make([]installBackup, 0, len(paths))
	for _, path := range paths {
		backup, err := captureInstallBackup(path)
		if err != nil {
			return nil, err
		}
		backups = append(backups, backup)
	}
	return backups, nil
}

func installMutationPaths(preparation installPreparation) []string {
	unique := map[string]bool{statePath(preparation.codexDir): true}
	for _, file := range preparation.filePlan.Desired {
		unique[filepath.Join(preparation.codexDir, filepath.FromSlash(file.Path))] = true
	}
	for _, path := range preparation.filePlan.Remove {
		unique[filepath.Join(preparation.codexDir, filepath.FromSlash(path))] = true
	}
	if preparation.configPlan.Changed {
		unique[preparation.configPlan.Path] = true
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func captureInstallBackup(path string) (installBackup, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return installBackup{Path: path}, nil
	}
	if err != nil {
		return installBackup{}, fmt.Errorf("stat install rollback surface %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return installBackup{}, fmt.Errorf("install rollback surface is not a regular file: %s", path)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return installBackup{}, fmt.Errorf("read install rollback surface %s: %w", path, err)
	}
	return installBackup{Path: path, Exists: true, Content: content, Mode: info.Mode().Perm()}, nil
}

func rollbackInstall(backups []installBackup, cause error) error {
	errs := []error{cause}
	for index := len(backups) - 1; index >= 0; index-- {
		if err := restoreInstallBackup(backups[index]); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func restoreInstallBackup(backup installBackup) error {
	if backup.Exists {
		if err := writeAtomic(backup.Path, backup.Content, backup.Mode); err != nil {
			return fmt.Errorf("restore install rollback surface %s: %w", backup.Path, err)
		}
		return nil
	}
	if err := os.Remove(backup.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove newly-created install surface %s during rollback: %w", backup.Path, err)
	}
	return nil
}
''')

# Add focused regression tests for symlink containment and rollback.
test_path = Path('glm-worker/internal/codexinstall/install_test.go')
test = test_path.read_text()
anchor = 'func runInstall(t *testing.T, repo, codexDir string) {\n'
new_tests = r'''func TestInstallRejectsSymlinkedManagedAncestor(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(codexDir, "instructions")); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := Install(repo, codexDir, &stdout); err == nil {
		t.Fatal("expected symlinked managed ancestor rejection")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("managed install escaped Codex directory: entries=%v err=%v", entries, err)
	}
}

func TestInstallRollsBackWhenStateCommitFails(t *testing.T) {
	repo := initInstallFixtureRepo(t)
	codexDir := t.TempDir()
	runInstall(t, repo, codexDir)
	target := filepath.Join(codexDir, "instructions", "test.md")
	configPath := filepath.Join(codexDir, "config.toml")
	stateFile := statePath(codexDir)
	oldTarget := append([]byte(nil), readTestFile(t, target)...)
	oldConfig := append([]byte(nil), readTestFile(t, configPath)...)
	oldState := append([]byte(nil), readTestFile(t, stateFile)...)

	writeTestFile(t, filepath.Join(repo, "codex", "instructions", "test.md"), []byte("# tool instruction changed\n"))
	writeTestFile(t, filepath.Join(repo, "codex", "config-managed.toml"), []byte(managedConfigKey+" = 21600001\n"))
	preparation, err := prepareInstall(repo, codexDir)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	stateFailure := errors.New("state commit failed")
	err = applyInstallWithStateWriter(preparation, &stdout, func(string, installState) error { return stateFailure })
	if !errors.Is(err, stateFailure) {
		t.Fatalf("expected state failure, got %v", err)
	}
	assertFileBytes(t, target, oldTarget)
	assertFileBytes(t, configPath, oldConfig)
	assertFileBytes(t, stateFile, oldState)
}

'''
if anchor not in test:
    raise SystemExit('test insertion anchor missing')
test = test.replace(anchor, new_tests + anchor, 1)
# errors is required for rollback test.
test = test.replace('"encoding/json"\n', '"encoding/json"\n\t"errors"\n', 1)
test_path.write_text(test)
