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

type reviewNextPlanSections struct {
	outside strings.Builder
	next    []string
	found   bool
	inNext  bool
}

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
		return current, packet.ValidateReviewTargetProof(repo, targets)
	}
	if err := validateEquivalentReviewAdvancement(repo, original, current, targets); err != nil {
		return state.SnapshotDigest{}, err
	}
	return current, nil
}

func validateEquivalentReviewAdvancement(repo string, original, current state.SnapshotDigest, targets []string) error {
	if _, err := reviewSourceGit(repo, "merge-base", "--is-ancestor", original.Head, current.Head); err != nil {
		return fmt.Errorf("review source is not a proven fast-forward: %w", err)
	}
	changed, added, err := metadataReviewAdvancement(repo, original.Head, current.Head)
	if err != nil {
		return err
	}
	if err := equivalentNextOnlyPlan(repo, original.Head, current.Head, added); err != nil {
		return err
	}
	if err := equivalentPreAdvancementIndex(repo, original.Head, current.Head, original.IndexDigest, changed); err != nil {
		return err
	}
	if err := verifyEquivalentReviewTargets(targets, changed); err != nil {
		return err
	}
	return packet.ValidateReviewTargetProof(repo, targets)
}

func verifyEquivalentReviewTargets(targets []string, changed map[string]string) error {
	for _, target := range targets {
		for path := range changed {
			if strings.HasPrefix(target, path+":") {
				return fmt.Errorf("review target %q is modified by the advancement", target)
			}
		}
	}
	return nil
}

func reviewSourceGit(repo string, args ...string) ([]byte, error) {
	out, err := gitexec.Command("git", repo, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

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
		switch {
		case status == "M" && path == state.ParentPlanFile:
			planModified = true
		case status == "A" && strings.HasPrefix(path, state.ParentTasksDir+"/") && strings.HasSuffix(path, ".md"):
			added[path] = true
		default:
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
	oldOutside, oldNext, err := readReviewPlanAt(repo, oldHead)
	if err != nil {
		return err
	}
	newOutside, newNext, err := readReviewPlanAt(repo, newHead)
	if err != nil {
		return err
	}
	if oldOutside != newOutside {
		return fmt.Errorf("active, goal, blocked or other plan contract changed")
	}
	if err := verifyEquivalentNextSchedule(oldNext, newNext, added); err != nil {
		return err
	}
	return verifyEquivalentActiveClosure(repo, oldHead, oldOutside, added)
}

func readReviewPlanAt(repo, commit string) (string, []string, error) {
	raw, err := reviewSourceGit(repo, "show", commit+":"+state.ParentPlanFile)
	if err != nil {
		return "", nil, err
	}
	return splitReviewNextPlan(string(raw))
}

func verifyEquivalentNextSchedule(oldNext, newNext []string, added map[string]bool) error {
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
			return fmt.Errorf("plan NEXT was changed beyond new Task additions")
		}
		delete(remaining, path)
	}
	if i != len(oldNext) || len(remaining) != 0 {
		return fmt.Errorf("plan NEXT reordered, removed, or omitted a Task")
	}
	return nil
}

func verifyEquivalentActiveClosure(repo, oldHead, oldOutside string, added map[string]bool) error {
	activePath, ok := reviewPlanActivePath(oldOutside)
	if !ok {
		return fmt.Errorf("old active task is not unambiguously identified")
	}
	active, err := reviewSourceGit(repo, "show", oldHead+":"+activePath)
	if err != nil {
		return err
	}
	for path := range added {
		if strings.Contains(string(active), path) {
			return fmt.Errorf("new Task %s changes ACTIVE reference closure", path)
		}
	}
	return nil
}

func splitReviewNextPlan(plan string) (string, []string, error) {
	var sections reviewNextPlanSections
	for _, line := range strings.SplitAfter(plan, "\n") {
		if err := sections.addLine(line); err != nil {
			return "", nil, err
		}
	}
	if !sections.found {
		return "", nil, fmt.Errorf("next section missing")
	}
	return sections.outside.String(), sections.next, nil
}

func (s *reviewNextPlanSections) addLine(line string) error {
	if strings.HasPrefix(line, "## ") {
		if strings.HasPrefix(line, "## NEXT") {
			if s.found {
				return fmt.Errorf("duplicate NEXT section")
			}
			s.found, s.inNext = true, true
			s.outside.WriteString(line)
			return nil
		}
		s.inNext = false
	}
	if !s.inNext {
		s.outside.WriteString(line)
		return nil
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if _, ok := reviewPlanTaskPath(trimmed); !ok {
		return fmt.Errorf("unrecognized NEXT contract entry: %s", trimmed)
	}
	s.next = append(s.next, trimmed)
	return nil
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
	active := ""
	for _, line := range strings.Split(planWithoutNext, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.HasPrefix(line, "## ACTIVE")
			continue
		}
		if !section || strings.TrimSpace(line) == "" {
			continue
		}
		path, ok := reviewPlanTaskPath(strings.TrimSpace(line))
		if !ok || active != "" {
			return "", false
		}
		active = path
	}
	return active, active != ""
}

func equivalentPreAdvancementIndex(repo, oldHead, newHead, oldDigest string, changed map[string]string) error {
	index, err := reviewSourceGit(repo, "ls-files", "-s")
	if err != nil {
		return err
	}
	lines, err := parseEquivalentIndex(index)
	if err != nil {
		return err
	}
	if err := restorePreviousMetadataEntries(repo, oldHead, newHead, changed, lines); err != nil {
		return err
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

func parseEquivalentIndex(index []byte) (map[string]string, error) {
	lines := make(map[string]string)
	for _, line := range strings.Split(strings.TrimSuffix(string(index), "\n"), "\n") {
		head, path, ok := strings.Cut(line, "\t")
		fields := strings.Fields(head)
		if !ok || len(fields) != 3 || fields[2] != "0" {
			return nil, fmt.Errorf("review index cannot be reconstructed from unmerged or ambiguous entries")
		}
		lines[path] = line
	}
	return lines, nil
}

func restorePreviousMetadataEntries(repo, oldHead, newHead string, changed map[string]string, lines map[string]string) error {
	for path, status := range changed {
		expectedCurrent, err := oldMetadataIndexEntry(repo, newHead, path)
		if err != nil {
			return err
		}
		if lines[path] != expectedCurrent {
			return fmt.Errorf("staged metadata differs from committed fast-forward at %s", path)
		}
		delete(lines, path)
		if status == "A" {
			continue
		}
		line, err := oldMetadataIndexEntry(repo, oldHead, path)
		if err != nil {
			return err
		}
		lines[path] = line
	}
	return nil
}

func oldMetadataIndexEntry(repo, oldHead, path string) (string, error) {
	out, err := reviewSourceGit(repo, "ls-tree", oldHead, "--", path)
	if err != nil {
		return "", err
	}
	parts := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(parts) != 1 {
		return "", fmt.Errorf("ambiguous old index metadata for %s", path)
	}
	meta, name, ok := strings.Cut(parts[0], "\t")
	fields := strings.Fields(meta)
	if !ok || name != path || len(fields) != 3 || fields[1] != "blob" {
		return "", fmt.Errorf("old review metadata blob missing for %s", path)
	}
	return fmt.Sprintf("%s %s 0\t%s", fields[0], fields[2], path), nil
}
