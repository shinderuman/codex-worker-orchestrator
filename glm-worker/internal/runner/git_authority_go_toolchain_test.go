package runner

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
)

func writeGoContractRepo(t *testing.T, goVersion string) string {
	t.Helper()
	repo := t.TempDir()
	if output, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	contract := "namespace: probe\n"
	if goVersion != "" {
		contract += "go: " + goVersion + "\n"
	}
	if err := os.WriteFile(filepath.Join(repo, "quality-tools.yml"), []byte(contract), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestGitAuthorityGuardFailsClosedOnUnusableGoContract(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		repo func(t *testing.T) string
	}{
		{name: "malformed-version", repo: func(t *testing.T) string { return writeGoContractRepo(t, "latest") }},
		{name: "contract-entry-missing", repo: func(t *testing.T) string {
			repo := t.TempDir()
			if output, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			if err := os.WriteFile(filepath.Join(repo, "quality-tools.yml"), []byte("namespace: probe\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return repo
		}},
		{name: "unreadable-contract", repo: func(t *testing.T) string {
			repo := t.TempDir()
			if output, err := exec.Command("git", "init", repo).CombinedOutput(); err != nil {
				t.Fatalf("git init: %v: %s", err, output)
			}
			if err := os.Mkdir(filepath.Join(repo, "quality-tools.yml"), 0o700); err != nil {
				t.Fatal(err)
			}
			return repo
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prepareGitAuthorityGuard(tc.repo(t))
			if err == nil {
				t.Fatal("契約repoの壊れたquality-tools.ymlでguardが準備されました")
			}
			var guardErr *GitAuthorityGuardError
			if !errors.As(err, &guardErr) || guardErr.Stage != "resolve-go-toolchain" {
				t.Fatalf("guard error = %#v", err)
			}
		})
	}
}

func TestResolveGoVersionContractDistinguishesAbsentContract(t *testing.T) {
	t.Parallel()
	repo := writeGoContractRepo(t, "")
	if err := os.Remove(filepath.Join(repo, "quality-tools.yml")); err != nil {
		t.Fatal(err)
	}
	version, declared, err := qualitygate.ResolveGoVersionContract(repo)
	if err != nil || declared || version != "" {
		t.Fatalf("absent contract resolution = %q, %v, %v", version, declared, err)
	}
	version, declared, err = qualitygate.ResolveGoVersionContract(writeGoContractRepo(t, "1.25.4"))
	if err != nil || !declared || version != "1.25.4" {
		t.Fatalf("declared contract resolution = %q, %v, %v", version, declared, err)
	}
}

func TestGitAuthorityClaudeWrapperPinsContractGoToolchain(t *testing.T) {
	t.Parallel()
	repo := writeGoContractRepo(t, "1.25.4")
	guard, err := prepareGitAuthorityGuard(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	if guard.goToolchain != "go1.25.4" {
		t.Fatalf("guard goToolchain = %q", guard.goToolchain)
	}

	probe := filepath.Join(t.TempDir(), "probe.sh")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$GOTOOLCHAIN\" >\"$1\"\n"
	if err := os.WriteFile(probe, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	wrapped, err := guard.prepareClaudeWrapper(probe)
	if err != nil {
		t.Fatal(err)
	}
	observed := filepath.Join(t.TempDir(), "gotoolchain.txt")
	command := exec.Command(wrapped, observed)
	command.Env = append(os.Environ(), "GOTOOLCHAIN=go9.9.9", "PATH="+t.TempDir())
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("wrapped probe failed: %v: %s", err, output)
	}
	data, err := os.ReadFile(observed)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "go1.25.4" {
		t.Fatalf("wrapped claude env GOTOOLCHAIN = %q", string(data))
	}
}

func TestGitAuthorityClaudeWrapperSkipsPinWithoutGoContract(t *testing.T) {
	t.Parallel()
	repo := writeGoContractRepo(t, "1.25.4")
	if err := os.Remove(filepath.Join(repo, "quality-tools.yml")); err != nil {
		t.Fatal(err)
	}
	guard, err := prepareGitAuthorityGuard(repo)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	if guard.goToolchain != "" {
		t.Fatalf("guard goToolchain = %q want empty", guard.goToolchain)
	}
	script := gitAuthorityClaudeWrapperScript(guard, "/bin/true")
	if strings.Contains(script, "GOTOOLCHAIN") {
		t.Fatalf("wrapper script pins a toolchain without a go contract: %s", script)
	}
}
