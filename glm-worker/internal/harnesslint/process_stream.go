package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

var processStreamOwners = map[string]map[string]int{
	"glm-worker/cmd/glm-worker/main.go":         {"Stderr": 1},
	"glm-worker/cmd/glm-parent-action/main.go": {"Stdout": 1, "Stderr": 1},
	"glm-worker/cmd/commentlint/main.go":        {"Stdout": 1, "Stderr": 1},
	"glm-worker/cmd/harnesslint/main.go":        {"Stdout": 1, "Stderr": 1},
	"glm-worker/cmd/merge-json/main.go":         {"Stdout": 1, "Stderr": 1},
	"glm-worker/cmd/plancheck/main.go":          {"Stdout": 1, "Stderr": 1},
	"glm-worker/internal/app/machine_output.go": {"Stdin": 1, "Stdout": 1, "Stderr": 1},
	"glm-worker/internal/state/stats.go":        {"Stderr": 1},
}

func processStreamViolations(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range goFiles(paths) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		data, err := readRegularFile(root, path)
		if err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, path, data, 0)
		if err != nil {
			continue
		}
		found, positions, direct := processStreamReferences(set, file, path)
		violations = append(violations, direct...)
		expected := processStreamOwners[path]
		for _, name := range []string{"Stdin", "Stdout", "Stderr"} {
			if found[name] == expected[name] {
				continue
			}
			line, column := 1, 1
			if position, ok := positions[name]; ok {
				line, column = position.Line, position.Column
			}
			violations = append(violations, Violation{
				Rule: "process-stream-boundary", Path: path, Line: line, Column: column,
				Message: fmt.Sprintf("os.%s ownership mismatch: found %d want %d", name, found[name], expected[name]),
			})
		}
	}
	return violations, nil
}

func processStreamReferences(set *token.FileSet, file *ast.File, path string) (map[string]int, map[string]token.Position, []Violation) {
	aliases, violations := processStreamImportAliases(set, file, path)
	found := make(map[string]int)
	positions := make(map[string]token.Position)
	ast.Inspect(file, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok {
			recordProcessStreamSelector(set, aliases, selector, found, positions)
		}
		if call, ok := node.(*ast.CallExpr); ok {
			if violation, ok := directPrintViolation(set, aliases, path, call); ok {
				violations = append(violations, violation)
			}
		}
		return true
	})
	return found, positions, violations
}

func processStreamImportAliases(set *token.FileSet, file *ast.File, path string) (map[string]string, []Violation) {
	aliases := make(map[string]string)
	var violations []Violation
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if spec.Name != nil && spec.Name.Name == "." {
			if importPath == "os" || importPath == "fmt" {
				position := set.Position(spec.Pos())
				violations = append(violations, Violation{
					Rule: "process-stream-boundary", Path: path, Line: position.Line, Column: position.Column,
					Message: "dot import of " + importPath + " makes process-stream ownership ambiguous",
				})
			}
			continue
		}
		if spec.Name != nil && spec.Name.Name == "_" {
			continue
		}
		name := importPath[strings.LastIndex(importPath, "/")+1:]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		aliases[name] = importPath
	}
	return aliases, violations
}

func recordProcessStreamSelector(set *token.FileSet, aliases map[string]string, selector *ast.SelectorExpr, found map[string]int, positions map[string]token.Position) {
	identifier, ok := selector.X.(*ast.Ident)
	if !ok || aliases[identifier.Name] != "os" || !isProcessStreamName(selector.Sel.Name) {
		return
	}
	found[selector.Sel.Name]++
	if _, exists := positions[selector.Sel.Name]; !exists {
		positions[selector.Sel.Name] = set.Position(selector.Pos())
	}
}

func isProcessStreamName(name string) bool {
	switch name {
	case "Stdin", "Stdout", "Stderr":
		return true
	default:
		return false
	}
}

func directPrintViolation(set *token.FileSet, aliases map[string]string, path string, call *ast.CallExpr) (Violation, bool) {
	name, ok := directPrintCall(aliases, call)
	if !ok {
		return Violation{}, false
	}
	position := set.Position(call.Pos())
	return Violation{
		Rule: "process-stream-boundary", Path: path, Line: position.Line, Column: position.Column,
		Message: "direct process print " + name + " is forbidden; use an injected writer",
	}, true
}

func directPrintCall(aliases map[string]string, call *ast.CallExpr) (string, bool) {
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		identifier, ok := selector.X.(*ast.Ident)
		if !ok || aliases[identifier.Name] != "fmt" {
			return "", false
		}
		switch selector.Sel.Name {
		case "Print", "Printf", "Println":
			return "fmt." + selector.Sel.Name, true
		default:
			return "", false
		}
	}
	identifier, ok := call.Fun.(*ast.Ident)
	if !ok {
		return "", false
	}
	switch identifier.Name {
	case "print", "println":
		return identifier.Name, true
	default:
		return "", false
	}
}
