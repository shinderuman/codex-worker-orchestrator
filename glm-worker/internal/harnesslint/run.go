package harnesslint

import (
	"crypto/sha256"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
)

func Check(root string) (Report, error) {
	return run(root, false, realCommandRunner{})
}

func Run(root string, fix bool) (Report, error) {
	return run(root, fix, realCommandRunner{})
}

func run(root string, fix bool, runner commandRunner) (Report, error) {
	if err := ValidateRoot(root); err != nil {
		return Report{}, err
	}
	paths, err := repositoryPaths(root)
	if err != nil {
		return Report{}, err
	}
	fixed := 0
	if fix {
		before, err := snapshots(root, paths)
		if err != nil {
			return Report{}, err
		}
		if err := fixGoFormatting(root, paths); err != nil {
			return Report{}, err
		}
		if err := runExternalFixers(root, paths, runner); err != nil {
			return Report{}, err
		}
		paths, err = repositoryPaths(root)
		if err != nil {
			return Report{}, err
		}
		after, err := snapshots(root, paths)
		if err != nil {
			return Report{}, err
		}
		fixed = changedSnapshotCount(before, after)
	}
	violations, err := checkRules(root, paths)
	if err != nil {
		return Report{}, err
	}
	external, err := runExternalChecks(root, paths, runner)
	if err != nil {
		return Report{}, err
	}
	violations = append(violations, external...)
	return makeReport(fixed, violations), nil
}

func checkRules(root string, paths []string) ([]Violation, error) {
	goViolations, err := scanGoRules(root, paths)
	if err != nil {
		return nil, err
	}
	textViolations, err := scanTextRules(root, paths)
	if err != nil {
		return nil, err
	}
	qualityViolations, err := scanQualitySurface(root, paths)
	if err != nil {
		return nil, err
	}
	violations := append([]Violation{}, goViolations...)
	violations = append(violations, textViolations...)
	violations = append(violations, qualityViolations...)
	return violations, nil
}

func fixGoFormatting(root string, paths []string) error {
	for _, path := range goFiles(paths) {
		data, err := readRegularFile(root, path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(data)
		if err != nil {
			return fmt.Errorf("gofmt %s: %w", path, err)
		}
		if string(formatted) == string(data) {
			continue
		}
		if err := writeRegularFile(root, path, formatted); err != nil {
			return err
		}
	}
	return nil
}

func snapshots(root string, paths []string) (map[string][32]byte, error) {
	result := make(map[string][32]byte)
	for _, path := range paths {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(absolute)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(absolute)
		if err != nil {
			return nil, err
		}
		result[path] = sha256.Sum256(data)
	}
	return result, nil
}

func changedSnapshotCount(before, after map[string][32]byte) int {
	changed := 0
	seen := make(map[string]bool, len(before)+len(after))
	for path, digest := range before {
		seen[path] = true
		if current, ok := after[path]; !ok || current != digest {
			changed++
		}
	}
	for path := range after {
		if !seen[path] {
			changed++
		}
	}
	return changed
}
