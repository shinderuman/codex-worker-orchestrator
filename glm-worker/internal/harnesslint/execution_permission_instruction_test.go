package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func executionPermissionRepoRoot(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
}

func readExecutionPermissionFile(t *testing.T, segments ...string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(executionPermissionRepoRoot(t), filepath.Join(segments...)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestExecutionPermissionContractWiring(t *testing.T) {
	contract := readExecutionPermissionFile(t, "codex", "instructions", "execution-permission.md")
	for _, token := range []string{
		"execution deniedだけでawait-user",
		"permission-relevant state差分",
		"再要求には前回成立後のpermission-relevant state change証拠",
		"rule fileはhost-localでありsession rotationで失われない",
		"permission grant・permission denial・新task指示・停止指示へ変換せず",
		"glm-parent-action install",
		"awaiting-parent-completion中のみ",
		".glm-worker-repository-harness",
		"install_guard_rejected",
		"user denial・permission不足・await-user・task terminalへ変換せず",
	} {
		if !strings.Contains(contract, token) {
			t.Errorf("execution-permission.md does not carry contract token %q", token)
		}
	}

	routing := readExecutionPermissionFile(t, "codex", "AGENTS.md")
	if !strings.Contains(routing, "execution-permission.md") {
		t.Error("codex/AGENTS.md does not route execution-permission.md")
	}

	lifecycle := readExecutionPermissionFile(t, "codex", "instructions", "task-lifecycle.md")
	if !strings.Contains(lifecycle, "execution-permission.md") {
		t.Error("task-lifecycle.md stop conditions do not route execution-permission.md")
	}

	smoke := readExecutionPermissionFile(t, "tests", "install_smoke.sh")
	for _, token := range []string{
		"codex/instructions/execution-permission.md",
	} {
		if !strings.Contains(smoke, token) {
			t.Errorf("install_smoke.sh does not verify deployment wiring for %q", token)
		}
	}
}

func TestExecutionPermissionContractKeepsStateSeparation(t *testing.T) {
	contract := readExecutionPermissionFile(t, "codex", "instructions", "execution-permission.md")
	separation, ok := markdownSection(contract, "## state分離")
	if !ok {
		t.Fatal("execution-permission.md missing state separation section")
	}
	for _, token := range []string{
		"task authority",
		"execution permission",
		"execution result",
		"user denial・user authority欠如へ変換しない",
	} {
		if !strings.Contains(separation, token) {
			t.Errorf("state separation section missing %q", token)
		}
	}
	for _, forbidden := range []string{
		"execution deniedだけでawait-user・親USER_REQUEST完了・task terminalへ自動遷移する",
	} {
		if strings.Contains(separation, forbidden) {
			t.Errorf("state separation section must not allow %q", forbidden)
		}
	}
}
