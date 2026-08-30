package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type acceptedFixScope struct {
	Version      int            `json:"version"`
	BaselineHead string         `json:"baseline_head"`
	Changes      map[string]int `json:"changes"`
}

type acceptedPatchState struct {
	oldLine        int
	inHunk         bool
	previousChange byte
}

const (
	acceptedFixScopeStateFile   = "accepted-fix-scope.json"
	acceptedFixScopeCurrentDiff = "current-diff"
	acceptedFixScopeVersion     = 1
)

var zeroContextHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+[0-9]+(?:,[0-9]+)? @@`)

func (w *Workflow) prepareAcceptedFixScope(mode string) {
	_ = w.state.Remove(acceptedFixScopeStateFile)
	if mode != acceptedFixScopeCurrentDiff || !w.acceptedFixScopeBaselineSafe() {
		return
	}
	baselineHead := w.state.ReadOr("baseline-head", "")
	if baselineHead == "" {
		return
	}
	changes, err := captureAcceptedChangeSet(w.config.RepoRoot, baselineHead)
	if err != nil {
		return
	}
	data, err := json.Marshal(acceptedFixScope{
		Version:      acceptedFixScopeVersion,
		BaselineHead: baselineHead,
		Changes:      changes,
	})
	if err != nil {
		return
	}
	_ = w.state.Write(acceptedFixScopeStateFile, string(data))
}

func (w *Workflow) acceptedFixScopeCoversCurrent() bool {
	return w.acceptedFixScopeAllowsCurrent(true)
}

func (w *Workflow) acceptedFixScopeContainsCurrent() bool {
	return w.acceptedFixScopeAllowsCurrent(false)
}

func (w *Workflow) acceptedFixScopeAllowsCurrent(consume bool) bool {
	data, err := os.ReadFile(w.state.Path(acceptedFixScopeStateFile))
	if err != nil {
		return false
	}
	var scope acceptedFixScope
	if err := json.Unmarshal(bytes.TrimSpace(data), &scope); err != nil || scope.Version != acceptedFixScopeVersion {
		return false
	}
	if scope.BaselineHead == "" || scope.BaselineHead != w.state.ReadOr("baseline-head", "") {
		return false
	}
	current, err := captureAcceptedChangeSet(w.config.RepoRoot, scope.BaselineHead)
	if err != nil || !changeSetSubset(current, scope.Changes) {
		return false
	}
	if consume {
		_ = w.state.Remove(acceptedFixScopeStateFile)
	}
	return true
}

func (w *Workflow) acceptedFixScopeBaselineSafe() bool {
	data, err := os.ReadFile(w.state.Path("baseline-status"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		paths, ok := porcelainStatusPaths(line)
		if !ok {
			return false
		}
		for _, path := range paths {
			if !isParentManagedImplementationPath(path) {
				return false
			}
		}
	}
	return true
}

func porcelainStatusPaths(line string) ([]string, bool) {
	if len(line) < 4 || line[2] != ' ' {
		return nil, false
	}
	value := strings.TrimSpace(line[3:])
	if value == "" {
		return nil, false
	}
	parts := strings.Split(value, " -> ")
	paths := make([]string, 0, len(parts))
	for _, raw := range parts {
		path, ok := porcelainPath(raw)
		if !ok {
			return nil, false
		}
		paths = append(paths, path)
	}
	return paths, true
}

func porcelainPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, `"`) {
		decoded, err := strconv.Unquote(value)
		if err != nil {
			return "", false
		}
		value = decoded
	}
	value = filepath.ToSlash(value)
	return value, value != ""
}

func isParentManagedImplementationPath(path string) bool {
	path = filepath.ToSlash(path)
	return path == implementationRulesFile ||
		path == implementationPlanFile ||
		path == implementationHistoryFile ||
		strings.HasPrefix(path, implementationTasksDir+"/")
}

func captureAcceptedChangeSet(repoRoot, baselineHead string) (map[string]int, error) {
	paths, err := collectChangedPaths(repoRoot, baselineHead)
	if err != nil {
		return nil, err
	}
	changes := make(map[string]int)
	for _, path := range paths {
		path = filepath.ToSlash(path)
		if isParentManagedImplementationPath(path) {
			continue
		}
		patch, err := exec.Command("git", "-C", repoRoot, "diff", "--no-renames", "--unified=0", "--no-ext-diff", "--no-color", baselineHead, "--", path).Output()
		if err != nil {
			return nil, fmt.Errorf("accepted scope diff %s: %w", path, err)
		}
		if len(patch) == 0 {
			if err := addUntrackedScopeChange(changes, repoRoot, path); err != nil {
				return nil, err
			}
			continue
		}
		if err := addPatchScopeChanges(changes, path, patch); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

func addUntrackedScopeChange(changes map[string]int, repoRoot, path string) error {
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return fmt.Errorf("accepted scope cannot compare binary untracked file %s", path)
	}
	sum := sha256.Sum256(data)
	changes["untracked\x00"+path+"\x00"+hex.EncodeToString(sum[:])]++
	return nil
}

func addPatchScopeChanges(changes map[string]int, path string, patch []byte) error {
	if bytes.Contains(patch, []byte("GIT binary patch")) || bytes.Contains(patch, []byte("Binary files ")) || bytes.IndexByte(patch, 0) >= 0 {
		return fmt.Errorf("accepted scope cannot compare binary patch %s", path)
	}
	state := acceptedPatchState{}
	for _, line := range strings.Split(string(patch), "\n") {
		handled, err := state.addMetadataChange(changes, path, line)
		if err != nil {
			return err
		}
		if handled {
			continue
		}
		if err := state.addHunkChange(changes, path, line); err != nil {
			return err
		}
	}
	return nil
}

func (s *acceptedPatchState) addMetadataChange(changes map[string]int, path, line string) (bool, error) {
	switch {
	case line == "":
		return true, nil
	case strings.HasPrefix(line, "diff --git "), strings.HasPrefix(line, "index "), strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
		return true, nil
	case strings.HasPrefix(line, "old mode "), strings.HasPrefix(line, "new mode "), strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "):
		changes["meta\x00"+path+"\x00"+line]++
		return true, nil
	case strings.HasPrefix(line, "@@ "):
		match := zeroContextHunk.FindStringSubmatch(line)
		if match == nil {
			return true, fmt.Errorf("accepted scope cannot parse hunk %q", line)
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return true, err
		}
		s.oldLine = value
		s.inHunk = true
		s.previousChange = 0
		return true, nil
	case strings.HasPrefix(line, `\ No newline at end of file`):
		if !s.inHunk || s.previousChange == 0 {
			return true, fmt.Errorf("accepted scope cannot place no-newline marker in %s", path)
		}
		changes[fmt.Sprintf("newline\x00%s\x00%c\x00%d", path, s.previousChange, s.oldLine)]++
		return true, nil
	case !s.inHunk:
		return true, fmt.Errorf("accepted scope cannot parse patch metadata %q", line)
	default:
		return false, nil
	}
}

func (s *acceptedPatchState) addHunkChange(changes map[string]int, path, line string) error {
	switch line[0] {
	case '-':
		changes[fmt.Sprintf("line\x00%s\x00-\x00%d\x00%s", path, s.oldLine, line[1:])]++
		s.oldLine++
		s.previousChange = '-'
	case '+':
		changes[fmt.Sprintf("line\x00%s\x00+\x00%d\x00%s", path, s.oldLine, line[1:])]++
		s.previousChange = '+'
	case ' ':
		s.oldLine++
		s.previousChange = 0
	default:
		return fmt.Errorf("accepted scope cannot parse patch line %q", line)
	}
	return nil
}

func changeSetSubset(current, accepted map[string]int) bool {
	for signature, count := range current {
		if count > accepted[signature] {
			return false
		}
	}
	return true
}
