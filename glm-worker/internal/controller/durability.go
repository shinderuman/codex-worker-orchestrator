package controller

import (
	"fmt"
	"os"
)

func syncDirectoryPath(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory for durability sync %s: %w", path, err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}
