package workflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskdiff"
)

type acceptedFixScope struct {
	Version           int            `json:"version"`
	OwnerTaskID       string         `json:"owner_task_id"`
	OwnerParentLease  int64          `json:"owner_parent_lease"`
	OwnerAction       string         `json:"owner_action"`
	OwnerInvocationID string         `json:"owner_invocation_id"`
	BaselineHead      string         `json:"baseline_head"`
	Changes           map[string]int `json:"changes"`
}

type acceptedPatchState struct {
	oldLine        int
	inHunk         bool
	previousChange byte
}

const (
	acceptedFixScopeStateFile   = "accepted-fix-scope.json"
	acceptedFixScopeCurrentDiff = "current-diff"
	acceptedFixScopeVersion     = 3
)

var zeroContextHunk = regexp.MustCompile(`^@@ -([0-9]+)(?:,[0-9]+)? \+[0-9]+(?:,[0-9]+)? @@`)

func (w *Workflow) prepareAcceptedFixScopeChecked(mode string) error {
	return w.prepareAcceptedFixScopeForAction(mode, state.ParentActionFix)
}

func (w *Workflow) prepareAcceptedFixScopeForAction(mode string, action state.ParentAction) error {
	if action != state.ParentActionFix && action != state.ParentActionApproveSurface {
		return fmt.Errorf("accepted fix scope cannot bind parent action %q", action)
	}
	if err := w.invalidateAcceptedFixScope(); err != nil {
		return err
	}
	if mode != acceptedFixScopeCurrentDiff {
		return nil
	}
	baselineHead := w.state.ReadOr("baseline-head", "")
	if baselineHead == "" {
		return nil
	}
	invocationID := w.acceptedFixScopeInvocationID()
	if invocationID == "" {
		return fmt.Errorf("accepted fix scope requires an active parent action invocation")
	}
	taskID, err := w.state.TaskID()
	if err != nil {
		return err
	}
	lease, err := w.state.ParentEvidenceLeaseEpoch()
	if err != nil {
		return err
	}
	changes, err := w.captureAcceptedChangeSet()
	if err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	data, err := json.Marshal(acceptedFixScope{
		Version:           acceptedFixScopeVersion,
		OwnerTaskID:       taskID,
		OwnerParentLease:  lease,
		OwnerAction:       string(action),
		OwnerInvocationID: invocationID,
		BaselineHead:      baselineHead,
		Changes:           changes,
	})
	if err != nil {
		return err
	}
	return w.state.Write(acceptedFixScopeStateFile, string(data))
}

func (w *Workflow) invalidateAcceptedFixScope() error {
	if err := w.state.Write(acceptedFixScopeStateFile, "{}"); err != nil {
		return err
	}
	return w.state.Remove(acceptedFixScopeStateFile)
}

func (w *Workflow) discardPreparedAcceptedFixScope() error {
	return w.invalidateAcceptedFixScope()
}

func (w *Workflow) acceptedFixScopeCoversCurrent() bool {
	if w.state.TaskStatus() != state.TaskStatusActive {
		return false
	}
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
	if !w.acceptedFixScopeOwnerAllows(scope) {
		return false
	}
	if scope.BaselineHead == "" || scope.BaselineHead != w.state.ReadOr("baseline-head", "") {
		return false
	}
	current, err := w.captureAcceptedChangeSet()
	if err != nil || !changeSetSubset(current, scope.Changes) {
		return false
	}
	if consume {
		if err := w.invalidateAcceptedFixScope(); err != nil {
			return false
		}
	}
	return true
}

func (w *Workflow) acceptedFixScopeOwnerAllows(scope acceptedFixScope) bool {
	taskID, err := w.state.TaskID()
	if err != nil || scope.OwnerTaskID == "" || scope.OwnerTaskID != taskID {
		return false
	}
	lease, err := w.state.ParentEvidenceLeaseEpoch()
	if err != nil || scope.OwnerParentLease != lease {
		return false
	}
	invocationID := w.acceptedFixScopeInvocationID()
	if invocationID == "" || scope.OwnerInvocationID != invocationID {
		return false
	}
	action := state.ParentAction(scope.OwnerAction)
	switch w.state.TaskStatus() {
	case state.TaskStatusActive:
		return action == state.ParentActionFix || action == state.ParentActionApproveSurface
	case state.TaskStatusWaitingSolReview:
		return action == state.ParentActionApproveSurface
	default:
		return false
	}
}

func (w *Workflow) acceptedFixScopeInvocationID() string {
	if w.temp == "" {
		return ""
	}
	info, err := os.Stat(w.temp)
	if err != nil || !info.IsDir() {
		return ""
	}
	sum := sha256.Sum256([]byte(w.temp))
	return hex.EncodeToString(sum[:])
}

func isParentManagedImplementationPath(path string) bool {
	path = filepath.ToSlash(path)
	return path == implementationRulesFile ||
		path == implementationPlanFile ||
		path == implementationHistoryFile ||
		strings.HasPrefix(path, implementationTasksDir+"/")
}

func (w *Workflow) captureAcceptedChangeSet() (map[string]int, error) {
	paths, err := w.collectChangedPaths(w.config.RepoRoot, w.state.ReadOr("baseline-head", ""))
	if err != nil {
		return nil, err
	}
	return captureAcceptedChangeSetForPaths(w.config.RepoRoot, w.state, paths)
}

func captureAcceptedChangeSetForPaths(repoRoot string, st *state.StateStore, paths []string) (map[string]int, error) {
	scopePaths := make([]string, 0, len(paths))
	for _, path := range paths {
		if !isParentManagedImplementationPath(filepath.ToSlash(path)) {
			scopePaths = append(scopePaths, path)
		}
	}
	changes := make(map[string]int)
	if len(scopePaths) == 0 {
		return changes, nil
	}
	patches, err := taskdiff.BaselineWorktreePathPatches(repoRoot, st, scopePaths)
	if err != nil {
		return nil, err
	}
	for _, path := range scopePaths {
		path = filepath.ToSlash(path)
		patch, differs := patches[path]
		if !differs {
			continue
		}
		if patch == nil {
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
