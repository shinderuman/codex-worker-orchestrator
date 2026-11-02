//go:build !go1.24

package parentevidence

import "fmt"

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	return "", "", nil, false, fmt.Errorf("secure diff evidence capture on Darwin requires Go 1.24 or later for %q", path)
}
