package commentlint

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanGoAllowsOnlyValidBuildConstraint(t *testing.T) {
	data := []byte("//go:build linux\n\npackage p\n\nvar a = \"// value\"\n// prose\nvar b = 1 /* prose */\n// TODO work\n//nolint:all\n//go:generate false\n")
	findings := scanGo("p.go", data)
	if len(findings) != 5 {
		t.Fatalf("findings = %v", findings)
	}
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{6, 7, 8, 9, 10}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanGoRejectsDocAndInvalidDirective(t *testing.T) {
	data := []byte("// package prose\npackage p\n\n// Exported prose\nfunc Exported() {}\n")
	findings := scanGo("p.go", data)
	if len(findings) != 2 || findings[0].Line != 1 || findings[1].Line != 4 {
		t.Fatalf("findings = %v", findings)
	}
}

func TestScanShellDistinguishesSyntaxStringsAndHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\nvalue=${name#x}\nprintf '%s' '# text'\nvalue=x#y\nvalue=x # prose\ncat <<'EOF'\n# payload\nEOF\n# tail\n")
	findings := scanShell("a.sh", data)
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{5, 9}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellKeepsScanningAfterQuotedFakeHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\nprintf '%s' 'fake <<EOF in single quotes'\n# prose after single-quoted fake\necho \"fake <<EOF in double quotes\"\n# prose after double-quoted fake\necho fake\\<<EOF\n# prose after escaped operator\n")
	findings := scanShell("a.sh", data)
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{3, 5, 7}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellKeepsScanningAfterUnterminatedHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\ncat <<EOF\n# prose inside unterminated body\nvalue=1 # prose tail\n")
	findings := scanShell("a.sh", data)
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{3, 4}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellTreatsHereStringAsNonHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\ngrep -q marker <<<\"$value\"\n# prose after herestring\ncat <<<EOF\n# prose after herestring word\n")
	findings := scanShell("a.sh", data)
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{3, 5}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellScansBodyOnlyAfterRealTerminator(t *testing.T) {
	data := []byte("#!/bin/sh\ncat <<A <<B\n# body of A\nA\n# body of B\n\tB\n# tail\n")
	findings := scanShell("a.sh", data)
	lines := make([]int, 0, len(findings))
	for _, item := range findings {
		lines = append(lines, item.Line)
	}
	if !reflect.DeepEqual(lines, []int{7}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanHashAndGitignoreDistinguishData(t *testing.T) {
	toml := []byte("a = \"# value\"\nb = 1 # prose\n")
	if findings := scanHash("a.toml", toml); len(findings) != 1 || findings[0].Line != 2 {
		t.Fatalf("toml findings = %v", findings)
	}
	ignore := []byte("\\#literal\n# prose\n")
	if findings := scanGitignore(".gitignore", ignore); len(findings) != 1 || findings[0].Line != 2 {
		t.Fatalf("gitignore findings = %v", findings)
	}
}

func TestClassifyCurrentSourceAndNonSourceKinds(t *testing.T) {
	cases := []struct {
		path     string
		kind     string
		eligible bool
	}{
		{"a.go", "go", true},
		{"go.mod", "go", true},
		{"a.sh", "shell", true},
		{"commentlint", "shell", true},
		{".githooks/post-merge", "shell", true},
		{"a.toml", "toml", true},
		{"a.rules", "toml", true},
		{".gitignore", "gitignore", true},
		{"a.md", "", false},
		{"a.json", "", false},
		{"a.txt", "", false},
		{"LICENSE", "", false},
		{"a.py", "unclassified", true},
	}
	for _, item := range cases {
		kind, eligible := classify(item.path)
		if kind != item.kind || eligible != item.eligible {
			t.Fatalf("classify(%q) = %q,%v", item.path, kind, eligible)
		}
	}
}

func TestRunFixIsSafeAndIdempotent(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, "a.go"), "package p\n\nvar x = 1 // prose\nvar y = \"// data\"\n")
	writeFile(t, filepath.Join(root, "run.sh"), "#!/bin/sh\nprintf '%s' '# data' # prose\n")
	report, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Fixed != 2 || len(report.Violations) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if string(readFile(t, filepath.Join(root, "a.go"))) != "package p\n\nvar x = 1         \nvar y = \"// data\"\n" {
		t.Fatalf("go fix = %q", readFile(t, filepath.Join(root, "a.go")))
	}
	second, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fixed != 0 || len(second.Violations) != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func TestRunFixFailsClosedBeforeEditingUnclassifiedSource(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	path := filepath.Join(root, "a.go")
	content := "package p\n// prose\n"
	writeFile(t, path, content)
	writeFile(t, filepath.Join(root, "new.xyz"), "value\n")
	report, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || report.Fixed != 0 || len(report.Violations) != 1 || report.Violations[0].Kind != "unclassified" {
		t.Fatalf("report = %+v", report)
	}
	if string(readFile(t, path)) != content {
		t.Fatalf("source changed = %q", readFile(t, path))
	}
}

func TestRunWithoutGitUsesFilesystemInventory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package p\n// prose\n")
	writeFile(t, filepath.Join(root, "README.md"), "# data\n")
	report, err := Run(root, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "fail" || len(report.Violations) != 1 || report.Violations[0].Path != "a.go" {
		t.Fatalf("report = %+v", report)
	}
}

func TestRemoveFindingsRemovesCommentOnlyLines(t *testing.T) {
	cases := []struct {
		name     string
		scan     func(string, []byte) []finding
		path     string
		data     string
		expected string
	}{
		{
			name:     "shell drops comment-only lines and keeps existing blank line single",
			scan:     scanShell,
			path:     "a.sh",
			data:     "#!/bin/sh\n# full prose\necho one\n# first\n# second\necho two\n\necho three # tail\n",
			expected: "#!/bin/sh\necho one\necho two\n\necho three       \n",
		},
		{
			name:     "shell keeps comment line after line continuation as blanked line",
			scan:     scanShell,
			path:     "a.sh",
			data:     "#!/bin/sh\necho one \\\n# continued prose\necho two\n",
			expected: "#!/bin/sh\necho one \\\n                 \necho two\n",
		},
		{
			name:     "shell drops indented comment-only line and unterminated tail comment",
			scan:     scanShell,
			path:     "a.sh",
			data:     "#!/bin/sh\nif true; then\n    # prose\n    echo one\nfi\n# tail prose",
			expected: "#!/bin/sh\nif true; then\n    echo one\nfi\n",
		},
		{
			name:     "go drops line and block comment-only lines",
			scan:     scanGo,
			path:     "a.go",
			data:     "package p\n\n// full prose\nvar x = 1\n\n/* block head\nblock tail\n*/\nvar y = 2\n",
			expected: "package p\n\nvar x = 1\n\nvar y = 2\n",
		},
		{
			name:     "go blanks code line head and drops covered tail line of block comment",
			scan:     scanGo,
			path:     "a.go",
			data:     "package p\nvar a = 1 /* head\ntail */\nvar b = 2\n",
			expected: "package p\nvar a = 1        \nvar b = 2\n",
		},
		{
			name:     "go drops line holding only two comments",
			scan:     scanGo,
			path:     "a.go",
			data:     "package p\n/* one */ /* two */\nvar x = 1\n",
			expected: "package p\nvar x = 1\n",
		},
		{
			name:     "toml drops comment-only line",
			scan:     scanHash,
			path:     "a.toml",
			data:     "# prose\nvalue = 1\n",
			expected: "value = 1\n",
		},
		{
			name:     "gitignore drops comment-only line",
			scan:     scanGitignore,
			path:     ".gitignore",
			data:     "# prose\n/file\n",
			expected: "/file\n",
		},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			data := []byte(item.data)
			fixed := removeFindings(data, item.scan(item.path, data))
			if string(fixed) != item.expected {
				t.Fatalf("fixed = %q, want %q", fixed, item.expected)
			}
		})
	}
}

func TestRunFixDropsCommentOnlyLinesWithoutNewBlankLines(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init")
	writeFile(t, filepath.Join(root, "run.sh"), "#!/bin/sh\n# full prose\necho one\n# first\n# second\necho two\n\necho three # tail\n")
	writeFile(t, filepath.Join(root, "a.go"), "package p\n\n// full prose\nvar x = 1\n")
	report, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "pass" || report.Fixed != 5 || len(report.Violations) != 0 {
		t.Fatalf("report = %+v", report)
	}
	if string(readFile(t, filepath.Join(root, "run.sh"))) != "#!/bin/sh\necho one\necho two\n\necho three       \n" {
		t.Fatalf("shell fix = %q", readFile(t, filepath.Join(root, "run.sh")))
	}
	if string(readFile(t, filepath.Join(root, "a.go"))) != "package p\n\nvar x = 1\n" {
		t.Fatalf("go fix = %q", readFile(t, filepath.Join(root, "a.go")))
	}
	second, err := Run(root, true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fixed != 0 || len(second.Violations) != 0 {
		t.Fatalf("second = %+v", second)
	}
}

func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
