package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	productionProseMetadataMinCount = 3
	productionProseMetadataMinRunes = 192
	productionProseClusterMinCount  = 10
	productionProseClusterMinRunes  = 1200
)

type productionProseOccurrence struct {
	position token.Position
	target   string
	runes    int
}

func scanProductionProseData(root string, paths []string) ([]Violation, error) {
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
		if violation, found := productionProseDataViolation(set, file, path); found {
			violations = append(violations, violation)
		}
	}
	return violations, nil
}

func productionProseDataViolation(set *token.FileSet, file *ast.File, path string) (Violation, bool) {
	occurrences := productionProseOccurrences(set, file)
	metadata := make([]productionProseOccurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if explanationTarget(occurrence.target) {
			metadata = append(metadata, occurrence)
		}
	}
	if count, runes := proseOccurrenceSize(metadata); count >= productionProseMetadataMinCount && runes >= productionProseMetadataMinRunes {
		return proseDataViolation(path, metadata[0], count, runes, "explanation metadata"), true
	}
	if count, runes := proseOccurrenceSize(occurrences); count >= productionProseClusterMinCount && runes >= productionProseClusterMinRunes {
		return proseDataViolation(path, occurrences[0], count, runes, "production data"), true
	}
	return Violation{}, false
}

func productionProseOccurrences(set *token.FileSet, file *ast.File) []productionProseOccurrence {
	var occurrences []productionProseOccurrence
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			if typed.Tok == token.CONST || typed.Tok == token.VAR {
				collectProseValueSpecs(set, typed.Specs, &occurrences)
			}
		case *ast.FuncDecl:
			if typed.Body == nil || prosePayloadFunction(typed.Name.Name) {
				continue
			}
			collectFunctionProseData(set, typed, &occurrences)
		}
	}
	return occurrences
}

func collectProseValueSpecs(set *token.FileSet, specs []ast.Spec, occurrences *[]productionProseOccurrence) {
	for _, spec := range specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for index, value := range valueSpec.Values {
			target := ""
			if len(valueSpec.Names) > 0 {
				nameIndex := index
				if nameIndex >= len(valueSpec.Names) {
					nameIndex = len(valueSpec.Names) - 1
				}
				target = valueSpec.Names[nameIndex].Name
			}
			collectProseDataExpression(set, value, target, occurrences)
		}
	}
}

func collectFunctionProseData(set *token.FileSet, function *ast.FuncDecl, occurrences *[]productionProseOccurrence) {
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for index, value := range typed.Rhs {
				target := function.Name.Name
				if index < len(typed.Lhs) {
					target = expressionTarget(typed.Lhs[index], target)
				}
				collectProseDataExpression(set, value, target, occurrences)
			}
		case *ast.DeclStmt:
			if declaration, ok := typed.Decl.(*ast.GenDecl); ok && (declaration.Tok == token.CONST || declaration.Tok == token.VAR) {
				collectProseValueSpecs(set, declaration.Specs, occurrences)
			}
		case *ast.ReturnStmt:
			for _, value := range typed.Results {
				collectProseDataExpression(set, value, function.Name.Name, occurrences)
			}
		}
		return true
	})
}

func collectProseDataExpression(set *token.FileSet, expression ast.Expr, target string, occurrences *[]productionProseOccurrence) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return
		}
		value, err := strconv.Unquote(typed.Value)
		if err != nil || !proseLike(value) {
			return
		}
		*occurrences = append(*occurrences, productionProseOccurrence{
			position: set.Position(typed.Pos()),
			target:   target,
			runes:    utf8.RuneCountInString(value),
		})
	case *ast.BinaryExpr:
		collectProseDataExpression(set, typed.X, target, occurrences)
		collectProseDataExpression(set, typed.Y, target, occurrences)
	case *ast.ParenExpr:
		collectProseDataExpression(set, typed.X, target, occurrences)
	case *ast.CompositeLit:
		for _, element := range typed.Elts {
			if keyed, ok := element.(*ast.KeyValueExpr); ok {
				key := expressionTarget(keyed.Key, "")
				collectProseDataExpression(set, keyed.Value, combineTargets(target, key), occurrences)
				continue
			}
			expression, ok := element.(ast.Expr)
			if ok {
				collectProseDataExpression(set, expression, target, occurrences)
			}
		}
	case *ast.CallExpr, *ast.FuncLit:
		return
	}
}

func expressionTarget(expression ast.Expr, fallback string) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.BasicLit:
		if typed.Kind == token.STRING {
			if value, err := strconv.Unquote(typed.Value); err == nil {
				return value
			}
		}
	}
	return fallback
}

func combineTargets(parent, child string) string {
	if parent == "" {
		return child
	}
	if child == "" {
		return parent
	}
	return parent + ":" + child
}

func explanationTarget(target string) bool {
	lower := strings.ToLower(target)
	for _, marker := range []string{"basis", "note", "rule", "explain", "rationale", "description", "guidance", "policy"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func prosePayloadFunction(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), "prompt")
}

func proseOccurrenceSize(occurrences []productionProseOccurrence) (int, int) {
	runes := 0
	for _, occurrence := range occurrences {
		runes += occurrence.runes
	}
	return len(occurrences), runes
}

func proseDataViolation(path string, first productionProseOccurrence, count, runes int, role string) Violation {
	return Violation{
		Rule:   "production-prose-data",
		Path:   path,
		Line:   first.position.Line,
		Column: first.position.Column,
		Message: fmt.Sprintf(
			"%s contains %d long natural-language literals (%d runes) as %s; keep production state structural instead of accumulating explanatory prose",
			first.target, count, runes, role,
		),
	}
}
