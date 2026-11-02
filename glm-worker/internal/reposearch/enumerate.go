package reposearch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type trackedEntry struct {
	mode string
	sha  string
	path string
}

const (
	enumerationVersion    = 1
	trackedModeRegular    = "100644"
	trackedModeExecutable = "100755"
)

var ErrUnsupportedIndexFlags = errors.New("repo-search does not support assume-unchanged or skip-worktree index entries")

var defaultExcludeDirs = []string{".git", ".hg", ".svn", "node_modules", "vendor", "dist", "build", "target", "__pycache__"}

func resolveExcludeDirs(extra []string) (map[string]bool, error) {
	dirs := make(map[string]bool, len(defaultExcludeDirs)+len(extra))
	for _, name := range defaultExcludeDirs {
		dirs[name] = true
	}
	for _, name := range extra {
		if name == "" || name == "." || name == ".." || strings.ContainsRune(name, '/') || filepath.IsAbs(name) {
			return nil, fmt.Errorf("%w: ExcludeDirsの不正なdirectory名 %q", ErrInvalidOptions, name)
		}
		dirs[name] = true
	}
	return dirs, nil
}

func excludedPath(rel string, excludeDirs map[string]bool) bool {
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for _, segment := range segments[:len(segments)-1] {
		if excludeDirs[segment] {
			return true
		}
	}
	return false
}

func enumerateFiles(ctx context.Context, repoRoot string, excludeDirs map[string]bool) ([]string, error) {
	entries, err := trackedFileEntries(ctx, repoRoot, excludeDirs)
	if err != nil {
		return nil, err
	}
	untracked, err := untrackedFilePaths(ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(entries)+len(untracked))
	paths := make([]string, 0, len(entries)+len(untracked))
	for _, entry := range entries {
		if entry.path == "" || seen[entry.path] || excludedPath(entry.path, excludeDirs) {
			continue
		}
		seen[entry.path] = true
		paths = append(paths, entry.path)
	}
	for _, path := range untracked {
		if path == "" || seen[path] || excludedPath(path, excludeDirs) {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func trackedFileEntries(ctx context.Context, repoRoot string, excludeDirs map[string]bool) ([]trackedEntry, error) {
	output, err := gitOutput(ctx, repoRoot, "ls-files", "-z", "-v", "-s", "--cached")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --cached: %w", err)
	}
	seen := map[string]bool{}
	var entries []trackedEntry
	for _, raw := range strings.Split(string(output), "\x00") {
		if raw == "" {
			continue
		}
		entry, include, err := parseTrackedIndexEntry(raw, excludeDirs)
		if err != nil {
			return nil, err
		}
		if !include || seen[entry.path] {
			continue
		}
		seen[entry.path] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

func parseTrackedIndexEntry(raw string, excludeDirs map[string]bool) (trackedEntry, bool, error) {
	if len(raw) < 3 || raw[1] != ' ' {
		return trackedEntry{}, false, fmt.Errorf("git ls-files -v --cachedのentryを解析できません: %q", raw)
	}
	mode, sha, path, ok := parseLsFilesStage(raw[2:])
	if !ok {
		return trackedEntry{}, false, fmt.Errorf("git ls-files --cachedのentryを解析できません: %q", raw)
	}
	if (mode != trackedModeRegular && mode != trackedModeExecutable) || excludedPath(path, excludeDirs) {
		return trackedEntry{}, false, nil
	}
	if raw[0] != 'H' {
		return trackedEntry{}, false, fmt.Errorf("%w: %s (index flag %c)", ErrUnsupportedIndexFlags, path, raw[0])
	}
	return trackedEntry{mode: mode, sha: sha, path: path}, true, nil
}

func untrackedFilePaths(ctx context.Context, repoRoot string) ([]string, error) {
	output, err := gitOutput(ctx, repoRoot, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others: %w", err)
	}
	var paths []string
	for _, path := range strings.Split(string(output), "\x00") {
		if path == "" || strings.HasSuffix(path, "/") {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func parseLsFilesStage(entry string) (string, string, string, bool) {
	tab := strings.IndexByte(entry, '\t')
	if tab < 0 {
		return "", "", "", false
	}
	header := strings.Fields(entry[:tab])
	if len(header) != 3 {
		return "", "", "", false
	}
	return header[0], header[1], entry[tab+1:], true
}

func joinWithinRoot(root, rel string) (string, error) {
	abs := filepath.Join(root, rel)
	relToRoot, err := filepath.Rel(root, abs)
	if err != nil {
		return "", err
	}
	if relToRoot == "." || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("pathがrepository境界を越えています: %s", rel)
	}
	return abs, nil
}
