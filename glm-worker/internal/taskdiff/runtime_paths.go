package taskdiff

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func SelectedPaths(paths []string, include func(string) bool) []string {
	selected := make([]string, 0, len(paths))
	for _, path := range paths {
		if include(path) {
			selected = append(selected, filepath.ToSlash(filepath.Clean(path)))
		}
	}
	sort.Strings(selected)
	return selected
}

func SourceDigest(repoRoot string, paths []string) (string, error) {
	hash := sha256.New()
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(path)))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				_, _ = fmt.Fprintf(hash, "%s\x00missing\n", path)
				continue
			}
			return "", fmt.Errorf("runtime source %s: %w", path, err)
		}
		content := sha256.Sum256(data)
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", path, hex.EncodeToString(content[:]))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
