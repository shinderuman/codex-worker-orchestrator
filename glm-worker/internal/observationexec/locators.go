package observationexec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type LocatorError struct {
	Reason string
}

func (e *LocatorError) Error() string { return e.Reason }

func ValidateReferenceLocator(artifactRoot string, reference string) (string, error) {
	if reference == "" || reference == slotUnsetValue {
		return "", nil
	}
	if err := validateRelativeLocator(reference); err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(artifactRoot)
	if err != nil {
		return "", &LocatorError{Reason: fmt.Sprintf("基準artifact dirを解決できません: %v", err)}
	}
	candidate := filepath.Join(resolvedRoot, filepath.FromSlash(reference))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", &LocatorError{Reason: fmt.Sprintf("reference locatorを解決できません: %v", err)}
	}
	if pathOutsideRoot(resolvedRoot, resolved) {
		return "", &LocatorError{Reason: fmt.Sprintf("reference locator %qが基準artifact dirの外へ解決されました", reference)}
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", &LocatorError{Reason: fmt.Sprintf("reference locatorを確認できません: %v", err)}
	}
	if !info.Mode().IsRegular() {
		return "", &LocatorError{Reason: fmt.Sprintf("reference locator %qは通常fileではありません", reference)}
	}
	return resolved, nil
}

func ValidateGoTestWorkingDir(repoRoot string, workingDir string) (string, error) {
	resolvedRepo, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return "", &LocatorError{Reason: fmt.Sprintf("repository rootを解決できません: %v", err)}
	}
	if workingDir == "" || workingDir == slotUnsetValue {
		if !moduleRoot(resolvedRepo) {
			return "", &LocatorError{Reason: "repository rootにgo.modがないためworking-dir slotでmodule dirを指定してください"}
		}
		return resolvedRepo, nil
	}
	if err := validateRelativeLocator(workingDir); err != nil {
		return "", err
	}
	candidate := filepath.Join(resolvedRepo, filepath.FromSlash(workingDir))
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", &LocatorError{Reason: fmt.Sprintf("working-dir locatorを解決できません: %v", err)}
	}
	if pathOutsideRoot(resolvedRepo, resolved) {
		return "", &LocatorError{Reason: fmt.Sprintf("working-dir locator %qがrepositoryの外へ解決されました", workingDir)}
	}
	for _, segment := range strings.Split(filepath.ToSlash(workingDir), "/") {
		if segment == ".git" {
			return "", &LocatorError{Reason: "working-dir locatorが.git配下を指しています"}
		}
	}
	if !moduleRoot(resolved) {
		return "", &LocatorError{Reason: fmt.Sprintf("working-dir locator %qにgo.modがありません", workingDir)}
	}
	return resolved, nil
}

func ValidateExecutionID(executionID string) error {
	if len(executionID) < 8 || len(executionID) > 64 {
		return &LocatorError{Reason: fmt.Sprintf("execution idの長さが不正です: %q", executionID)}
	}
	for _, r := range executionID {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			continue
		}
		return &LocatorError{Reason: fmt.Sprintf("execution idに使えない文字があります: %q", executionID)}
	}
	return nil
}

func moduleRoot(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil && info.Mode().IsRegular()
}

func pathOutsideRoot(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return true
	}
	return rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
