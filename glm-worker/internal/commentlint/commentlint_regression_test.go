package commentlint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanGoRejectsDocAndInvalidDirective(t *testing.T) {
	data := []byte("// package prose\npackage p\n\n// Exported prose\nfunc Exported() {}\n")
	findings := scanGo("p.go", data)
	if len(findings) != 2 || findings[0].Line != 1 || findings[1].Line != 4 {
		t.Fatalf("findings = %v", findings)
	}
}

func TestScanShellDistinguishesSyntaxStringsAndHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\nvalue=${name#x}\nprintf '%s' '# text'\nvalue=x#y\nvalue=x # prose\ncat <<'EOF'\n# payload\nEOF\n# tail\n")
	if lines := findingLines(scanShell("a.sh", data)); !reflect.DeepEqual(lines, []int{5, 9}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellKeepsScanningAfterQuotedFakeHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\nprintf '%s' 'fake <<EOF in single quotes'\n# prose after single-quoted fake\necho \"fake <<EOF in double quotes\"\n# prose after double-quoted fake\necho fake\\<<EOF\n# prose after escaped operator\n")
	if lines := findingLines(scanShell("a.sh", data)); !reflect.DeepEqual(lines, []int{3, 5, 7}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellKeepsScanningAfterUnterminatedHeredoc(t *testing.T) {
	data := []byte("#!/bin/sh\ncat <<EOF\n# prose inside unterminated body\nvalue=1 # prose tail\n")
	if lines := findingLines(scanShell("a.sh", data)); !reflect.DeepEqual(lines, []int{3, 4}) {
		t.Fatalf("lines = %v", lines)
	}
}

func TestScanShellScansSequentialHeredocsWithStripTabs(t *testing.T) {
	data := []byte("#!/bin/sh\ncat <<A <<-B\n# body of A\nA\n# body of B\n\t\tB\n# tail\n")
	if lines := findingLines(scanShell("a.sh", data)); !reflect.DeepEqual(lines, []int{7}) {
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
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Fatalf("source changed = %q", data)
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

func TestRemoveFindingsPreservesCodeAndDropsCommentOnlyLines(t *testing.T) {
	cases := []struct {
		name string
		scan func(string, []byte) []finding
		path string
		data string
		want string
	}{
		{"shell", scanShell, "a.sh", "#!/bin/sh\n# full prose\necho one\n\necho two # tail\n", "#!/bin/sh\necho one\n\necho two       \n"},
		{"go", scanGo, "a.go", "package p\n\n// full prose\nvar x = 1\n", "package p\n\nvar x = 1\n"},
		{"toml", scanHash, "a.toml", "# prose\nvalue = 1\n", "value = 1\n"},
		{"gitignore", scanGitignore, ".gitignore", "# prose\n/file\n", "/file\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := []byte(tc.data)
			if got := string(removeFindings(data, tc.scan(tc.path, data))); got != tc.want {
				t.Fatalf("fixed = %q want %q", got, tc.want)
			}
		})
	}
}
