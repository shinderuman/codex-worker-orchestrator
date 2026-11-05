package runner

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestSessionPromptTransportPreservesPayload(t *testing.T) {
	fixture := newRunnerSessionFixture(t)
	scriptPath := filepath.Join(t.TempDir(), "capture-claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$GLM_ARGS_FILE\"\ncat > \"$GLM_ARGS_FILE.stdin\"\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\"},\"result\":\"ok\"}'\n"
	if err := execfile.Write(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture.runner.config.ClaudeBin = scriptPath

	cases := []struct {
		name    string
		role    state.SessionRole
		prompt  string
		resumed bool
	}{
		{"worker-new", state.WorkerRole, "日本語\n\\\"quoted\\\" 'single' `backtick` $HOME\x00tail", false},
		{"worker-resume", state.WorkerRole, strings.Repeat("長文\n'\\\"$`\\\\", 16384) + "\x00end", true},
		{"reviewer-new", state.ReviewerRole, "Reviewer\nline2\x00end", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "output")
			result, err := fixture.runner.Run(tc.role, tc.name, "worker-model", true, "high", tc.prompt, out)
			if err != nil {
				t.Fatal(err)
			}
			if result.Resumed != tc.resumed || result.Response != "ok" {
				t.Fatalf("session result: resumed=%v response=%q", result.Resumed, result.Response)
			}
			args := readLines(t, fixture.argumentsPath)
			if bytes.Contains([]byte(strings.Join(args, "\n")), []byte(tc.prompt)) {
				t.Fatal("prompt leaked into CLI argv")
			}
			if containsArgument(args, "--resume") != tc.resumed {
				t.Fatalf("resume args: %#v", args)
			}
			got, err := os.ReadFile(fixture.argumentsPath + ".stdin")
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte(tc.prompt)) {
				t.Fatalf("stdin prompt mismatch: got %d bytes want %d", len(got), len(tc.prompt))
			}
		})
	}
}
