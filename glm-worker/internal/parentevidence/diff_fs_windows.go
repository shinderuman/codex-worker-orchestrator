package parentevidence

import "fmt"

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	return "", "", nil, false, fmt.Errorf("secure diff evidence capture is unsupported on Windows for %q", path)
}
