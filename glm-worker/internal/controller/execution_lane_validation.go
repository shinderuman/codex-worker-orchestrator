package controller

import (
	"fmt"
	"path/filepath"
)

func (s *Store) validateExecutionLaneLocation(workspace WorkspaceIdentity) error {
	base, err := canonicalPath(s.dir)
	if err != nil {
		return err
	}
	name := s.identity.LineageID[:16] + "-lane"
	if workspace.RepositoryID != s.identity.LineageID || workspace.Root != filepath.Join(base, name) || workspace.GitDir != filepath.Join(s.identity.CommonDir, "worktrees", name) {
		return fmt.Errorf("execution lane is outside controller ownership")
	}
	return nil
}
