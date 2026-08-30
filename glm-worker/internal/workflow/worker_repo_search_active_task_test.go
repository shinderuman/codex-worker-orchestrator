package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/reposearch"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestNewWorkerTaskPromptUsesActiveTaskSeedForKnownTargetSkip(t *testing.T) {
	root := t.TempDir()
	activeTaskPath := filepath.ToSlash(filepath.Join(state.ParentTasksDir, "commentlint-launcher.md"))
	if err := os.MkdirAll(filepath.Join(root, state.ParentTasksDir), 0o700); err != nil {
		t.Fatal(err)
	}
	task := strings.Join([]string{
		"# commentlint launcher",
		"## Purpose",
		"launcher target",
		"## Resolved references",
		"- commentlint",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, activeTaskPath), []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "commentlint"), []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig{RepoRoot: root, RepoHash: "active-task-search-skip", StateBase: t.TempDir(), RepoSearch: true}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, nil)
	w.repoSearch = func(context.Context, string, string, reposearch.Options) (reposearch.Report, error) {
		t.Fatal("ACTIVE taskの既知targetでsearchが実行されました")
		return reposearch.Report{}, nil
	}

	prompt := w.newWorkerTaskPrompt("現在のACTIVE taskを実行してください。", activeTaskPath)
	if !strings.Contains(prompt, "USER_REQUEST:\n現在のACTIVE taskを実行してください。") {
		t.Fatalf("fixed parent transport changed: %s", prompt)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.RepoSearchOutcomes[repoSearchKnownSkip] != 1 {
		t.Fatalf("repo search outcomes = %+v", stats.RepoSearchOutcomes)
	}
}

func TestNewWorkerTaskPromptSearchesFromActiveTaskAuthority(t *testing.T) {
	root := t.TempDir()
	activeTaskPath := filepath.ToSlash(filepath.Join(state.ParentTasksDir, "worker-search-routing.md"))
	if err := os.MkdirAll(filepath.Join(root, state.ParentTasksDir), 0o700); err != nil {
		t.Fatal(err)
	}
	task := strings.Join([]string{
		"# worker search routing",
		"## Original instruction",
		"NOISE_TOKEN",
		"## Purpose",
		"ACTIVE_AUTHORITY_TOKEN",
		"## Resolved references",
		"- glm-worker/internal/workflow/worker_repo_search.go",
		"## Contract",
		"CONTRACT_NOISE",
	}, "\n")
	if err := os.WriteFile(filepath.Join(root, activeTaskPath), []byte(task), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.AppConfig{RepoRoot: root, RepoHash: "active-task-search-query", StateBase: t.TempDir(), RepoSearch: true}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, nil)
	w.repoSearch = func(_ context.Context, _ string, query string, _ reposearch.Options) (reposearch.Report, error) {
		for _, want := range []string{"worker search routing", "ACTIVE_AUTHORITY_TOKEN", "glm-worker/internal/workflow/worker_repo_search.go"} {
			if !strings.Contains(query, want) {
				t.Fatalf("ACTIVE task seed lacks %q: %q", want, query)
			}
		}
		if strings.Contains(query, "現在のACTIVE taskを実行してください。") || strings.Contains(query, "NOISE_TOKEN") || strings.Contains(query, "CONTRACT_NOISE") {
			t.Fatalf("unrelated transport/task prose leaked into search query: %q", query)
		}
		return reposearch.Report{Results: []reposearch.Result{{Path: "glm-worker/internal/workflow/worker_repo_search.go", Line: 1}}}, nil
	}

	prompt := w.newWorkerTaskPrompt("現在のACTIVE taskを実行してください。", activeTaskPath)
	if !strings.Contains(prompt, "CANDIDATE: glm-worker/internal/workflow/worker_repo_search.go:1") {
		t.Fatalf("navigation missing: %s", prompt)
	}
}
