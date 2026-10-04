package reviewtarget

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type ProofError struct {
	Code       string
	Target     string
	Correction string
	Cause      error
}

func (e *ProofError) Error() string {
	message := fmt.Sprintf("review target proof [%s] %q is unavailable; correction=%s", e.Code, e.Target, e.Correction)
	if e.Cause != nil {
		message += fmt.Sprintf("; cause=%v", e.Cause)
	}
	return message
}

func (e *ProofError) Unwrap() error {
	return e.Cause
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
		return validateGoSymbolProofAddressable(repoRoot, target)
	case LocatorLineRange:
		return validateLineRangeProofAddressable(repoRoot, target)
	default:
		return proofError("locator-kind", target, canonicalCorrection)
	}
}

func validateGoSymbolProofAddressable(repoRoot string, target Target) error {
	content, err := readRepositoryFile(repoRoot, target.Path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return validateDeletedGoSymbolProofAddressable(repoRoot, target)
		}
		return proofError("symbol-source-unavailable", target, "use a declaration locator only for a readable current source or a deleted Go source with canonical diff evidence")
	}
	declaration, err := FindGoDeclaration(content, target.Locator)
	if err != nil {
		return proofError("symbol-not-declared", target, "use the exact top-level Go declaration name or Type.Member identity that exists in the current source")
	}
	if declaration.LineEnd-declaration.LineStart+1 > MaxSourceProofLines {
		return proofError("symbol-source-too-large", target, "use an exact numeric line/range within the canonical source proof bound")
	}
	return nil
}

func validateDeletedGoSymbolProofAddressable(repoRoot string, target Target) error {
	body, err := targetDiff(repoRoot, target.Path)
	if err != nil {
		return err
	}
	if len(body) == 0 || !bytes.Contains(body, []byte("+++ /dev/null")) {
		return proofError("symbol-source-missing", target, "use a declaration locator only for a current declaration or a declaration in a deleted changed Go file")
	}
	content, err := headFile(repoRoot, target.Path)
	if err != nil {
		return proofError("deleted-symbol-head-unavailable", target, "use a deleted-file declaration only when the exact source exists in HEAD")
	}
	declaration, err := FindGoDeclaration(content, target.Locator)
	if err != nil {
		return proofError("deleted-symbol-not-declared", target, "use the exact top-level Go declaration name or Type.Member identity that existed in the deleted HEAD source")
	}
	if declaration.LineEnd-declaration.LineStart+1 > MaxSourceProofLines {
		return proofError("deleted-symbol-source-too-large", target, "use an exact numeric line/range within the canonical source proof bound")
	}
	return nil
}

func validateLineRangeProofAddressable(repoRoot string, target Target) error {
	if target.LineEnd-target.LineStart+1 > MaxSourceProofLines {
		return proofError("line-range-too-large", target, "split the source proof into canonical ranges within the source proof bound")
	}
	content, err := readRepositoryFile(repoRoot, target.Path)
	if err == nil {
		if !textLineEvidence(content) {
			return proofError("line-source-nontext", target, "use a line/range only for UTF-8 text source; use @diff when binary diff evidence exists")
		}
		if lineRangeExists(content, target.LineEnd) {
			return nil
		}
		return proofError("line-out-of-range", target, "use a line/range that exists in the current file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return proofError("line-source-unavailable", target, "use a readable repository file line/range")
	}
	return validateDeletedLineRange(repoRoot, target)
}

func validateDeletedLineRange(repoRoot string, target Target) error {
	body, err := targetDiff(repoRoot, target.Path)
	if err != nil {
		return err
	}
	if len(body) == 0 || !bytes.Contains(body, []byte("+++ /dev/null")) {
		return proofError("line-source-missing", target, "use @diff for a deleted changed file or a line/range in a current file")
	}
	content, err := headFile(repoRoot, target.Path)
	if err != nil {
		return proofError("deleted-line-head-unavailable", target, "use a deleted-file line/range only when that exact line exists in HEAD")
	}
	if !textLineEvidence(content) {
		return proofError("deleted-line-nontext", target, "use a deleted-file line/range only for UTF-8 text source")
	}
	if !lineRangeExists(content, target.LineEnd) {
		return proofError("deleted-line-out-of-range", target, "use a line/range that existed in the deleted HEAD file")
	}
	return nil
}

func textLineEvidence(content []byte) bool {
	return utf8.Valid(content) && !bytes.ContainsRune(content, '\x00')
}

func lineRangeExists(content []byte, end int) bool {
	if end <= 0 || len(content) == 0 {
		return false
	}
	lines := bytes.Count(content, []byte{'\n'}) + 1
	if content[len(content)-1] == '\n' {
		lines--
	}
	return end <= lines
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
		return nil, &ProofError{
			Code:       "diff-unavailable",
			Target:     path + ":@diff",
			Correction: "retry after canonical Git diff evidence is available; do not substitute an unproved target",
			Cause:      err,
		}
	}
	return output, nil
}

func headFile(repoRoot, path string) ([]byte, error) {
	command := exec.Command("git", "-C", repoRoot, "show", "HEAD:"+path)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("read HEAD review target %s: %w", path, err)
	}
	return output, nil
}

func proofError(code string, target Target, correction string) error {
	return &ProofError{Code: code, Target: target.Path + ":" + target.Locator, Correction: correction}
}
