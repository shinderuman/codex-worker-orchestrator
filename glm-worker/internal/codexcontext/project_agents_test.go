package codexcontext

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectAgentsOverrideLifecyclePreservesRepositoryAgents(t *testing.T) {
	repo := initTestRepo(t)
	codexHome := filepath.Join(t.TempDir(), ".codex")
	t.Setenv("CODEX_CONFIG_DIR", codexHome)
	rootAgents := filepath.Join(repo, "AGENTS.md")
	rootContent := []byte("# user repository instructions\n")
	if err := os.WriteFile(rootAgents, rootContent, 0o644); err != nil {
		t.Fatal(err)
	}

	runAction(t, "enable", repo)
	overridePath := filepath.Join(repo, ProjectAgentsOverrideRelativePath)
	override, err := os.ReadFile(overridePath)
	if err != nil {
		t.Fatal(err)
	}
	wantInstruction := filepath.ToSlash(filepath.Join(codexHome, "instructions", "codex-worker-orchestrator.md"))
	if !bytes.HasPrefix(override, []byte(projectAgentsManagedMarker+"\n")) || !strings.Contains(string(override), wantInstruction) {
		t.Fatalf("managed project AGENTS bootstrap = %q", override)
	}
	if ignored, err := projectAgentsIgnored(repo); err != nil || !ignored {
		t.Fatalf("project AGENTS override ignored=%v err=%v", ignored, err)
	}

	runAction(t, "disable", repo)
	if _, err := os.Stat(overridePath); !os.IsNotExist(err) {
		t.Fatalf("managed project AGENTS override remains after disable: %v", err)
	}
	current, err := os.ReadFile(rootAgents)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, rootContent) {
		t.Fatalf("repository AGENTS changed: %q", current)
	}
}

func TestEnableRefusesUserOwnedAgentsOverrideAndRollsBackConfig(t *testing.T) {
	repo := initTestRepo(t)
	overridePath := filepath.Join(repo, ProjectAgentsOverrideRelativePath)
	original := []byte("# user override\n")
	if err := os.WriteFile(overridePath, original, 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	err := Run([]string{"enable", repo}, &stdout)
	if err == nil || !strings.Contains(err.Error(), "overwrite") || !strings.Contains(err.Error(), ProjectAgentsOverrideRelativePath) {
		t.Fatalf("expected project AGENTS conflict, got %v", err)
	}
	current, readErr := os.ReadFile(overridePath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, original) {
		t.Fatalf("user-owned AGENTS.override.md changed: %q", current)
	}
	if _, statErr := os.Stat(filepath.Join(repo, filepath.FromSlash(ProjectConfigRelativePath))); !os.IsNotExist(statErr) {
		t.Fatalf("project config was not rolled back: %v", statErr)
	}
}

func TestStatusFailsClosedWhenManagedProjectSurfacesDiverge(t *testing.T) {
	repo := initTestRepo(t)
	runAction(t, "enable", repo)
	if err := os.Remove(filepath.Join(repo, ProjectAgentsOverrideRelativePath)); err != nil {
		t.Fatal(err)
	}
	status := runAction(t, "status", repo)
	if status.Status != contextStateConflict {
		t.Fatalf("status = %+v", status)
	}
}

func TestProjectAgentsExcludeRoundTripPreservesBytes(t *testing.T) {
	for _, original := range [][]byte{
		[]byte("# user rule"),
		[]byte("# user rule\n"),
	} {
		name := "without-trailing-newline"
		if bytes.HasSuffix(original, []byte("\n")) {
			name = "with-trailing-newline"
		}
		t.Run(name, func(t *testing.T) {
			repo := initTestRepo(t)
			excludePath := gitOutput(t, repo, "rev-parse", "--git-path", "info/exclude")
			if !filepath.IsAbs(excludePath) {
				excludePath = filepath.Join(repo, excludePath)
			}
			if err := os.WriteFile(excludePath, original, 0o644); err != nil {
				t.Fatal(err)
			}
			runAction(t, "enable", repo)
			runAction(t, "disable", repo)
			current, err := os.ReadFile(excludePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(current, original) {
				t.Fatalf("Git exclude changed: want %q got %q", original, current)
			}
		})
	}
}

func TestStatusFailsClosedWhenManagedProjectAgentsIsNotIgnored(t *testing.T) {
	repo := initTestRepo(t)
	runAction(t, "enable", repo)
	if err := removeProjectAgentsExclude(repo); err != nil {
		t.Fatal(err)
	}
	status := runAction(t, "status", repo)
	if status.Status != contextStateConflict {
		t.Fatalf("status = %+v", status)
	}
}
