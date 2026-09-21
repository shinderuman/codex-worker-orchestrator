package harnesslint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalAuthorityBootstrapPrecedesRepositoryReadsAndRestoresWaitContract(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("test source path is unavailable")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))

	agentsData, err := os.ReadFile(filepath.Join(root, "codex", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	workScope, ok := markdownSection(string(agentsData), "## 2. 作業範囲")
	if !ok {
		t.Fatal("codex/AGENTS.md missing work-scope section")
	}
	for _, token := range []string{
		".glm-worker-repository-harness",
		"glm-worker --authority bootstrap",
		"最初のrepository authority read",
		"ContextCompaction",
		"provider/session interruption",
		"long stop/resume",
		"authority_snapshot_sha256",
		"conversation/compaction memoryだけで続行しない",
		"fallback successとして扱わない",
		"glm-execution.md",
		"long-blocking parent wait contract",
		"short-yield override",
	} {
		if !strings.Contains(workScope, token) {
			t.Errorf("work-scope bootstrap contract missing %q", token)
		}
	}
	for _, legacy := range []string{
		"glm-worker --authority rules",
		"glm-worker --authority plan",
		"glm-worker --authority active",
	} {
		if strings.Contains(workScope, legacy) {
			t.Errorf("work-scope contract still reconstructs bootstrap with %q", legacy)
		}
	}

	executionData, err := os.ReadFile(filepath.Join(root, "codex", "instructions", "glm-execution.md"))
	if err != nil {
		t.Fatal(err)
	}
	waitSection, ok := markdownSection(string(executionData), "## 待機")
	if !ok {
		t.Fatal("glm-execution.md missing wait section")
	}
	for _, token := range []string{
		"yield-time_ms\":21600000",
		"background_terminal_max_timeout=21600000",
		"tools.write_stdin",
	} {
		if !strings.Contains(waitSection, token) {
			t.Errorf("restored wait contract missing %q", token)
		}
	}
	for _, short := range []string{"yield-time_ms=30000", "yield-time_ms=60000", "yield-time_ms\":30000", "yield-time_ms\":60000"} {
		if strings.Contains(waitSection, short) {
			t.Errorf("wait contract permits explicit short-yield override %q", short)
		}
	}
}
