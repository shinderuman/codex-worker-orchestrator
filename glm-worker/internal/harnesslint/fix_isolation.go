package harnesslint

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type isolatedFixRun func(string) (Report, error)

type fixFileState struct {
	mode os.FileMode
	data []byte
	link string
}

type fixManifest map[string]fixFileState

func runWithIsolatedFixes(root string, execute isolatedFixRun) (Report, error) {
	paths, err := repositoryPaths(root)
	if err != nil {
		return Report{}, err
	}
	before, err := captureFixManifest(root, paths)
	if err != nil {
		return Report{}, err
	}

	workspace, err := os.MkdirTemp("", "harnesslint-fix-*")
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = os.RemoveAll(workspace) }()
	if err := materializeFixManifest(workspace, before); err != nil {
		return Report{}, err
	}
	if err := initializeFixWorkspace(workspace); err != nil {
		return Report{}, err
	}

	report, err := execute(workspace)
	if err != nil {
		return Report{}, err
	}
	afterPaths, err := repositoryPaths(workspace)
	if err != nil {
		return Report{}, err
	}
	after, err := captureFixManifest(workspace, afterPaths)
	if err != nil {
		return Report{}, err
	}
	changed, err := fixManifestChangedPaths(before, after)
	if err != nil {
		return Report{}, err
	}
	if len(changed) != report.Fixed {
		return Report{}, fmt.Errorf("quality fixer change count mismatch: report=%d postimages=%d", report.Fixed, len(changed))
	}
	if len(changed) == 0 {
		return report, nil
	}

	if err := verifyFixManifest(root, before); err != nil {
		return Report{}, fmt.Errorf("quality fixer input changed while isolated fixer ran: %w", err)
	}
	for _, path := range changed {
		if err := applyFixPostimage(root, path, before[path], after[path]); err != nil {
			return Report{}, err
		}
	}
	if err := verifyFixManifest(root, after); err != nil {
		return Report{}, fmt.Errorf("quality fixer postimage verification failed: %w", err)
	}
	report.FixEvidence = &FixEvidence{Method: FixProvenanceIsolatedPostimageV1}
	return report, nil
}

func initializeFixWorkspace(root string) error {
	for _, args := range [][]string{{"init", "-q"}, {"add", "-f", "--all"}} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("prepare isolated quality fixer git state (%s): %w: %s", args[0], err, bytes.TrimSpace(output))
		}
	}
	return nil
}

func captureFixManifest(root string, paths []string) (fixManifest, error) {
	manifest := make(fixManifest, len(paths))
	for _, path := range paths {
		state, err := captureFixFileState(root, path)
		if err != nil {
			return nil, err
		}
		manifest[path] = state
	}
	return manifest, nil
}

func captureFixFileState(root, path string) (fixFileState, error) {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return fixFileState{}, fmt.Errorf("inspect quality fixer path %s: %w", path, err)
	}
	switch {
	case info.Mode().IsRegular():
		data, err := os.ReadFile(absolute)
		if err != nil {
			return fixFileState{}, fmt.Errorf("read quality fixer path %s: %w", path, err)
		}
		return fixFileState{mode: info.Mode(), data: data}, nil
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(absolute)
		if err != nil {
			return fixFileState{}, fmt.Errorf("read quality fixer symlink %s: %w", path, err)
		}
		return fixFileState{mode: info.Mode(), link: target}, nil
	default:
		return fixFileState{}, fmt.Errorf("unsupported quality fixer path type %s: %s", path, info.Mode().Type())
	}
}

func materializeFixManifest(root string, manifest fixManifest) error {
	paths := fixManifestPaths(manifest)
	for _, path := range paths {
		state := manifest[path]
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			return err
		}
		switch {
		case state.mode.IsRegular():
			if err := os.WriteFile(absolute, state.data, state.mode.Perm()); err != nil {
				return fmt.Errorf("materialize quality fixer path %s: %w", path, err)
			}
		case state.mode&os.ModeSymlink != 0:
			if err := os.Symlink(state.link, absolute); err != nil {
				return fmt.Errorf("materialize quality fixer symlink %s: %w", path, err)
			}
		default:
			return fmt.Errorf("unsupported quality fixer manifest path %s", path)
		}
	}
	return nil
}

func fixManifestChangedPaths(before, after fixManifest) ([]string, error) {
	if len(before) != len(after) {
		return nil, fmt.Errorf("quality fixer changed repository inventory")
	}
	var changed []string
	for _, path := range fixManifestPaths(before) {
		beforeState := before[path]
		afterState, ok := after[path]
		if !ok {
			return nil, fmt.Errorf("quality fixer removed repository path %s", path)
		}
		if beforeState.mode.Type() != afterState.mode.Type() || beforeState.mode.Perm() != afterState.mode.Perm() {
			return nil, fmt.Errorf("quality fixer changed file mode or type for %s", path)
		}
		if beforeState.mode.IsRegular() {
			if !bytes.Equal(beforeState.data, afterState.data) {
				changed = append(changed, path)
			}
			continue
		}
		if beforeState.link != afterState.link {
			return nil, fmt.Errorf("quality fixer changed symlink target for %s", path)
		}
	}
	return changed, nil
}

func verifyFixManifest(root string, expected fixManifest) error {
	paths, err := repositoryPaths(root)
	if err != nil {
		return err
	}
	current, err := captureFixManifest(root, paths)
	if err != nil {
		return err
	}
	if len(current) != len(expected) {
		return fmt.Errorf("repository inventory changed")
	}
	for _, path := range fixManifestPaths(expected) {
		got, ok := current[path]
		if !ok || !sameFixFileState(expected[path], got) {
			return fmt.Errorf("repository path changed: %s", path)
		}
	}
	return nil
}

func sameFixFileState(a, b fixFileState) bool {
	if a.mode.Type() != b.mode.Type() || a.mode.Perm() != b.mode.Perm() {
		return false
	}
	if a.mode.IsRegular() {
		return bytes.Equal(a.data, b.data)
	}
	return a.link == b.link
}

func applyFixPostimage(root, path string, before, after fixFileState) error {
	if !before.mode.IsRegular() || !after.mode.IsRegular() {
		return fmt.Errorf("quality fixer postimage is not a regular file: %s", path)
	}
	current, err := captureFixFileState(root, path)
	if err != nil {
		return err
	}
	if !sameFixFileState(before, current) {
		return fmt.Errorf("quality fixer path changed before postimage apply: %s", path)
	}
	absolute := filepath.Join(root, filepath.FromSlash(path))
	file, err := os.CreateTemp(filepath.Dir(absolute), ".harnesslint-fix-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(after.mode.Perm()); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(after.data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, absolute); err != nil {
		return fmt.Errorf("apply quality fixer postimage %s: %w", path, err)
	}
	return nil
}

func fixManifestPaths(manifest fixManifest) []string {
	paths := make([]string, 0, len(manifest))
	for path := range manifest {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
