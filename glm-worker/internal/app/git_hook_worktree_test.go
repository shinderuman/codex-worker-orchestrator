package app

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func metadataHookLiveLane(t *testing.T, alternate string) (config.AppConfig, string, string, []string) {
	t.Helper()
	cfg, task, other, _ := newCanonicalWorkflowCorpus(t)
	root := runCanonicalWorkflow(t, cfg, ModeNewTask, "metadata hook root", task, "")
	store, err := controller.Open(root.cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := canonicalWorkflowAdmission(t, root.cfg, store)
	episode := planCanonicalWorkflowBlocker(t, store, source, other)
	suspended, err := store.SuspendExecution(source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	lane, err := store.MaterializeExecution(controller.MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	laneCfg := cfg
	laneCfg.RepoRoot = lane.Admission.Workspace.Root
	if alternate != "unpinned" {
		decision := &fakeRunner{steps: []fakeStep{{structured: needsSolDecisionPacketApp()}}}
		if err := Execute(Command{Mode: ModeNewTask, Payload: "metadata hook lane"}, laneCfg, decision.factory(), io.Discard, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	other = task
	installCompletionGuard(t, cfg)
	hooks := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "config", "--get", "core.hooksPath")))
	hook, err := os.ReadFile(filepath.Join("..", "..", "..", ".githooks", "pre-commit"))
	if err != nil {
		t.Fatal(err)
	}
	if err := execfile.Write(filepath.Join(hooks, "pre-commit"), hook, 0755); err != nil {
		t.Fatal(err)
	}
	shim := writeRetirementCLIShimDir(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nGLM_PARENT_ACTION_TEST_PROCESS=1 exec '" + strings.ReplaceAll(executable, "'", "'\"'\"'") + "' -test.run '^TestParentActionCLIProcess$' -- \"$@\"\n"
	if err := execfile.Write(filepath.Join(shim, "glm-parent-action"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	index := ".git/index"
	if alternate == "absolute" {
		index = filepath.Join(cfg.RepoRoot, index)
	}
	if alternate == "alternate" {
		index = filepath.Join(cfg.RepoRoot, ".git", "alternate-index")
		data, err := os.ReadFile(filepath.Join(cfg.RepoRoot, ".git", "index"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(index, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	env := append(parentActionChildEnv(t, cfg, shim), "GIT_INDEX_FILE="+index)
	return cfg, laneCfg.RepoRoot, other, env
}

func TestMetadataCommitHookKeepsPrimaryIndexOutOfLiveLane(t *testing.T) {
	for _, alternate := range []string{"relative", "absolute", "alternate", "unpinned"} {
		t.Run(alternate, func(t *testing.T) {
			cfg, laneRoot, other, env := metadataHookLiveLane(t, alternate)
			path := filepath.Join(cfg.RepoRoot, other)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\nmetadata change\n")...), 0644); err != nil {
				t.Fatal(err)
			}
			add := exec.Command("git", "add", other)
			add.Dir, add.Env = cfg.RepoRoot, env
			if output, err := add.CombinedOutput(); err != nil {
				t.Fatalf("stage metadata: %v %s", err, output)
			}
			commit := exec.Command("git", "commit", "-qm", "metadata through real hook")
			commit.Dir, commit.Env = cfg.RepoRoot, env
			if alternate == "unpinned" {
				before := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
				if output, err := commit.CombinedOutput(); err == nil || !strings.Contains(string(output), "controller-runtime-binding.json") {
					t.Fatalf("unstarted lane must reach the runtime-binding guard: %v %s", err, output)
				}
				if after := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"))); after != before {
					t.Fatal("unstarted lane committed metadata")
				}
				return
			}
			if output, err := commit.CombinedOutput(); err != nil {
				t.Fatalf("metadata hook commit: %v %s", err, output)
			}
			before := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD")))
			if err := os.WriteFile(filepath.Join(laneRoot, "unexpected-source.txt"), []byte("changed\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(data, []byte("\nsecond metadata change\n")...), 0644); err != nil {
				t.Fatal(err)
			}
			add = exec.Command("git", "add", other)
			add.Dir, add.Env = cfg.RepoRoot, env
			if output, err := add.CombinedOutput(); err != nil {
				t.Fatalf("stage second metadata: %v %s", err, output)
			}
			commit = exec.Command("git", "commit", "-qm", "must reject changed lane")
			commit.Dir, commit.Env = cfg.RepoRoot, env
			if output, err := commit.CombinedOutput(); err == nil || !strings.Contains(string(output), "snapshot") {
				t.Fatalf("changed lane was not rejected: %v %s", err, output)
			}
			if after := strings.TrimSpace(string(appTestGitOutput(t, cfg.RepoRoot, "rev-parse", "HEAD"))); after != before {
				t.Fatal("rejected commit changed HEAD")
			}
		})
	}
}
