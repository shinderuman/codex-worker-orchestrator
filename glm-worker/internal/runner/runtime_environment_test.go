package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRunRecordsRuntimeEnvironmentPerCall(t *testing.T) {
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	claudeConfigDir := t.TempDir()
	settings := []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"secret-value","ANTHROPIC_BASE_URL":"https://example.internal"}}`)
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"), settings, 0o600); err != nil {
		t.Fatal(err)
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: claudeConfigDir,
	}, st)
	r.instructionSurfaceDigest = "instruction-digest"

	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := result.Runtime
	if runtime == nil {
		t.Fatal("runtime environmentが記録されていません")
	}
	if runtime.ClaudeBinResolved == "" || !strings.HasSuffix(runtime.ClaudeBinResolved, "fake-claude") {
		t.Fatalf("claude bin resolved = %q", runtime.ClaudeBinResolved)
	}
	if runtime.ClaudeBinBytes != int64(len(script)) || runtime.ClaudeBinModifiedAt == "" {
		t.Fatalf("claude bin identity = %d / %q", runtime.ClaudeBinBytes, runtime.ClaudeBinModifiedAt)
	}
	if runtime.InstructionSurfaceDigest != "instruction-digest" {
		t.Fatalf("instruction surface digest = %q", runtime.InstructionSurfaceDigest)
	}
	if len(runtime.IsolationSettingsDigest) != 64 {
		t.Fatalf("isolation settings digest = %q", runtime.IsolationSettingsDigest)
	}
	if strings.Join(runtime.SettingEnvKeys, ",") != "ANTHROPIC_AUTH_TOKEN,ANTHROPIC_BASE_URL" {
		t.Fatalf("setting env keys = %v", runtime.SettingEnvKeys)
	}
	if strings.Contains(strings.Join(runtime.SettingEnvKeys, "\n"), "secret-value") ||
		strings.Contains(runtime.IsolationSettingsDigest, "secret") {
		t.Fatal("setting valueがruntime環境記録へ漏れています")
	}
	if runtime.EnvironmentObservedAt == "" {
		t.Fatalf("observed at = %q", runtime.EnvironmentObservedAt)
	}
	if runtime.ClaudeVersion != "" || runtime.ClaudeVersionSource != claudeVersionSourceUnknown {
		t.Fatalf("transcriptがない場合のversion = %q/%q", runtime.ClaudeVersion, runtime.ClaudeVersionSource)
	}
}

func TestRunObservesClaudeVersionFromSessionTranscript(t *testing.T) {
	t.Setenv("GLM_FAKE_VERSION", "2.1.226")
	claudeConfigDir, st, r := newRuntimeEnvironmentFixture(t, `{"env":{"ANTHROPIC_AUTH_TOKEN":"secret"}}`)
	sessionID := seedRuntimeSession(t, st)

	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtime.ClaudeVersion != "2.1.226" || result.Runtime.ClaudeVersionSource != claudeVersionSourceTranscript {
		t.Fatalf("version = %q/%q", result.Runtime.ClaudeVersion, result.Runtime.ClaudeVersionSource)
	}
	if result.Runtime.ClaudeVersionObservedAt == "" || result.Runtime.EnvironmentObservedAt == "" {
		t.Fatalf("観測時点 = env:%q version:%q", result.Runtime.EnvironmentObservedAt, result.Runtime.ClaudeVersionObservedAt)
	}
	if _, err := os.Stat(filepath.Join(claudeConfigDir, "projects", "proj-a", sessionID+".jsonl")); err != nil {
		t.Fatalf("fake CLIがtranscriptを書いていません: %v", err)
	}
}

func TestRunClaudeVersionUnknownWhenCallAppendsNoRecords(t *testing.T) {
	_, st, r := newRuntimeEnvironmentFixture(t, `{"env":{"ANTHROPIC_AUTH_TOKEN":"secret"}}`)
	sessionID := seedRuntimeSession(t, st)
	seedWorkerReady(t, st)
	writeRuntimeTranscriptAt(t, transcriptProjectDir(t, r), sessionID,
		"{\"type\":\"user\",\"version\":\"2.0.0-old\"}\n")

	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtime.ClaudeVersion != "" || result.Runtime.ClaudeVersionSource != claudeVersionSourceUnknown {
		t.Fatalf("旧記録のみで追記なしはunknown: %q/%q", result.Runtime.ClaudeVersion, result.Runtime.ClaudeVersionSource)
	}
}

func TestRunClaudeVersionUsesOnlyRecordsAppendedByThisCall(t *testing.T) {
	t.Setenv("GLM_FAKE_VERSION", "2.1.226")
	_, st, r := newRuntimeEnvironmentFixture(t, `{"env":{"ANTHROPIC_AUTH_TOKEN":"secret"}}`)
	sessionID := seedRuntimeSession(t, st)
	seedWorkerReady(t, st)
	writeRuntimeTranscriptAt(t, transcriptProjectDir(t, r), sessionID,
		"{\"type\":\"user\",\"version\":\"2.1.0\"}\n")

	result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "worker.log"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtime.ClaudeVersion != "2.1.226" || result.Runtime.ClaudeVersionSource != claudeVersionSourceTranscript {
		t.Fatalf("今回追記されたrecordのみを採用すべき: %q/%q", result.Runtime.ClaudeVersion, result.Runtime.ClaudeVersionSource)
	}
}

func TestRunClaudeVersionAttributionBoundariesStayUnknown(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{name: "ambiguous-candidates", env: map[string]string{"GLM_FAKE_VERSION": "2.1.226"}},
		{name: "trailing-fragment-only", env: map[string]string{"GLM_FAKE_MODE": "fragment"}},
		{name: "replaced-file", env: map[string]string{"GLM_FAKE_MODE": "replace", "GLM_FAKE_VERSION": "9.9.9"}},
		{name: "swapped-file-same-prefix", env: map[string]string{"GLM_FAKE_MODE": "swap"}},
		{name: "shrunken-file", env: map[string]string{"GLM_FAKE_MODE": "shrink"}},
		{name: "version-outside-observation-window", env: map[string]string{"GLM_FAKE_VERSION": "3.0.0", "GLM_FAKE_PAD": "40000"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			_, st, r := newRuntimeEnvironmentFixture(t, `{"env":{"ANTHROPIC_AUTH_TOKEN":"secret"}}`)
			sessionID := seedRuntimeSession(t, st)
			seedWorkerReady(t, st)
			dir := transcriptProjectDir(t, r)
			writeRuntimeTranscriptAt(t, dir, sessionID, "{\"type\":\"user\",\"version\":\"2.0.0-old\"}\n")
			if tc.name == "ambiguous-candidates" {
				writeRuntimeTranscriptAt(t, filepath.Join(filepath.Dir(dir), "proj-b"), sessionID,
					"{\"version\":\"5.0.0\"}\n")
			}

			result, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
				filepath.Join(t.TempDir(), "worker.log"))
			if err != nil {
				t.Fatal(err)
			}
			if result.Runtime.ClaudeVersion != "" || result.Runtime.ClaudeVersionSource != claudeVersionSourceUnknown {
				t.Fatalf("帰属不能境界はunknownであるべき: %q/%q", result.Runtime.ClaudeVersion, result.Runtime.ClaudeVersionSource)
			}
		})
	}
}

func TestRunObservesConditionsPerCallNotAsBundleTimeSnapshot(t *testing.T) {
	t.Setenv("GLM_FAKE_VERSION", "2.1.0")
	claudeConfigDir, st, r := newRuntimeEnvironmentFixture(t, `{"env":{"ANTHROPIC_AUTH_TOKEN":"secret"}}`)
	seedRuntimeSession(t, st)

	first, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "first.log"))
	if err != nil {
		t.Fatal(err)
	}

	commandPath := first.Runtime.ClaudeBinResolved
	replacement := "#!/bin/sh\n# binary rebuilt mid-task with different content length\n" + fakeClaudeScriptBody()
	if err := os.WriteFile(commandPath, []byte(replacement), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"secret","ANTHROPIC_BASE_URL":"https://changed.internal"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GLM_FAKE_VERSION", "2.1.226")

	second, err := r.Run(state.WorkerRole, "worker-new", "worker-model", false, "high", "prompt",
		filepath.Join(t.TempDir(), "second.log"))
	if err != nil {
		t.Fatal(err)
	}

	if first.Runtime.ClaudeVersion != "2.1.0" || second.Runtime.ClaudeVersion != "2.1.226" {
		t.Fatalf("version観測 = %q / %q", first.Runtime.ClaudeVersion, second.Runtime.ClaudeVersion)
	}
	if first.Runtime.ClaudeVersionSource != claudeVersionSourceTranscript ||
		second.Runtime.ClaudeVersionSource != claudeVersionSourceTranscript {
		t.Fatalf("version source = %q / %q", first.Runtime.ClaudeVersionSource, second.Runtime.ClaudeVersionSource)
	}
	if first.Runtime.ClaudeBinBytes == second.Runtime.ClaudeBinBytes {
		t.Fatalf("binary変更が観測されていません: %d / %d", first.Runtime.ClaudeBinBytes, second.Runtime.ClaudeBinBytes)
	}
	if strings.Join(first.Runtime.SettingEnvKeys, ",") != "ANTHROPIC_AUTH_TOKEN" ||
		strings.Join(second.Runtime.SettingEnvKeys, ",") != "ANTHROPIC_AUTH_TOKEN,ANTHROPIC_BASE_URL" {
		t.Fatalf("設定変更が観測されていません: %v / %v", first.Runtime.SettingEnvKeys, second.Runtime.SettingEnvKeys)
	}
}

func writeRuntimeTranscriptAt(t *testing.T, dir, sessionID, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".jsonl"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func transcriptProjectDir(t *testing.T, r *ClaudeRunner) string {
	t.Helper()
	return filepath.Join(r.config.ClaudeConfigDir, "projects", "proj-a")
}

func seedWorkerReady(t *testing.T, st *state.StateStore) {
	t.Helper()
	if err := st.MarkReady(state.WorkerRole); err != nil {
		t.Fatal(err)
	}
}

func seedRuntimeSession(t *testing.T, st *state.StateStore) string {
	t.Helper()
	sessionID, _, err := st.SessionID(state.WorkerRole)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetIsolationPolicy(isolationPolicyVersion); err != nil {
		t.Fatal(err)
	}
	return sessionID
}

func fakeClaudeScriptBody() string {
	transcript := `
sid=""
prev=""
for a in "$@"; do
  if [ "$prev" = "--session-id" ] || [ "$prev" = "--resume" ]; then sid=$a; fi
  prev=$a
done
if [ -n "$sid" ]; then
  dir="$CLAUDE_CONFIG_DIR/projects/proj-a"
  mkdir -p "$dir"
  f="$dir/$sid.jsonl"
  if [ "$GLM_FAKE_MODE" = "replace" ]; then
    printf '%s\n' '{"rewritten":"prefix"}' > "$f"
  fi
  if [ "$GLM_FAKE_MODE" = "shrink" ]; then
    printf '%s\n' '{"short":"file"}' > "$f"
  fi
  if [ "$GLM_FAKE_MODE" = "swap" ]; then
    cp "$f" "$f.swap"
    printf '%s\n' '{"type":"user","version":"7.7.7-swap"}' >> "$f.swap"
    mv "$f.swap" "$f"
  fi
  if [ -n "$GLM_FAKE_VERSION" ]; then
    printf '%s\n' "{\"type\":\"user\",\"version\":\"$GLM_FAKE_VERSION\"}" >> "$f"
  fi
  if [ "$GLM_FAKE_MODE" = "fragment" ]; then
    printf '%s' '{"version":"9.9.9-truncated' >> "$f"
  fi
  if [ -n "$GLM_FAKE_PAD" ]; then
    awk -v n="$GLM_FAKE_PAD" 'BEGIN{s="";for(i=0;i<n;i++)s=s "x";printf "{\"pad\":\"%s\"}\n", s}' >> "$f"
  fi
fi
`
	return transcript + "printf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
}

func newRuntimeEnvironmentFixture(t *testing.T, settingsJSON string) (string, *state.StateStore, *ClaudeRunner) {
	t.Helper()
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	claudeConfigDir := t.TempDir()
	if settingsJSON != "" {
		if err := os.WriteFile(filepath.Join(claudeConfigDir, "settings.json"), []byte(settingsJSON), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(commandPath, []byte("#!/bin/sh\n"+fakeClaudeScriptBody()), 0o700); err != nil {
		t.Fatal(err)
	}
	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: claudeConfigDir,
		EnvAllowlist:    []string{"GLM_FAKE_VERSION", "GLM_FAKE_MODE", "GLM_FAKE_PAD"},
	}, st)
	return claudeConfigDir, st, r
}

func TestRunRecordsRuntimeEnvironmentWithoutSettings(t *testing.T) {
	promptDir := t.TempDir()
	for _, name := range []string{"WORKER.md", "REVIEWER.md"} {
		if err := os.WriteFile(filepath.Join(promptDir, name), []byte("system"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	commandPath := filepath.Join(t.TempDir(), "fake-claude")
	script := "#!/bin/sh\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"structured_output\":{\"status\":\"IMPLEMENTED\",\"risk\":\"LOW\",\"summary\":\"done\",\"requirement_coverage\":\"covered\",\"tests\":\"pass\",\"unverified\":\"none\"},\"result\":\"ok\\n\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}'\n"
	if err := os.WriteFile(commandPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	st := newTestStateStore(t)
	if err := st.Write("task.id", "12345678-aaaa-bbbb-cccc-dddddddddddd"); err != nil {
		t.Fatal(err)
	}
	r := NewClaudeRunner(config.AppConfig{
		RepoRoot:        t.TempDir(),
		PromptDir:       promptDir,
		ClaudeBin:       commandPath,
		ClaudeConfigDir: t.TempDir(),
	}, st)

	result, err := r.Run(state.ReviewerRole, "reviewer-1", "reviewer-model", true, "high", "prompt",
		filepath.Join(t.TempDir(), "reviewer.log"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Runtime == nil || len(result.Runtime.SettingEnvKeys) != 0 {
		t.Fatalf("runtime = %#v", result.Runtime)
	}
}
