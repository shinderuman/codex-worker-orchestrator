package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestSaveCurrentTaskAuthorityKeepsLatestTaskContract(t *testing.T) {
	root := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  filepath.Join(root, "repo"),
		RepoHash:  strings.Repeat("a", 64),
		StateBase: filepath.Join(root, "state"),
	}
	if err := os.MkdirAll(cfg.RepoRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/014.md", []byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCurrentTaskAuthority("IMPLEMENTATION_TASKS/014.md", []byte("second\n")); err != nil {
		t.Fatal(err)
	}
	path, err := os.ReadFile(st.TaskAuthorityPathPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(st.TaskAuthorityContentPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if string(path) != "IMPLEMENTATION_TASKS/014.md\n" || string(content) != "second\n" {
		t.Fatalf("authority snapshot = path:%q content:%q", path, content)
	}
}
