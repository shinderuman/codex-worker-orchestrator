package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const (
	processStreamKindOsStream    = "osstream"
	processStreamKindDirectPrint = "directprint"
	processStreamKindDotImport   = "dotimport"
)

var processStreamExpectations = map[string]map[string]int{
	"cmd/glm-worker/main.go":         {"Stderr": 1},
	"cmd/commentlint/main.go":        {"Stderr": 4, "Stdout": 1},
	"internal/app/machine_output.go": {"Stdin": 1, "Stdout": 1, "Stderr": 1},
	"internal/state/stats.go":        {"Stderr": 1},
}

func TestProcessStreamReferencesStayInsideBoundary(t *testing.T) {
	moduleRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	found := map[string][]string{}
	seenExpectationFiles := map[string]bool{}
	err = filepath.WalkDir(moduleRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		if _, declared := processStreamExpectations[relative]; declared {
			seenExpectationFiles[relative] = true
		}
		references, err := processStreamReferences(path)
		if err != nil {
			return err
		}
		violations := processStreamExpectationViolations(processStreamExpectations[relative], references)
		if len(violations) > 0 {
			found[relative] = violations
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var stale []string
	for relative := range processStreamExpectations {
		if !seenExpectationFiles[relative] {
			stale = append(stale, relative)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Fatalf("process stream期待集合に存在しないfileが残っています: %v", stale)
	}
	if len(found) > 0 {
		var report []string
		for _, relative := range sortedStringSliceKeys(found) {
			report = append(report, relative+": "+strings.Join(found[relative], ", "))
		}
		t.Fatalf(
			"os.Stdin/os.Stdout/os.Stderr参照はfile毎の期待集合(%s)と種類・個数が完全一致しないと拒否され、writer引数なしのfmt.Print/fmt.Printf/fmt.Printlnとbuiltin print/printlnは期待0件です。writerを引数経由で渡すかsubprocess出力をcaptureしてください。正当な所有点を増やす場合はprocessStreamExpectationsの明示更新が必要です: %s",
			declaredExpectationSummary(),
			strings.Join(report, "; "),
		)
	}
}

func declaredExpectationSummary() string {
	entries := make([]string, 0, len(processStreamExpectations))
	for _, relative := range sortedIntMapKeys(processStreamExpectations) {
		members := make([]string, 0, len(processStreamExpectations[relative]))
		for member := range processStreamExpectations[relative] {
			members = append(members, fmt.Sprintf("%s×%d", member, processStreamExpectations[relative][member]))
		}
		sort.Strings(members)
		entries = append(entries, relative+"="+strings.Join(members, ","))
	}
	return strings.Join(entries, "; ")
}

func sortedIntMapKeys(source map[string]map[string]int) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type processStreamReference struct {
	Position string
	Kind     string
	Name     string
}

func processStreamReferences(path string) ([]processStreamReference, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}
	imports := importedPackagePaths(file)
	references := []processStreamReference{}
	for _, imported := range imports.dotImports {
		if imported == "os" || imported == "fmt" {
			references = append(references, processStreamReference{
				Position: fset.Position(imports.dotImportPositions[imported]).String(),
				Kind:     processStreamKindDotImport,
				Name:     imported,
			})
		}
	}
	for _, decl := range file.Decls {
		collectProcessStreamReferences(fset, imports, decl, &references)
	}
	sort.Slice(references, func(i, j int) bool { return references[i].Position < references[j].Position })
	return references, nil
}

func collectProcessStreamReferences(fset *token.FileSet, imports importPathResolution, root ast.Node, references *[]processStreamReference) {
	ast.Inspect(root, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			identifier, ok := node.X.(*ast.Ident)
			if !ok || imports.paths[identifier.Name] != "os" {
				return true
			}
			switch node.Sel.Name {
			case "Stdin", "Stdout", "Stderr":
				*references = append(*references, processStreamReference{
					Position: fset.Position(node.Pos()).String(),
					Kind:     processStreamKindOsStream,
					Name:     node.Sel.Name,
				})
			}
		case *ast.CallExpr:
			if name, direct := directProcessPrintName(imports, node); direct {
				*references = append(*references, processStreamReference{
					Position: fset.Position(node.Pos()).String(),
					Kind:     processStreamKindDirectPrint,
					Name:     name,
				})
			}
		}
		return true
	})
}

func processStreamExpectationViolations(expected map[string]int, references []processStreamReference) []string {
	violations := []string{}
	found := map[string]int{}
	positions := map[string][]string{}
	for _, reference := range references {
		switch reference.Kind {
		case processStreamKindDotImport:
			violations = append(violations, reference.Position+": dot import("+reference.Name+")は安全判定不能のためfail closed")
		case processStreamKindDirectPrint:
			violations = append(violations, reference.Position+": 直接print("+reference.Name+")は期待0件です")
		case processStreamKindOsStream:
			found[reference.Name]++
			positions[reference.Name] = append(positions[reference.Name], reference.Position)
		}
	}
	for _, member := range sortedMembers(expected, found) {
		if found[member] != expected[member] {
			violations = append(violations, fmt.Sprintf(
				"os.%s参照が期待と不一致: found %d want %d (%s)",
				member, found[member], expected[member], strings.Join(positions[member], ", "),
			))
		}
	}
	sort.Strings(violations)
	return violations
}

func sortedMembers(expected map[string]int, found map[string]int) []string {
	seen := map[string]bool{}
	for member := range expected {
		seen[member] = true
	}
	for member := range found {
		seen[member] = true
	}
	members := make([]string, 0, len(seen))
	for member := range seen {
		members = append(members, member)
	}
	sort.Strings(members)
	return members
}

type importPathResolution struct {
	paths              map[string]string
	dotImports         []string
	dotImportPositions map[string]token.Pos
}

func importedPackagePaths(file *ast.File) importPathResolution {
	resolution := importPathResolution{
		paths:              map[string]string{},
		dotImportPositions: map[string]token.Pos{},
	}
	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name == nil {
			resolution.paths[defaultPackageName(value)] = value
			continue
		}
		switch spec.Name.Name {
		case "_":
		case ".":
			if _, seen := resolution.dotImportPositions[value]; !seen {
				resolution.dotImports = append(resolution.dotImports, value)
				resolution.dotImportPositions[value] = spec.Pos()
			}
		default:
			resolution.paths[spec.Name.Name] = value
		}
	}
	return resolution
}

func defaultPackageName(importPath string) string {
	return importPath[strings.LastIndex(importPath, "/")+1:]
}

func directProcessPrintName(imports importPathResolution, call *ast.CallExpr) (string, bool) {
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		identifier, ok := selector.X.(*ast.Ident)
		if ok && imports.paths[identifier.Name] == "fmt" {
			switch selector.Sel.Name {
			case "Print", "Printf", "Println":
				return selector.Sel.Name, true
			}
		}
		return "", false
	}
	if identifier, ok := call.Fun.(*ast.Ident); ok {
		switch identifier.Name {
		case "print", "println":
			return identifier.Name, true
		}
	}
	return "", false
}

func TestProcessStreamReferencesDetectDirectPrints(t *testing.T) {
	leaky := "package probe\n" +
		"\n" +
		"import \"fmt\"\n" +
		"\n" +
		"func leak() {\n" +
		"\tfmt.Println(\"install smoke: PASS\")\n" +
		"\tfmt.Print(\"text\")\n" +
		"\tfmt.Printf(\"%d\\n\", 1)\n" +
		"\tprint(\"builtin stdout\")\n" +
		"\tprintln(\"builtin stderr\")\n" +
		"}\n"
	leakyPath := filepath.Join(t.TempDir(), "leaky.go")
	if err := os.WriteFile(leakyPath, []byte(leaky), 0o600); err != nil {
		t.Fatal(err)
	}
	references, err := processStreamReferences(leakyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 5 {
		t.Fatalf("直接print系の検出数 = %d want 5: %v", len(references), references)
	}
	violations := processStreamExpectationViolations(nil, references)
	if len(violations) != 5 {
		t.Fatalf("期待0件での直接print違反数 = %d want 5: %v", len(violations), violations)
	}

	clean := "package probe\n" +
		"\n" +
		"import (\n" +
		"\t\"fmt\"\n" +
		"\t\"io\"\n" +
		")\n" +
		"\n" +
		"func emit(w io.Writer) {\n" +
		"\tfmt.Fprintln(w, \"writer経由\")\n" +
		"\tfmt.Fprintf(w, \"writer経由 %d\\n\", 1)\n" +
		"}\n"
	cleanPath := filepath.Join(t.TempDir(), "clean.go")
	if err := os.WriteFile(cleanPath, []byte(clean), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanReferences, err := processStreamReferences(cleanPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleanReferences) != 0 {
		t.Fatalf("writer引数経由のFprint系が誤検出されています: %v", cleanReferences)
	}
}

func TestProcessStreamReferencesResolveImportAliases(t *testing.T) {
	aliased := "package probe\n" +
		"\n" +
		"import (\n" +
		"\tf \"fmt\"\n" +
		"\to \"os\"\n" +
		")\n" +
		"\n" +
		"func leak() {\n" +
		"\tf.Println(\"alias経由の直接print\")\n" +
		"\t_, _ = o.Stdout.Write([]byte(\"alias経由の直結\"))\n" +
		"}\n"
	aliasedPath := filepath.Join(t.TempDir(), "aliased.go")
	if err := os.WriteFile(aliasedPath, []byte(aliased), 0o600); err != nil {
		t.Fatal(err)
	}
	references, err := processStreamReferences(aliasedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 2 {
		t.Fatalf("import alias経由の検出数 = %d want 2: %v", len(references), references)
	}

	dotted := "package probe\n" +
		"\n" +
		"import . \"os\"\n" +
		"\n" +
		"func leak() {\n" +
		"\t_, _ = Stdout.Write([]byte(\"dot import\"))\n" +
		"}\n"
	dottedPath := filepath.Join(t.TempDir(), "dotted.go")
	if err := os.WriteFile(dottedPath, []byte(dotted), 0o600); err != nil {
		t.Fatal(err)
	}
	dotReferences, err := processStreamReferences(dottedPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(dotReferences) != 1 || dotReferences[0].Kind != processStreamKindDotImport {
		t.Fatalf("dot importのfail closed検出 = %v want dotimport 1件", dotReferences)
	}
}

func TestProcessStreamExpectationsFailClosedOnExtraReferences(t *testing.T) {
	runCase := func(t *testing.T, name string, expected map[string]int, source string, want int) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "probe.go")
			if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
				t.Fatal(err)
			}
			references, err := processStreamReferences(path)
			if err != nil {
				t.Fatal(err)
			}
			violations := processStreamExpectationViolations(expected, references)
			if len(violations) != want {
				t.Fatalf("違反数 = %d want %d: %v", len(violations), want, violations)
			}
		})
	}

	boundaryOwner := "package probe\n" +
		"\n" +
		"import \"os\"\n" +
		"\n" +
		"func Run(w interface{ Write([]byte) (int, error) }) {\n" +
		"\t_ = w\n" +
		"\t_, _, _ = os.Stdin, os.Stdout, os.Stderr\n" +
		"}\n"
	exactExpectation := map[string]int{"Stdin": 1, "Stdout": 1, "Stderr": 1}

	runCase(t, "正当参照が種類・個数とも期待一致なら素通り", exactExpectation, boundaryOwner, 0)

	runCase(t, "許可対象のRun内へos.Stdout参照を1件追加するとfail closed",
		exactExpectation,
		"package probe\n"+
			"\n"+
			"import \"os\"\n"+
			"\n"+
			"func Run(w interface{ Write([]byte) (int, error) }) {\n"+
			"\t_, _ = os.Stdout.Write([]byte(\"追加直結\"))\n"+
			"\t_, _, _ = os.Stdin, os.Stdout, os.Stderr\n"+
			"}\n",
		1)

	runCase(t, "許可対象のRun内へfmt.Printlnを追加するとfail closed",
		exactExpectation,
		"package probe\n"+
			"\n"+
			"import (\n"+
			"\t\"fmt\"\n"+
			"\t\"os\"\n"+
			")\n"+
			"\n"+
			"func Run(w interface{ Write([]byte) (int, error) }) {\n"+
			"\tfmt.Println(\"追加直接print\")\n"+
			"\t_, _, _ = os.Stdin, os.Stdout, os.Stderr\n"+
			"}\n",
		1)

	runCase(t, "package level宣言許可形へ関数内os.Stderr追加でfail closed",
		map[string]int{"Stderr": 1},
		"package probe\n"+
			"\n"+
			"import \"os\"\n"+
			"\n"+
			"var warnOut interface{ Write([]byte) (int, error) } = os.Stderr\n"+
			"\n"+
			"func emit() {\n"+
			"\t_, _ = os.Stderr.Write([]byte(\"関数内の追加直結\"))\n"+
			"}\n",
		1)

	runCase(t, "期待にある正当参照が実際に消えてもfail closed",
		exactExpectation,
		"package probe\n"+
			"\n"+
			"import \"os\"\n"+
			"\n"+
			"func Run() {\n"+
			"\t_, _ = os.Stdin, os.Stderr\n"+
			"}\n",
		1)

	runCase(t, "期待宣言のないfileのos.Stdin参照はfail closed",
		nil,
		"package probe\n"+
			"\n"+
			"import \"os\"\n"+
			"\n"+
			"func emit() {\n"+
			"\t_, _ = os.Stdin.Read(make([]byte, 1))\n"+
			"}\n",
		1)

	runCase(t, "種類個数一致を狙ったdot importもfail closed",
		map[string]int{"Stdout": 1},
		"package probe\n"+
			"\n"+
			"import . \"os\"\n"+
			"\n"+
			"func emit() {\n"+
			"\t_, _ = Stdout.Write([]byte(\"dot import\"))\n"+
			"}\n",
		2)
}

func sortedStringSliceKeys(source map[string][]string) []string {
	keys := make([]string, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
