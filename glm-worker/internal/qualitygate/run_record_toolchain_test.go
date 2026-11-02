package qualitygate

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeQualityToolContract(t *testing.T, content string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, QualityToolsFile), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestRequiredGoVersionReadsContract(t *testing.T) {
	t.Parallel()
	repo := writeQualityToolContract(t, "namespace: probe\ngo: 1.25.4\nlint-go: 1.22.12\n")
	version, err := RequiredGoVersion(repo)
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.25.4" {
		t.Fatalf("required go version = %q", version)
	}
}

func TestRequiredGoVersionFailsClosed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content string
	}{
		{name: "missing-contract", content: ""},
		{name: "no-go-entry", content: "namespace: probe\nshellcheck: 0.11.0\n"},
		{name: "malformed-version", content: "go: latest\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := writeQualityToolContract(t, tc.content)
			if tc.content == "" {
				if err := os.Remove(filepath.Join(repo, QualityToolsFile)); err != nil {
					t.Fatal(err)
				}
			}
			version, err := RequiredGoVersion(repo)
			if err == nil || version != "" {
				t.Fatalf("RequiredGoVersion = %q, %v", version, err)
			}
			if !strings.Contains(err.Error(), QualityToolsFile) && !strings.Contains(err.Error(), "go") {
				t.Fatalf("error does not identify the contract: %v", err)
			}
		})
	}
}

func TestRunToolchainMatchesRequiredGoVersion(t *testing.T) {
	t.Parallel()
	matched := &RunToolchain{GoVersion: "go1.25.4", RequiredGoVersion: "1.25.4", EnvironmentRecorded: true}
	if !matched.MatchesRequiredGoVersion() {
		t.Fatal("matching toolchain was rejected")
	}
	var missingObservation *RunToolchain
	if missingObservation.MatchesRequiredGoVersion() {
		t.Fatal("nil toolchain matched a required version")
	}
	for name, toolchain := range map[string]*RunToolchain{
		"version-drift":       {GoVersion: "go1.26.1", RequiredGoVersion: "1.25.4", EnvironmentRecorded: true},
		"unrecorded":          {GoVersion: "go1.25.4", RequiredGoVersion: "1.25.4"},
		"missing-required":    {GoVersion: "go1.25.4", EnvironmentRecorded: true},
		"missing-observation": {RequiredGoVersion: "1.25.4", EnvironmentRecorded: true},
	} {
		if toolchain.MatchesRequiredGoVersion() {
			t.Fatalf("%s toolchain matched a required version", name)
		}
	}
}

func TestNoIgnoredInputsRejectsIgnoredFilesInsideNestedRepository(t *testing.T) {
	root := t.TempDir()
	runGit := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = dir
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit(root, "init", "-q")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(root, "add", ".gitignore")
	runGit(root, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "ignore rules")

	nested := filepath.Join(root, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "nested.txt"), []byte("nested\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(nested, "init", "-q")
	clean, err := NoIgnoredInputs(root)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Fatal("ignored fileがないnested repositoryで拒否されました")
	}

	if err := os.WriteFile(filepath.Join(nested, "trace.log"), []byte("on disk only after transfer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignored, err := NoIgnoredInputs(root)
	if err != nil {
		t.Fatal(err)
	}
	if ignored {
		t.Fatal("nested repository内のouter-ignored fileが無音通過されました")
	}
}
