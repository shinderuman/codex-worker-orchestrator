package guardrepair

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const StrategySourcePatch = "bounded-guard-source-repair-v1"

var allowedPaths = []string{
	"glm-worker/internal/runner/git_authority_guard.go",
	"glm-worker/internal/runner/git_authority_guard_test.go",
	"glm-worker/internal/runner/git_authority_proxy.go",
	"glm-worker/internal/runner/guard_recovery.go",
	"glm-worker/internal/runner/guard_recovery_test.go",
	"glm-worker/internal/runner/instruction_surface_guard.go",
	"glm-worker/internal/runner/instruction_surface_guard_test.go",
	"glm-worker/internal/runner/instruction_surface_runner.go",
	"glm-worker/internal/workflow/guard_recovery.go",
	"glm-worker/internal/workflow/guard_recovery_test.go",
	"glm-worker/internal/workflow/guard_ref_recovery_test.go",
}

func AllowedPaths() []string {
	paths := append([]string(nil), allowedPaths...)
	sort.Strings(paths)
	return paths
}

func IsAllowed(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	for _, allowed := range allowedPaths {
		if path == allowed {
			return true
		}
	}
	return false
}

func RelevantDigest(repoRoot string) (string, error) {
	h := sha256.New()
	for _, path := range AllowedPaths() {
		content, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(h, "%s\x00<missing>\x00", path)
				continue
			}
			return "", fmt.Errorf("read guard repair scope %s: %w", path, err)
		}
		fmt.Fprintf(h, "%s\x00", path)
		h.Write(content)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func NewRecord(repoRoot, taskID, phase, failure string) (state.GuardRepairRecord, error) {
	relevantDigest, err := RelevantDigest(repoRoot)
	if err != nil {
		return state.GuardRepairRecord{}, err
	}
	failure = strings.TrimSpace(failure)
	fingerprintInput := strings.Join([]string{taskID, phase, failure, StrategySourcePatch, relevantDigest}, "\x00")
	sum := sha256.Sum256([]byte(fingerprintInput))
	return state.GuardRepairRecord{
		TaskID:         taskID,
		Phase:          phase,
		Fingerprint:    hex.EncodeToString(sum[:]),
		Strategy:       StrategySourcePatch,
		Status:         state.GuardRepairRequested,
		Failure:        failure,
		RelevantDigest: relevantDigest,
	}, nil
}
