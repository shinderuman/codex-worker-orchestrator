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

func TestNewWorkerTaskPromptSearchesLauncherFromActiveTaskSeed(t *testing.T) {
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

	cfg := config.AppConfig{RepoRoot: root, RepoHash: "active-task-search-launcher", StateBase: t.TempDir(), RepoSearch: true}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	w := NewWorkflow(cfg, st, nil, nil)
	request := "現在のACTIVE taskを実行してください。"
	w.repoSearch = func(_ context.Context, _ string, query string, _ reposearch.Options) (reposearch.Report, error) {
		for _, want := range []string{"commentlint launcher", "launcher target", "commentlint"} {
			if !strings.Contains(query, want) {
				t.Fatalf("ACTIVE task seed lacks %q: %q", want, query)
			}
		}
		if strings.Contains(query, request) {
			t.Fatalf("fixed parent transport leaked into search query: %q", query)
		}
		return reposearch.Report{Results: []reposearch.Result{{Path: "commentlint", Line: 1}}}, nil
	}

	prompt := w.newWorkerTaskPrompt(request, activeTaskPath)
	if !strings.Contains(prompt, request) || !strings.Contains(prompt, "CANDIDATE: commentlint:1") {
		t.Fatalf("prompt = %s", prompt)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.RepoSearchOutcomes[repoSearchHit] != 1 || stats.RepoSearchOutcomes[repoSearchKnownSkip] != 0 {
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
	target := filepath.Join(root, "glm-worker", "internal", "workflow", "worker_repo_search.go")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("package workflow\n"), 0o600); err != nil {
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
	request := "現在のACTIVE taskを実行してください。"
	w.repoSearch = func(_ context.Context, _ string, query string, _ reposearch.Options) (reposearch.Report, error) {
		for _, want := range []string{"worker search routing", "ACTIVE_AUTHORITY_TOKEN", "glm-worker/internal/workflow/worker_repo_search.go"} {
			if !strings.Contains(query, want) {
				t.Fatalf("ACTIVE task seed lacks %q: %q", want, query)
			}
		}
		if strings.Contains(query, request) || strings.Contains(query, "NOISE_TOKEN") || strings.Contains(query, "CONTRACT_NOISE") {
			t.Fatalf("unrelated transport/task prose leaked into search query: %q", query)
		}
		return reposearch.Report{Results: []reposearch.Result{{Path: "glm-worker/internal/workflow/worker_repo_search.go", Line: 1}}}, nil
	}

	prompt := w.newWorkerTaskPrompt(request, activeTaskPath)
	if !strings.Contains(prompt, "CANDIDATE: glm-worker/internal/workflow/worker_repo_search.go:1") {
		t.Fatalf("navigation missing: %s", prompt)
	}
}
