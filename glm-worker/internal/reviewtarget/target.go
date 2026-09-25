package reviewtarget

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Parse(target string) (string, string, error) {
	target = strings.TrimSpace(target)
	separator := strings.Index(target, ":")
	if separator <= 0 || separator == len(target)-1 {
		return "", "", fmt.Errorf("review evidence target must use repository-relative path:locator form: %s", target)
	}
	path := strings.TrimSpace(target[:separator])
	locator := strings.TrimSpace(target[separator+1:])
	if path == "" || locator == "" || !relativePath(path) {
		return "", "", fmt.Errorf("review evidence target must use repository-relative path:locator form: %s", target)
	}
	if strings.ContainsAny(path, " ,()") {
		return "", "", fmt.Errorf("review evidence target path is not machine-addressable: %s", target)
	}
	return path, locator, nil
}

func relativePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}
