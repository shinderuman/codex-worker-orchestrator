package state

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/gitexec"
	"strings"
)

type GitAuthorityRef struct {
	Name     string
	ObjectID string
}

func CaptureGitAuthorityRefs(gitPath, repoRoot string) ([]GitAuthorityRef, error) {
	authority, err := ResolveGitHeadAuthority(gitPath, repoRoot)
	if err != nil {
		return nil, err
	}
	if authority.SymbolicHead == "" || authority.Unborn {
		return nil, nil
	}
	output, err := gitexec.Command(gitPath, repoRoot, "rev-parse", "--verify", authority.SymbolicHead).Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse %s: %w", authority.SymbolicHead, err)
	}
	objectID := strings.TrimSpace(string(output))
	if objectID == "" {
		return nil, fmt.Errorf("git rev-parse %s returned empty object", authority.SymbolicHead)
	}
	return []GitAuthorityRef{{Name: authority.SymbolicHead, ObjectID: objectID}}, nil
}

func DigestGitAuthorityRefs(refs []GitAuthorityRef) string {
	hash := sha256.New()
	for _, ref := range refs {
		hash.Write([]byte(ref.Name))
		hash.Write([]byte{0})
		hash.Write([]byte(ref.ObjectID))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}
