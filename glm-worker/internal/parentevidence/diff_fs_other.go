//go:build !linux && !darwin && !windows

package parentevidence

import "fmt"

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	return "", "", nil, false, fmt.Errorf("secure diff evidence capture is unsupported on this platform for %q", path)
}
