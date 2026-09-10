package repositoryharness

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Decision struct {
	Active bool
	Reason string
}

type MarkerGuard struct {
	Exists  bool
	Regular bool
	SHA256  string
}

type QualityScopeError struct {
	Reason string
	Cause  error
}

const MarkerPath = ".glm-worker-repository-harness"

const MarkerContent = "github.com/shinderuman/codex-worker-orchestrator/repository-harness/v1\n"

const ActivationStateKey = "repository-harness"

const ActivationActiveValue = "1"

const (
	ReasonAbsent          = "absent"
	ReasonNotRegularFile  = "not-regular-file"
	ReasonContentMismatch = "content-mismatch"
	ReasonUntracked       = "untracked"
)

const (
	QualityScopeEvaluationFailed = "evaluation-failed"
	QualityScopeModuleMissing    = "module-missing"
	QualityScopeModuleMismatch   = "module-mismatch"
	QualityScopeModuleUnreadable = "module-unreadable"
)

const repositoryGoModPath = "glm-worker/go.mod"

const repositoryModuleLine = "module github.com/shinderuman/codex-worker-orchestrator/glm-worker"

func (e *QualityScopeError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("repository quality scope invalid (%s): %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("repository quality scope invalid (%s)", e.Reason)
}

func (e *QualityScopeError) Unwrap() error {
	return e.Cause
}

func Evaluate(root string) (Decision, error) {
	if root == "" {
		return Decision{Reason: ReasonAbsent}, nil
	}
	marker := filepath.Join(root, MarkerPath)
	info, err := os.Lstat(marker)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Decision{Reason: ReasonAbsent}, nil
		}
		return Decision{}, fmt.Errorf("stat %s: %w", MarkerPath, err)
	}
	if !info.Mode().IsRegular() {
		return Decision{Reason: ReasonNotRegularFile}, nil
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		return Decision{}, fmt.Errorf("read %s: %w", MarkerPath, err)
	}
	if string(content) != MarkerContent {
		return Decision{Reason: ReasonContentMismatch}, nil
	}
	tracked, err := markerTracked(root)
	if err != nil {
		return Decision{}, err
	}
	if !tracked {
		return Decision{Reason: ReasonUntracked}, nil
	}
	return Decision{Active: true}, nil
}

func markerTracked(root string) (bool, error) {
	inside, err := gitWorktreePresent(root)
	if err != nil {
		return false, err
	}
	if !inside {
		return false, nil
	}
	output, err := exec.Command("git", "-C", root, "ls-files", "--", MarkerPath).Output()
	if err != nil {
		return false, fmt.Errorf("git ls-files: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func gitWorktreePresent(root string) (bool, error) {
	for dir := root; ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return true, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("stat %s: %w", filepath.Join(dir, ".git"), err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false, nil
		}
	}
}

func QualityToolsApply(root string) (bool, error) {
	decision, err := Evaluate(root)
	if err != nil {
		return false, &QualityScopeError{Reason: QualityScopeEvaluationFailed, Cause: err}
	}
	if !decision.Active {
		return false, nil
	}
	if err := validateRepositoryModule(root); err != nil {
		return false, err
	}
	return true, nil
}

func validateRepositoryModule(root string) error {
	goMod := filepath.Join(root, filepath.FromSlash(repositoryGoModPath))
	data, err := os.ReadFile(goMod)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &QualityScopeError{Reason: QualityScopeModuleMissing}
		}
		return &QualityScopeError{Reason: QualityScopeModuleUnreadable, Cause: fmt.Errorf("read %s: %w", repositoryGoModPath, err)}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == repositoryModuleLine {
			return nil
		}
	}
	return &QualityScopeError{Reason: QualityScopeModuleMismatch}
}

func CaptureMarker(root string) (MarkerGuard, error) {
	if root == "" {
		return MarkerGuard{}, nil
	}
	marker := filepath.Join(root, MarkerPath)
	info, err := os.Lstat(marker)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return MarkerGuard{}, nil
		}
		return MarkerGuard{}, fmt.Errorf("stat %s: %w", MarkerPath, err)
	}
	if !info.Mode().IsRegular() {
		return MarkerGuard{Exists: true}, nil
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		return MarkerGuard{}, fmt.Errorf("read %s: %w", MarkerPath, err)
	}
	sum := sha256.Sum256(content)
	return MarkerGuard{Exists: true, Regular: true, SHA256: hex.EncodeToString(sum[:])}, nil
}

func SameMarkerGuard(a, b MarkerGuard) bool {
	return a == b
}
