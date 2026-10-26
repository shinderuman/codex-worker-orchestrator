//go:build darwin

package observationexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfinementProfileBoundsHostCredentialReads(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "tester")
	writeRoot := filepath.Join(string(filepath.Separator), "private", "tmp", "observation")
	profile := confinementProfile(writeRoot, home)
	for _, required := range []string{
		"(allow process*)",
		"(allow file-read*)",
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

func TestConfinementRuntimeDeniesCredentialReads(t *testing.T) {
	home := t.TempDir()
	writeRoot := t.TempDir()
	for _, name := range []string{".ssh/id_fixture", ".netrc", "ordinary.txt"} {
		path := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	writeRoot, err = filepath.EvalSymlinks(writeRoot)
	if err != nil {
		t.Fatal(err)
	}
	script := `/bin/cat "$1/ordinary.txt" >/dev/null || exit 1
if /bin/cat "$1/.ssh/id_fixture" >/dev/null 2>&1; then exit 2; fi
if /bin/cat "$1/.netrc" >/dev/null 2>&1; then exit 3; fi`
	command := exec.Command(sandboxExecBin, "-p", confinementProfile(writeRoot, home), "/bin/sh", "-c", script, "credential-read-probe", home)
	if data, err := command.CombinedOutput(); err != nil {
		t.Fatalf("credential confinement: %v %s", err, data)
	}
}
