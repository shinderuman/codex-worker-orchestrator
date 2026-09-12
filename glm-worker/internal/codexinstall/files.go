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
		Path:       destinationPath,
		SourcePath: sourcePath,
		Content:    content,
		Mode:       info.Mode().Perm(),
		SHA256:     digestBytes(content),
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

func applyFileInstallPlan(codexDir string, plan fileInstallPlan, output func(string, ...any)) ([]managedFileRecord, error) {
	for _, path := range plan.Preserved {
		output("preserved user-modified obsolete Codex file: %s\n", filepath.Join(codexDir, filepath.FromSlash(path)))
	}
	for _, path := range plan.Remove {
		target := filepath.Join(codexDir, filepath.FromSlash(path))
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove obsolete managed Codex file %s: %w", path, err)
		}
		output("removed managed Codex file: %s\n", target)
	}
	records := make([]managedFileRecord, 0, len(plan.Desired))
	for _, file := range plan.Desired {
		target := filepath.Join(codexDir, filepath.FromSlash(file.Path))
		current, err := os.ReadFile(target)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read installed Codex file %s: %w", file.Path, err)
		}
		if errors.Is(err, os.ErrNotExist) || !bytes.Equal(current, file.Content) {
			if err := writeAtomic(target, file.Content, file.Mode); err != nil {
				return nil, fmt.Errorf("install managed Codex file %s: %w", file.Path, err)
			}
			output("updated: %s\n", target)
		}
		records = append(records, managedFileRecord{Path: file.Path, SHA256: file.SHA256})
	}
	return records, nil
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
