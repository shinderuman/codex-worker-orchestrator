package codexinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

type desiredFile struct {
	Path       string
	SourcePath string
	Content    []byte
	Mode       fs.FileMode
	SHA256     string
}

type fileInstallPlan struct {
	Desired   []desiredFile
	Remove    []string
	Preserved []string
}

func collectDesiredFiles(repoRoot string) ([]desiredFile, error) {
	files := []desiredFile{}
	if err := addDesiredFile(repoRoot, "codex/AGENTS.md", "instructions/codex-worker-orchestrator.md", &files); err != nil {
		return nil, err
	}
	if err := walkDesiredTree(repoRoot, "codex/instructions", "instructions", &files); err != nil {
		return nil, err
	}
	if err := addDesiredFile(repoRoot, "codex/rules/glm-worker.rules", "rules/glm-worker.rules", &files); err != nil {
		return nil, err
	}
	if err := walkDesiredTree(repoRoot, "codex/glm-worker/prompts", "glm-worker/prompts", &files); err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func walkDesiredTree(repoRoot, sourceRoot, destinationRoot string, files *[]desiredFile) error {
	root := filepath.Join(repoRoot, filepath.FromSlash(sourceRoot))
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("managed Codex source must not be a symlink: %s", path)
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		source := filepath.ToSlash(filepath.Join(sourceRoot, relative))
		destination := filepath.ToSlash(filepath.Join(destinationRoot, relative))
		return addDesiredFile(repoRoot, source, destination, files)
	})
}

func addDesiredFile(repoRoot, sourcePath, destinationPath string, files *[]desiredFile) error {
	path := filepath.Join(repoRoot, filepath.FromSlash(sourcePath))
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat managed Codex source %s: %w", sourcePath, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("managed Codex source is not a regular file: %s", sourcePath)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read managed Codex source %s: %w", sourcePath, err)
	}
	*files = append(*files, desiredFile{
		Path: destinationPath, SourcePath: sourcePath, Content: content,
		Mode: info.Mode().Perm(), SHA256: digestBytes(content),
	})
	return nil
}

func buildFileInstallPlan(repoRoot, codexDir string, state installState, stateExists bool) (fileInstallPlan, error) {
	desired, err := collectDesiredFiles(repoRoot)
	if err != nil {
		return fileInstallPlan{}, err
	}
	records := stateFileMap(state)
	if err := validateFilePlanAncestors(codexDir, desired, state); err != nil {
		return fileInstallPlan{}, err
	}
	desiredByPath := make(map[string]desiredFile, len(desired))
	for _, file := range desired {
		desiredByPath[file.Path] = file
		if err := requireCurrentPathOwnership(codexDir, file, records, stateExists); err != nil {
			return fileInstallPlan{}, err
		}
	}
	plan := fileInstallPlan{Desired: desired}
	if stateExists {
		planObsoleteStateFiles(codexDir, records, desiredByPath, &plan)
	}
	return plan, nil
}

func validateFilePlanAncestors(codexDir string, desired []desiredFile, state installState) error {
	paths := map[string]bool{}
	for _, file := range desired {
		paths[file.Path] = true
	}
	for _, record := range state.Files {
		paths[record.Path] = true
	}
	for path := range paths {
		if err := validateManagedPathAncestors(codexDir, path); err != nil {
			return err
		}
	}
	return nil
}

func requireCurrentPathOwnership(codexDir string, file desiredFile, records map[string]managedFileRecord, stateExists bool) error {
	target := filepath.Join(codexDir, filepath.FromSlash(file.Path))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return requireMissingPathOwnership(file.Path, records, stateExists)
	}
	if err != nil {
		return fmt.Errorf("stat installed Codex file %s: %w", file.Path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular Codex path %s", file.Path)
	}
	return requireExistingPathOwnership(target, file.Path, records)
}

func requireMissingPathOwnership(path string, records map[string]managedFileRecord, stateExists bool) error {
	if stateExists {
		if _, owned := records[path]; owned {
			return fmt.Errorf("managed Codex file is missing; refusing silent recreation: %s", path)
		}
	}
	return nil
}

func requireExistingPathOwnership(target, path string, records map[string]managedFileRecord) error {
	if record, owned := records[path]; owned {
		actual, err := digestFile(target)
		if err != nil {
			return err
		}
		if actual != record.SHA256 {
			return fmt.Errorf("managed Codex file was modified after install; refusing to overwrite: %s", path)
		}
		return nil
	}
	return fmt.Errorf("refusing to overwrite preexisting Codex file without tool ownership: %s", path)
}

func planObsoleteStateFiles(codexDir string, records map[string]managedFileRecord, desired map[string]desiredFile, plan *fileInstallPlan) {
	for path, record := range records {
		if _, current := desired[path]; current {
			continue
		}
		target := filepath.Join(codexDir, filepath.FromSlash(path))
		actual, err := digestRegularFile(target)
		switch {
		case errors.Is(err, os.ErrNotExist):
			continue
		case err != nil:
			plan.Preserved = append(plan.Preserved, path)
		case actual == record.SHA256:
			plan.Remove = append(plan.Remove, path)
		default:
			plan.Preserved = append(plan.Preserved, path)
		}
	}
	sort.Strings(plan.Remove)
	sort.Strings(plan.Preserved)
}

func applyFileInstallPlan(codexDir string, plan fileInstallPlan, state installState, stateExists bool, recordMutation func(string) error, output func(string, ...any)) ([]managedFileRecord, error) {
	for _, path := range plan.Preserved {
		output("preserved user-modified obsolete Codex file: %s\n", filepath.Join(codexDir, filepath.FromSlash(path)))
	}
	records := stateFileMap(state)
	if err := applyObsoleteFileRemovals(codexDir, plan.Remove, records, recordMutation, output); err != nil {
		return nil, err
	}
	return applyDesiredFiles(codexDir, plan.Desired, records, stateExists, recordMutation, output)
}

func applyObsoleteFileRemovals(codexDir string, paths []string, records map[string]managedFileRecord, recordMutation func(string) error, output func(string, ...any)) error {
	for _, path := range paths {
		target := filepath.Join(codexDir, filepath.FromSlash(path))
		record, owned := records[path]
		if !owned {
			return fmt.Errorf("obsolete Codex file lost ownership state after preparation: %s", path)
		}
		actual, err := digestRegularFile(target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("inspect obsolete managed Codex file %s before removal: %w", path, err)
		}
		if actual != record.SHA256 {
			return fmt.Errorf("obsolete managed Codex file changed after preparation; refusing to remove: %s", path)
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove obsolete managed Codex file %s: %w", path, err)
		}
		if err := recordMutation(target); err != nil {
			return err
		}
		output("removed managed Codex file: %s\n", target)
	}
	return nil
}

func applyDesiredFiles(codexDir string, desired []desiredFile, records map[string]managedFileRecord, stateExists bool, recordMutation func(string) error, output func(string, ...any)) ([]managedFileRecord, error) {
	result := make([]managedFileRecord, 0, len(desired))
	for _, file := range desired {
		if err := requireCurrentPathOwnership(codexDir, file, records, stateExists); err != nil {
			return nil, fmt.Errorf("managed Codex file changed after preparation: %w", err)
		}
		target := filepath.Join(codexDir, filepath.FromSlash(file.Path))
		current, err := os.ReadFile(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read installed Codex file %s: %w", file.Path, err)
		}
		if errors.Is(err, os.ErrNotExist) || !bytes.Equal(current, file.Content) {
			if err := writeAtomic(target, file.Content, file.Mode); err != nil {
				return nil, fmt.Errorf("install managed Codex file %s: %w", file.Path, err)
			}
			if err := recordMutation(target); err != nil {
				return nil, err
			}
			output("updated: %s\n", target)
		}
		result = append(result, managedFileRecord{Path: file.Path, SHA256: file.SHA256})
	}
	return result, nil
}

func validateManagedFilesForStateCommit(codexDir string, records []managedFileRecord) error {
	for _, record := range records {
		target := filepath.Join(codexDir, filepath.FromSlash(record.Path))
		actual, err := digestRegularFile(target)
		if err != nil {
			return fmt.Errorf("managed Codex file changed before state commit %s: %w", record.Path, err)
		}
		if actual != record.SHA256 {
			return fmt.Errorf("managed Codex file changed before state commit: %s", record.Path)
		}
	}
	return nil
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func digestFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read installed Codex file %s: %w", path, err)
	}
	return digestBytes(data), nil
}

func digestRegularFile(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file")
	}
	return digestFile(path)
}
