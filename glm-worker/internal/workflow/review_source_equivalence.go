package workflow

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/gitexec"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

// equivalentReviewSource proves a narrow committed metadata fast-forward without
// accepting a changed review subject. It does not authorize source adoption.
// The original snapshot is the one sealed with the independent Reviewer packet.
func equivalentReviewSource(repo string, original state.SnapshotDigest, targets []string) (state.SnapshotDigest, error) {
	snapshot, err := state.CaptureGitSnapshot(repo)
	if err != nil {
		return state.SnapshotDigest{}, err
	}
	current := state.SnapshotDigest{Head: snapshot.Head, IndexDigest: snapshot.IndexDigest, WorktreeDigest: snapshot.WorktreeDigest}
	if original.Head == "" || original.IndexDigest == "" || original.WorktreeDigest == "" ||
		current.Head == "" || current.WorktreeDigest != original.WorktreeDigest || len(targets) == 0 {
		return state.SnapshotDigest{}, fmt.Errorf("review source cannot prove unchanged working-tree and review identity")
	}
	if original.Head == current.Head {
		if original.IndexDigest != current.IndexDigest {
			return state.SnapshotDigest{}, fmt.Errorf("review source index changed without a committed advancement")
		}
		if err := packet.ValidateReviewTargetProof(repo, targets); err != nil {
			return state.SnapshotDigest{}, err
		}
		return current, nil
	}
	if _, err := reviewSourceGit(repo, "merge-base", "--is-ancestor", original.Head, current.Head); err != nil {
		return state.SnapshotDigest{}, fmt.Errorf("review source is not a proven fast-forward: %w", err)
	}
	changed, added, err := metadataReviewAdvancement(repo, original.Head, current.Head)
	if err != nil {
		return state.SnapshotDigest{}, err
	}
	if err := equivalentNextOnlyPlan(repo, original.Head, current.Head, added); err != nil {
		return state.SnapshotDigest{}, err
	}
	if err := equivalentPreAdvancementIndex(repo, original.Head, original.IndexDigest, changed); err != nil {
		return state.SnapshotDigest{}, err
	}
	for _, target := range targets {
		for path := range changed {
			if strings.HasPrefix(target, path+":") {
				return state.SnapshotDigest{}, fmt.Errorf("review target %q is modified by the advancement", target)
			}
		}
	}
	if err := packet.ValidateReviewTargetProof(repo, targets); err != nil {
		return state.SnapshotDigest{}, fmt.Errorf("current review targets are not addressable: %w", err)
	}
	return current, nil
}

func reviewSourceGit(repo string, args ...string) ([]byte, error) {
	out, err := gitexec.Command("git", repo, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// A metadata-only classification is insufficient: only adding a new, unreferenced
// NEXT task and its schedule entry leaves the active Review contract unchanged.
func metadataReviewAdvancement(repo, oldHead, newHead string) (map[string]string, map[string]bool, error) {
	out, err := reviewSourceGit(repo, "diff", "--name-status", "--no-renames", "-z", oldHead, newHead, "--")
	if err != nil {
		return nil, nil, err
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return nil, nil, fmt.Errorf("ambiguous committed advancement")
	}
	changed := make(map[string]string)
	added := make(map[string]bool)
	planModified := false
	for i := 0; i < len(fields); i += 2 {
		status, path := fields[i], fields[i+1]
		if status == "M" && path == state.ParentPlanFile {
			planModified = true
		} else if status == "A" && strings.HasPrefix(path, state.ParentTasksDir+"/") && strings.HasSuffix(path, ".md") {
			added[path] = true
		} else {
			return nil, nil, fmt.Errorf("review premise changed: %s %s", status, path)
		}
		changed[path] = status
	}
	if !planModified || len(added) == 0 {
		return nil, nil, fmt.Errorf("advancement is not a provable NEXT-only task addition")
	}
	return changed, added, nil
}

func equivalentNextOnlyPlan(repo, oldHead, newHead string, added map[string]bool) error {
	oldBytes, err := reviewSourceGit(repo, "show", oldHead+":"+state.ParentPlanFile)
	if err != nil {
		return err
	}
	newBytes, err := reviewSourceGit(repo, "show", newHead+":"+state.ParentPlanFile)
	if err != nil {
		return err
	}
	oldOutside, oldNext, err := splitReviewNextPlan(string(oldBytes))
	if err != nil {
		return err
	}
	newOutside, newNext, err := splitReviewNextPlan(string(newBytes))
	if err != nil {
		return err
	}
	if oldOutside != newOutside {
		return fmt.Errorf("ACTIVE, GOAL, BLOCKED or other Plan contract changed")
	}
	remaining := make(map[string]bool, len(added))
	for path := range added {
		remaining[path] = true
	}
	i := 0
	for _, row := range newNext {
		if i < len(oldNext) && row == oldNext[i] {
			i++
			continue
		}
		path, ok := reviewPlanTaskPath(row)
		if !ok || !remaining[path] {
			return fmt.Errorf("Plan NEXT was changed beyond new Task additions")
		}
		delete(remaining, path)
	}
	if i != len(oldNext) || len(remaining) != 0 {
		return fmt.Errorf("Plan NEXT reordered, removed, or omitted a Task")
	}
	activePath, ok := reviewPlanActivePath(oldOutside)
	if !ok {
		return fmt.Errorf("old ACTIVE task is not unambiguously identified")
	}
	active, err := reviewSourceGit(repo, "show", oldHead+":"+activePath)
	if err != nil {
		return err
	}
	for path := range added {
		// A previously unresolved reference to the newly created file changes
		// the old ACTIVE reference closure even if its own bytes are identical.
		if strings.Contains(string(active), path) {
			return fmt.Errorf("new Task %s changes ACTIVE reference closure", path)
		}
	}
	return nil
}

func splitReviewNextPlan(plan string) (string, []string, error) {
	var outside strings.Builder
	var next []string
	found, inNext := false, false
	for _, line := range strings.SplitAfter(plan, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			if strings.HasPrefix(line, "## NEXT") {
				if found {
					return "", nil, fmt.Errorf("duplicate NEXT section")
				}
				found, inNext = true, true
				outside.WriteString(line)
				continue
			}
			inNext = false
		}
		if !inNext {
			outside.WriteString(line)
		} else if trimmed != "" {
			if _, ok := reviewPlanTaskPath(trimmed); !ok {
				return "", nil, fmt.Errorf("unrecognized NEXT contract entry: %s", trimmed)
			}
			next = append(next, trimmed)
		}
	}
	if !found {
		return "", nil, fmt.Errorf("NEXT section missing")
	}
	return outside.String(), next, nil
}

func reviewPlanTaskPath(row string) (string, bool) {
	if !strings.HasPrefix(row, "- [\x60IMPLEMENTATION_TASKS/") {
		return "", false
	}
	rest := strings.TrimPrefix(row, "- [\x60")
	path, after, ok := strings.Cut(rest, "\x60](")
	if !ok || !strings.HasPrefix(path, state.ParentTasksDir+"/") ||
		!strings.HasSuffix(path, ".md") || after != path+")" {
		return "", false
	}
	return path, true
}

func reviewPlanActivePath(planWithoutNext string) (string, bool) {
	section := false
	for _, line := range strings.Split(planWithoutNext, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.HasPrefix(line, "## ACTIVE")
			continue
		}
		if section && strings.TrimSpace(line) != "" {
			return reviewPlanTaskPath(strings.TrimSpace(line))
		}
	}
	return "", false
}

// Reconstruct the pre-pull Git index from the current index and old committed
// metadata blobs. This proves staged implementation/test changes did not drift;
// comparing HEAD trees or the unstaged worktree digest alone cannot prove that.
func equivalentPreAdvancementIndex(repo, oldHead, oldDigest string, changed map[string]string) error {
	index, err := reviewSourceGit(repo, "ls-files", "-s")
	if err != nil {
		return err
	}
	lines := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(index), "\n"), "\n") {
		head, path, ok := strings.Cut(line, "\t")
		fields := strings.Fields(head)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return fmt.Errorf("review index cannot be reconstructed from unmerged or ambiguous entries")
		}
		lines[path] = line
	}
	for path, status := range changed {
		delete(lines, path)
		if status == "A" {
			continue
		}
		out, err := reviewSourceGit(repo, "ls-tree", oldHead, "--", path)
		if err != nil {
			return err
		}
		parts := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
		if len(parts) != 1 {
			return fmt.Errorf("ambiguous old index metadata for %s", path)
		}
		meta, name, ok := strings.Cut(parts[0], "\t")
		fields := strings.Fields(meta)
		if !ok || name != path || len(fields) != 3 || fields[1] != "blob" {
			return fmt.Errorf("old review metadata blob missing for %s", path)
		}
		lines[path] = fmt.Sprintf("%s %s 0\t%s", fields[0], fields[2], path)
	}
	names := make([]string, 0, len(lines))
	for path := range lines {
		names = append(names, path)
	}
	sort.Strings(names)
	var restored strings.Builder
	for _, name := range names {
		restored.WriteString(lines[name])
		restored.WriteByte('\n')
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(restored.String())))
	if digest != oldDigest {
		return fmt.Errorf("staged review source changed during advancement")
	}
	return nil
}
