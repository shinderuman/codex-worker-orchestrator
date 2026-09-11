package codexinstall

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
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

func buildFileInstallPlan(repoRoot, codexDir string, state installState, stateExists bool, legacy legacyManifest) (fileInstallPlan, error) {
	desired, err := collectDesiredFiles(repoRoot)
	if err != nil {
		return fileInstallPlan{}, err
	}
	records := stateFileMap(state)
	desiredByPath := make(map[string]desiredFile, len(desired))
	for _, file := range desired {
		desiredByPath[file.Path] = file
		if err := requireCurrentPathOwnership(repoRoot, codexDir, file, records, stateExists, legacy); err != nil {
			return fileInstallPlan{}, err
		}
	}
	plan := fileInstallPlan{Desired: desired}
	if stateExists {
		planObsoleteStateFiles(codexDir, records, desiredByPath, &plan)
		return plan, nil
	}
	if err := planLegacyFiles(repoRoot, codexDir, legacy, desiredByPath, &plan); err != nil {
		return fileInstallPlan{}, err
	}
	return plan, nil
}

func requireCurrentPathOwnership(repoRoot, codexDir string, file desiredFile, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
	target := filepath.Join(codexDir, filepath.FromSlash(file.Path))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return requireMissingPathOwnership(file.Path, records, stateExists, legacy)
	}
	if err != nil {
		return fmt.Errorf("stat installed Codex file %s: %w", file.Path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular Codex path %s", file.Path)
	}
	return requireExistingPathOwnership(repoRoot, target, file.Path, records, stateExists, legacy)
}

func requireMissingPathOwnership(path string, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
	if stateExists {
		if _, owned := records[path]; owned {
			return fmt.Errorf("managed Codex file is missing; refusing silent recreation: %s", path)
		}
		return nil
	}
	if legacy.Paths[path] {
		return fmt.Errorf("legacy managed Codex file is missing; refusing silent recreation: %s", path)
	}
	return nil
}

func requireExistingPathOwnership(repoRoot, target, path string, records map[string]managedFileRecord, stateExists bool, legacy legacyManifest) error {
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
	if !stateExists && legacy.Paths[path] {
		matches, err := managedPathMatchesRepositoryHistory(repoRoot, path, target)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
		return fmt.Errorf("legacy managed Codex file no longer matches repository history; refusing to overwrite: %s", path)
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

func planLegacyFiles(repoRoot, codexDir string, legacy legacyManifest, desired map[string]desiredFile, plan *fileInstallPlan) error {
	if !legacy.Present {
		return nil
	}
	paths := make([]string, 0, len(legacy.Paths))
	for path := range legacy.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if _, current := desired[path]; current {
			continue
		}
		target := filepath.Join(codexDir, filepath.FromSlash(path))
		matches, err := managedPathMatchesRepositoryHistory(repoRoot, path, target)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if matches {
			plan.Remove = append(plan.Remove, path)
		} else {
			plan.Preserved = append(plan.Preserved, path)
		}
	}
	sort.Strings(plan.Remove)
	sort.Strings(plan.Preserved)
	return nil
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

func supportedLegacyManagedPath(path string) bool {
	if path == "AGENTS.md" || path == "instructions/codex-worker-orchestrator.md" || path == "rules/glm-worker.rules" {
		return true
	}
	return strings.HasPrefix(path, "instructions/") || strings.HasPrefix(path, "glm-worker/prompts/")
}

func managedPathMatchesRepositoryHistory(repoRoot, installedPath, target string) (bool, error) {
	info, err := os.Lstat(target)
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	sourcePath, ok := legacySourcePath(installedPath)
	if !ok {
		return false, nil
	}
	current := filepath.Join(repoRoot, filepath.FromSlash(sourcePath))
	if source, readErr := os.ReadFile(current); readErr == nil {
		targetData, targetErr := os.ReadFile(target)
		if targetErr != nil {
			return false, targetErr
		}
		if bytes.Equal(source, targetData) {
			return true, nil
		}
	}
	command := exec.Command("git", "-C", repoRoot, "log", "--format=%H", "--", sourcePath)
	output, err := command.Output()
	if err != nil {
		return false, fmt.Errorf("enumerate managed Codex source history %s: %w", sourcePath, err)
	}
	targetData, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	for _, revision := range strings.Fields(string(output)) {
		show := exec.Command("git", "-C", repoRoot, "show", revision+":"+sourcePath)
		data, showErr := show.Output()
		if showErr == nil && bytes.Equal(data, targetData) {
			return true, nil
		}
	}
	return false, nil
}

func legacySourcePath(installedPath string) (string, bool) {
	switch {
	case installedPath == "AGENTS.md", installedPath == "instructions/codex-worker-orchestrator.md":
		return "codex/AGENTS.md", true
	case installedPath == "rules/glm-worker.rules":
		return "codex/rules/glm-worker.rules", true
	case strings.HasPrefix(installedPath, "instructions/"):
		return "codex/" + installedPath, true
	case strings.HasPrefix(installedPath, "glm-worker/prompts/"):
		return "codex/" + installedPath, true
	default:
		return "", false
	}
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
