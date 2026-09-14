//go:build unix

package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeadcodeVersionUsesBinaryModuleMetadata(t *testing.T) {
	binDir := t.TempDir()
	deadcodePath := filepath.Join(binDir, "codex-worker-orchestrator-deadcode-9.9.9")
	if err := os.WriteFile(deadcodePath, []byte("not executed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	goPath := filepath.Join(binDir, "go")
	goStub := `#!/bin/sh
printf '%s: go1.25.4\n' "$3"
printf '\tmod\tgolang.org/x/tools\tv0.49.0\n'
`
	if err := os.WriteFile(goPath, []byte(goStub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	runner := realCommandRunner{
		goToolchain:  "local",
		goCache:      t.TempDir(),
		deadcodePath: deadcodePath,
	}
	result, err := runner.runVersion(t.TempDir(), deadcodeToolName)
	if err != nil {
		t.Fatal(err)
	}
	if result.exitCode != 0 {
		t.Fatalf("deadcode version command exited %d: %s", result.exitCode, result.output)
	}
	if got := observedQualityToolVersion(deadcodeToolName, result.output); got != "0.49.0" {
		t.Fatalf("deadcode version came from filename instead of module metadata: got %q output=%q", got, result.output)
	}
}
