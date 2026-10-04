//go:build darwin

package observationexec

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfinementProfileBoundsHostCredentialReads(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "tester")
	writeRoot := filepath.Join(string(filepath.Separator), "private", "tmp", "observation")
	profile := confinementProfile(writeRoot, home)
	for _, required := range []string{
		"(deny file-write*)",
		"(deny network*)",
		"(deny file-read* (subpath \"" + filepath.Join(home, ".ssh") + "\"))",
		"(deny file-read* (subpath \"" + filepath.Join(home, ".aws") + "\"))",
		"(deny file-read* (subpath \"" + filepath.Join(home, "Library", "Keychains") + "\"))",
		"(deny file-read* (literal \"" + filepath.Join(home, ".netrc") + "\"))",
		"(allow file-write* (subpath \"" + writeRoot + "\"))",
	} {
		if !strings.Contains(profile, required) {
			t.Fatalf("confinement profile missing %q:\n%s", required, profile)
		}
	}
	if strings.Contains(profile, "(allow default)") {
		t.Fatalf("confinement profile must not replace deliberate policy with allow default:\n%s", profile)
	}
}
