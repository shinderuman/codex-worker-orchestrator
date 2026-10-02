package controller

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ExecutionTrees struct {
	IndexTree             string `json:"index_tree"`
	WorktreeTree          string `json:"worktree_tree"`
	ParentAuthorityDigest string `json:"parent_authority_digest"`
}

type workspaceTreeEntry struct {
	mode string
	oid  string
	path string
}

func CaptureExecutionTrees(repo, authorityBase string) (ExecutionTrees, error) {
	entries, err := suspensionIndexEntries(repo)
	if err != nil {
		return ExecutionTrees{}, err
	}
	index, err := newSuspensionIndex(repo, "")
	if err != nil {
		return ExecutionTrees{}, err
	}
	defer func() { _ = os.Remove(index) }()
	if err := writeSuspensionEntries(repo, index, entries); err != nil {
		return ExecutionTrees{}, err
	}
	parentDigest, err := normalizeParentPaths(repo, index, authorityBase)
	if err != nil {
		return ExecutionTrees{}, err
	}
	indexTree, err := suspensionGitText(repo, index, nil, "write-tree")
	if err != nil {
		return ExecutionTrees{}, err
	}
	worktree, err := captureSuspensionWorktree(repo, entries)
	if err != nil {
		return ExecutionTrees{}, err
	}
	if _, err := suspensionGit(repo, index, nil, "read-tree", "--empty"); err != nil {
		return ExecutionTrees{}, err
	}
	if err := writeSuspensionEntries(repo, index, worktree); err != nil {
		return ExecutionTrees{}, err
	}
	if _, err := normalizeParentPaths(repo, index, authorityBase); err != nil {
		return ExecutionTrees{}, err
	}
	worktreeTree, err := suspensionGitText(repo, index, nil, "write-tree")
	return ExecutionTrees{IndexTree: indexTree, WorktreeTree: worktreeTree, ParentAuthorityDigest: parentDigest}, err
}

func suspensionIndexEntries(repo string) ([]workspaceTreeEntry, error) {
	debug, err := runGitBinary(repo, nil, "ls-files", "--debug", "-z")
	if err != nil {
		return nil, err
	}
	if err := validateSuspensionIndexFlags(debug); err != nil {
		return nil, err
	}
	data, err := runGitBinary(repo, nil, "ls-files", "--stage", "-z")
	if err != nil {
		return nil, err
	}
	return parseWorkspaceEntries(data, true)
}

func validateSuspensionIndexFlags(data []byte) error {
	for len(data) != 0 {
		at := bytes.IndexByte(data, 0)
		if at < 0 {
			return fmt.Errorf("index debug path is malformed")
		}
		data = data[at+1:]
		var line []byte
		for count := 0; count < 5; count++ {
			at = bytes.IndexByte(data, '\n')
			if at < 0 {
				return fmt.Errorf("index debug flags are incomplete")
			}
			line = data[:at]
			data = data[at+1:]
		}
		at = strings.LastIndex(string(line), "flags: ")
		if at < 0 || strings.TrimSpace(string(line[at+7:])) != "0" {
			return fmt.Errorf("unsupported suspension index flags")
		}
	}
	return nil
}

func parseWorkspaceEntries(data []byte, staged bool) ([]workspaceTreeEntry, error) {
	var entries []workspaceTreeEntry
	for _, record := range bytes.Split(data, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		entry, err := parseWorkspaceEntry(record, staged)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseWorkspaceEntry(record []byte, staged bool) (workspaceTreeEntry, error) {
	parts := bytes.SplitN(record, []byte{'\t'}, 2)
	if len(parts) != 2 {
		return workspaceTreeEntry{}, fmt.Errorf("malformed Git tree entry")
	}
	fields := strings.Fields(string(parts[0]))
	if len(fields) != 3 {
		return workspaceTreeEntry{}, fmt.Errorf("malformed Git entry header")
	}
	if staged && fields[2] != "0" {
		return workspaceTreeEntry{}, fmt.Errorf("unresolved index stage")
	}
	path := string(parts[1])
	if err := validateSuspensionPath(path); err != nil {
		return workspaceTreeEntry{}, err
	}
	mode := fields[0]
	if mode != "100644" && mode != "100755" && mode != "120000" && mode != "160000" {
		return workspaceTreeEntry{}, fmt.Errorf("unsupported Git mode %s for %q", mode, path)
	}
	oid := fields[1]
	if !staged {
		oid = fields[2]
	}
	return workspaceTreeEntry{mode: mode, oid: oid, path: path}, nil
}

func validateSuspensionPath(path string) error {
	if path == "" || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") {
		return fmt.Errorf("unsafe suspension path %q", path)
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." || strings.EqualFold(component, ".git") {
			return fmt.Errorf("unsafe suspension path %q", path)
		}
	}
	return nil
}

func parentManagedPath(path string) bool {
	return path == "IMPLEMENTATION_RULES.md" || path == implementationPlanPath || path == "IMPLEMENTATION_HISTORY.md" || path == "IMPLEMENTATION_TASKS" || strings.HasPrefix(path, "IMPLEMENTATION_TASKS/")
}

func newSuspensionIndex(repo, tree string) (string, error) {
	file, err := os.CreateTemp("", "controller-index-")
	if err != nil {
		return "", err
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		return "", err
	}
	if err := os.Remove(path); err != nil {
		return "", err
	}
	args := []string{"read-tree", tree}
	if tree == "" {
		args = []string{"read-tree", "--empty"}
	}
	if _, err := suspensionGit(repo, path, nil, args...); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func writeSuspensionEntries(repo, index string, entries []workspaceTreeEntry) error {
	var input bytes.Buffer
	for _, entry := range entries {
		fmt.Fprintf(&input, "%s %s\t%s%c", entry.mode, entry.oid, entry.path, byte(0))
	}
	_, err := suspensionGit(repo, index, input.Bytes(), "update-index", "-z", "--index-info")
	return err
}

func normalizeParentPaths(repo, index, base string) (string, error) {
	current, err := suspensionGit(repo, index, nil, "ls-files", "-z")
	if err != nil {
		return "", err
	}
	var remove bytes.Buffer
	for _, path := range bytes.Split(current, []byte{0}) {
		if parentManagedPath(string(path)) {
			remove.Write(path)
			remove.WriteByte(0)
		}
	}
	if _, err := suspensionGit(repo, index, remove.Bytes(), "update-index", "--force-remove", "-z", "--stdin"); err != nil {
		return "", err
	}
	data, err := runGitBinary(repo, nil, "ls-tree", "-r", "-z", base, "--", "IMPLEMENTATION_RULES.md", implementationPlanPath, "IMPLEMENTATION_HISTORY.md", "IMPLEMENTATION_TASKS")
	if err != nil {
		return "", err
	}
	entries, err := parseWorkspaceEntries(data, false)
	if err != nil {
		return "", err
	}
	if err := writeSuspensionEntries(repo, index, entries); err != nil {
		return "", err
	}
	return digestBytes(data), nil
}

func suspensionGit(repo, index string, input []byte, args ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", repo}, args...)...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_INDEX_FILE=") {
			command.Env = append(command.Env, value)
		}
	}
	command.Env = append(command.Env, "GIT_INDEX_FILE="+index)
	command.Stdin = bytes.NewReader(input)
	var diagnostic bytes.Buffer
	command.Stderr = &diagnostic
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %v: %w: %s", args, err, diagnostic.String())
	}
	return data, nil
}

func suspensionGitText(repo, index string, input []byte, args ...string) (string, error) {
	data, err := suspensionGit(repo, index, input, args...)
	return strings.TrimSpace(string(data)), err
}

func suspensionWorktreePath(repo, path string) (string, error) {
	if err := validateSuspensionPath(path); err != nil {
		return "", err
	}
	parts := strings.Split(path, "/")
	current := repo
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return filepath.Join(repo, filepath.FromSlash(path)), nil
		}
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("ambiguous suspension path ancestor %q", current)
		}
	}
	return filepath.Join(repo, filepath.FromSlash(path)), nil
}
