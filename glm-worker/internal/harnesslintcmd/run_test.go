package harnesslintcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		mode runMode
		ok   bool
	}{
		{name: "check", mode: modeCheck, ok: true},
		{name: "fix", args: []string{"--fix"}, mode: modeFix, ok: true},
		{name: "deterministic", args: []string{"--deterministic-fix"}, mode: modeDeterministicFix, ok: true},
		{name: "controlled", args: []string{"--controlled-check"}, mode: modeControlledCheck, ok: true},
		{name: "unknown", args: []string{"x"}, mode: modeCheck, ok: false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			mode, ok := parseArgs(item.args)
			if mode != item.mode || ok != item.ok {
				t.Fatalf("parseArgs(%v) = %v,%v", item.args, mode, ok)
			}
		})
	}
}

func TestControlledRootRequiresAbsolutePath(t *testing.T) {
	t.Setenv("HARNESSLINT_CONTROL_ROOT", "relative")
	if _, err := controlledRoot(); err == nil {
		t.Fatal("relative control root must fail")
	}
	absolute := filepath.Join(t.TempDir(), "control")
	t.Setenv("HARNESSLINT_CONTROL_ROOT", absolute)
	got, err := controlledRoot()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(absolute) {
		t.Fatalf("controlledRoot = %q", got)
	}
	_ = os.Unsetenv("HARNESSLINT_CONTROL_ROOT")
}
