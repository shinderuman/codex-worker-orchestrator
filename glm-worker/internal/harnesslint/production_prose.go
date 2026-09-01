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

type productionProseOccurrence struct {
	position token.Position
	target   string
	runes    int
}

const (
	productionProseMetadataMinCount = 3
	productionProseMetadataMinRunes = 192
)

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
	count, runes := proseOccurrenceSize(metadata)
	if count < productionProseMetadataMinCount || runes < productionProseMetadataMinRunes {
		return Violation{}, false
	}
	return proseDataViolation(path, metadata[0], count, runes), true
}

func productionProseOccurrences(set *token.FileSet, file *ast.File) []productionProseOccurrence {
	var occurrences []productionProseOccurrence
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			collectProductionProseDeclaration(set, typed, &occurrences)
		case *ast.FuncDecl:
			if typed.Body != nil && !prosePayloadFunction(typed.Name.Name) {
				collectFunctionProseData(set, typed, &occurrences)
			}
		}
	}
	return occurrences
}

func collectProductionProseDeclaration(set *token.FileSet, declaration *ast.GenDecl, occurrences *[]productionProseOccurrence) {
	if declaration.Tok != token.CONST && declaration.Tok != token.VAR {
		return
	}
	collectProseValueSpecs(set, declaration.Specs, occurrences)
}

func collectProseValueSpecs(set *token.FileSet, specs []ast.Spec, occurrences *[]productionProseOccurrence) {
	for _, spec := range specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for index, value := range valueSpec.Values {
			collectProseDataExpression(set, value, valueSpecTarget(valueSpec, index), occurrences)
		}
	}
}

func valueSpecTarget(spec *ast.ValueSpec, index int) string {
	if len(spec.Names) == 0 {
		return ""
	}
	if index >= len(spec.Names) {
		index = len(spec.Names) - 1
	}
	return spec.Names[index].Name
}

func collectFunctionProseData(set *token.FileSet, function *ast.FuncDecl, occurrences *[]productionProseOccurrence) {
	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			collectAssignmentProse(set, function.Name.Name, typed, occurrences)
		case *ast.DeclStmt:
			collectDeclarationStatementProse(set, typed, occurrences)
		case *ast.ReturnStmt:
			collectReturnProse(set, function.Name.Name, typed, occurrences)
		}
		return true
	})
}

func collectAssignmentProse(set *token.FileSet, fallback string, assignment *ast.AssignStmt, occurrences *[]productionProseOccurrence) {
	for index, value := range assignment.Rhs {
		target := fallback
		if index < len(assignment.Lhs) {
			target = expressionTarget(assignment.Lhs[index], fallback)
		}
		collectProseDataExpression(set, value, target, occurrences)
	}
}

func collectDeclarationStatementProse(set *token.FileSet, statement *ast.DeclStmt, occurrences *[]productionProseOccurrence) {
	declaration, ok := statement.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	collectProductionProseDeclaration(set, declaration, occurrences)
}

func collectReturnProse(set *token.FileSet, target string, statement *ast.ReturnStmt, occurrences *[]productionProseOccurrence) {
	for _, value := range statement.Results {
		collectProseDataExpression(set, value, target, occurrences)
	}
}

func collectProseDataExpression(set *token.FileSet, expression ast.Expr, target string, occurrences *[]productionProseOccurrence) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		collectProseLiteral(set, typed, target, occurrences)
	case *ast.BinaryExpr:
		collectProseDataExpression(set, typed.X, target, occurrences)
		collectProseDataExpression(set, typed.Y, target, occurrences)
	case *ast.ParenExpr:
		collectProseDataExpression(set, typed.X, target, occurrences)
	case *ast.CompositeLit:
		collectCompositeProse(set, typed, target, occurrences)
	}
}

func collectProseLiteral(set *token.FileSet, literal *ast.BasicLit, target string, occurrences *[]productionProseOccurrence) {
	if literal.Kind != token.STRING {
		return
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil || !proseLike(value) {
		return
	}
	*occurrences = append(*occurrences, productionProseOccurrence{
		position: set.Position(literal.Pos()),
		target:   target,
		runes:    utf8.RuneCountInString(value),
	})
}

func collectCompositeProse(set *token.FileSet, composite *ast.CompositeLit, target string, occurrences *[]productionProseOccurrence) {
	for _, element := range composite.Elts {
		if keyed, ok := element.(*ast.KeyValueExpr); ok {
			key := expressionTarget(keyed.Key, "")
			collectProseDataExpression(set, keyed.Value, combineTargets(target, key), occurrences)
			continue
		}
		collectProseDataExpression(set, element, target, occurrences)
	}
}

func expressionTarget(expression ast.Expr, fallback string) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	case *ast.BasicLit:
		return literalTarget(typed, fallback)
	default:
		return fallback
	}
}

func literalTarget(literal *ast.BasicLit, fallback string) string {
	if literal.Kind != token.STRING {
		return fallback
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return fallback
	}
	return value
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

func proseDataViolation(path string, first productionProseOccurrence, count, runes int) Violation {
	return Violation{
		Rule:   "production-prose-data",
		Path:   path,
		Line:   first.position.Line,
		Column: first.position.Column,
		Message: fmt.Sprintf(
			"%s contains %d long natural-language literals (%d runes) as explanation metadata; keep production state structural instead of accumulating explanatory prose",
			first.target, count, runes,
		),
	}
}
