package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

type fakeRunner struct{}

func (fakeRunner) run(_ string, _ string, _ ...string) (commandResult, error) {
	return commandResult{}, nil
}

func TestRunFixFormatsGoAndIsIdempotent(t *testing.T) {
	root := fixtureRoot(t)
	writeFixture(t, root, "glm-worker/internal/x/x.go", "package x\nfunc X( ){println(\"x\")}\n")
	first, err := run(root, true, fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fixed != 1 {
		t.Fatalf("fixed=%d want 1", first.Fixed)
	}
	data, err := os.ReadFile(filepath.Join(root, "glm-worker/internal/x/x.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package x\n\nfunc X() { println(\"x\") }\n" {
		t.Fatalf("formatted=%q", data)
	}
	second, err := run(root, true, fakeRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Fixed != 0 {
		t.Fatalf("second fixed=%d", second.Fixed)
	}
}

func TestExternalParsersPreserveLocations(t *testing.T) {
	goIssues := parseGolangCI(commandResult{output: "internal/x.go:12:3: bad thing (revive)\ninternal/y.go:8: duplicated block (dupl)\n", exitCode: 1}, "glm-worker")
	if len(goIssues) != 2 || goIssues[0].Rule != "revive" || goIssues[0].Path != "glm-worker/internal/x.go" || goIssues[0].Line != 12 {
		t.Fatalf("golangci issues=%+v", goIssues)
	}
	if goIssues[1].Rule != "dupl" || goIssues[1].Path != "glm-worker/internal/y.go" || goIssues[1].Line != 8 || goIssues[1].Column != 1 {
		t.Fatalf("line-only golangci issue=%+v", goIssues[1])
	}
	withSourceExcerpt := parseGolangCI(commandResult{output: "internal/z.go:4:1: bad thing (revive)\nconst timestamp = \"2026-07-22 14:06:34\"\n^\n", exitCode: 1}, "glm-worker")
	if len(withSourceExcerpt) != 1 || withSourceExcerpt[0].Path != "glm-worker/internal/z.go" {
		t.Fatalf("source excerpt must not become a diagnostic: %+v", withSourceExcerpt)
	}
	shellIssues := parseShellcheck(commandResult{output: "tests/x.sh:4:2: warning: quote this [SC2086]\n", exitCode: 1}, "tests/x.sh")
	if len(shellIssues) != 1 || shellIssues[0].Rule != "sc2086" || shellIssues[0].Line != 4 {
		t.Fatalf("shellcheck issues=%+v", shellIssues)
	}
}

func TestCommentlintErrorEnvelopeIsFailure(t *testing.T) {
	issues := parseCommentlint(commandResult{output: `{"error":{"kind":"internal","message":"broken"}}`, exitCode: 1})
	if len(issues) != 1 || issues[0].Rule != "commentlint" {
		t.Fatalf("issues=%+v", issues)
	}
}

func TestWriteRegularFileRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "outside")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.go")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := writeRegularFile(root, "link.go", []byte("changed")); err == nil {
		t.Fatal("symlink write must fail")
	}
}
