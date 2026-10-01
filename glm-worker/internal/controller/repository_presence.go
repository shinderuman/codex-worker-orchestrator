package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func RepositoryPresent(repoRoot string) (bool, error) {
	if repoRoot == "" {
		return false, nil
	}
	current, err := filepath.Abs(repoRoot)
	if err != nil {
		return false, fmt.Errorf("resolve repository applicability root: %w", err)
	}
	for {
		_, err := os.Lstat(filepath.Join(current, ".git"))
		switch {
		case err == nil:
			return true, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return false, fmt.Errorf("inspect repository applicability marker: %w", err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}
