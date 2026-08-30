package commentlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanGoAllowsOnlyBuildConstraint(t *testing.T) {
	data := []byte("//go:build linux\n\npackage p\nvar a = \"// value\"\n// prose\nvar b = 1 /* prose */\n")
	findings := scanGo("p.go", data)
	lines := findingLines(findings)
	if !reflect.DeepEqual(lines, []int{5, 6}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellHeredocSemantics(t *testing.T) {
	plain := []byte("#!/bin/sh\ncat <<EOF\n# body\n\tEOF\n# tail\n")
	if lines := findingLines(scanShell("a.sh", plain)); !reflect.DeepEqual(lines, []int{3, 5}) {
		t.Fatalf("plain lines = %v", lines)
	}
	stripped := []byte("#!/bin/sh\ncat <<-EOF\n# body\n\t\tEOF\n# tail\n")
	if lines := findingLines(scanShell("a.sh", stripped)); !reflect.DeepEqual(lines, []int{5}) {
		t.Fatalf("stripped lines = %v", lines)
	}
}

func TestScanShellIgnoresStringsAndHereStrings(t *testing.T) {
	data := []byte("#!/bin/sh\nprintf '%s' '# data'\ngrep -q marker <<<\"$value\"\nvalue=x # prose\n")
	if lines := findingLines(scanShell("a.sh", data)); !reflect.DeepEqual(lines, []int{4}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestClassifyQualityFiles(t *testing.T) {
	cases := []struct {
		path string
		kind string
		ok   bool
	}{
		{"a.go", "go", true},
		{"a.sh", "shell", true},
		{"a.toml", "hash", true},
		{".golangci.yml", "hash", true},
		{"a.yaml", "hash", true},
		{"harnesslint", "shell", true},
		{"goquality", "shell", true},
		{"go.sum", "", false},
		{"README.md", "", false},
		{"a.py", "unclassified", true},
	}
	for _, item := range cases {
		kind, ok := classify(item.path)
		if kind != item.kind || ok != item.ok {
			t.Fatalf("classify(%q) = %q,%v", item.path, kind, ok)
		}
	}
}

func TestRunFixIsSafeAndIdempotent(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, "a.go"), "package p\n// prose\nvar x = 1 // tail\n")
	report, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Fixed != 2 {
		t.Fatalf("report = %+v", report)
	}
	second, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fixed != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func TestRunFixRefusesSymlinkWithoutTouchingTarget(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	outside := filepath.Join(t.TempDir(), "outside.go")
	writeFile(t, outside, "package p\n// keep\n")
	link := filepath.Join(root, "linked.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", "linked.go")
	if _, err := Run(root, true); err == nil {
		t.Fatal("symlink source must fail closed")
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "package p\n// keep\n" {
		t.Fatalf("outside target changed: %q", data)
	}
}

func TestRemoveFindingsDropsCommentOnlyLine(t *testing.T) {
	data := []byte("#!/bin/sh\n# prose\necho x # tail\n")
	fixed := removeFindings(data, scanShell("a.sh", data))
	if string(fixed) != "#!/bin/sh\necho x       \n" {
		t.Fatalf("fixed = %q", fixed)
	}
}

func findingLines(findings []finding) []int {
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	return lines
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
