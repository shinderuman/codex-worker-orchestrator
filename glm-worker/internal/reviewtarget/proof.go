package reviewtarget

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ProofError struct {
	Code       string
	Target     string
	Correction string
}

func (e *ProofError) Error() string {
	return fmt.Sprintf("review target proof [%s] %q is unavailable; correction=%s", e.Code, e.Target, e.Correction)
}

func ValidateProofAddressable(repoRoot string, target Target) error {
	switch target.Kind {
	case LocatorWholeDiff:
		body, err := targetDiff(repoRoot, target.Path)
		if err != nil {
			return err
		}
		if len(body) == 0 {
			return proofError("diff-absent", target, "use @diff only for a path with an actual HEAD-to-current diff; otherwise use an exact line/range or Go declaration locator")
		}
		return nil
	case LocatorGoSymbol:
		content, err := readRepositoryFile(repoRoot, target.Path)
		if err != nil {
			return proofError("symbol-source-unavailable", target, "use a declaration locator only for a current readable Go source declaration")
		}
		if _, err := FindGoDeclaration(content, target.Locator); err != nil {
			return proofError("symbol-not-declared", target, "use the exact top-level Go declaration name or Type.Member identity that exists in the current source")
		}
		return nil
	case LocatorLineRange:
		content, err := readRepositoryFile(repoRoot, target.Path)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			if target.LineEnd <= len(lines) {
				return nil
			}
			return proofError("line-out-of-range", target, "use a line/range that exists in the current file")
		}
		if !errors.Is(err, os.ErrNotExist) {
			return proofError("line-source-unavailable", target, "use a readable repository file line/range")
		}
		body, diffErr := targetDiff(repoRoot, target.Path)
		if diffErr == nil && len(body) != 0 && strings.Contains(body, "+++ /dev/null") {
			return nil
		}
		return proofError("line-source-missing", target, "use @diff for a deleted changed file or a line/range in a current file")
	default:
		return proofError("locator-kind", target, canonicalCorrection)
	}
}

func readRepositoryFile(repoRoot, path string) ([]byte, error) {
	root, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return nil, err
	}
	candidate := filepath.Join(root, filepath.FromSlash(path))
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return nil, err
	}
	if canonical != root && !strings.HasPrefix(canonical, root+string(filepath.Separator)) {
		return nil, fmt.Errorf("review target path escapes repository: %s", path)
	}
	info, err := os.Lstat(canonical)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("review target path is not a regular file: %s", path)
	}
	return os.ReadFile(canonical)
}

func targetDiff(repoRoot, path string) ([]byte, error) {
	command := exec.Command("git", "-C", repoRoot, "diff", "HEAD", "--no-ext-diff", "--no-renames", "--", path)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("capture review target diff %s: %w", path, err)
	}
	return output, nil
}

func proofError(code string, target Target, correction string) error {
	return &ProofError{Code: code, Target: target.Path + ":" + target.Locator, Correction: correction}
}
