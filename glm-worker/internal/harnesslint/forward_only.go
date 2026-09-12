package harnesslint

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

const forwardOnlyCompatibilityRule = "forward-only-compatibility"

var shellPathLookupAssignment = regexp.MustCompile(`(?m)^[\t ]*([A-Za-z_][A-Za-z0-9_]*)=\$\((?:command[\t ]+-v|which)[^)]*\)[\t ]*$`)

func scanForwardOnlyCompatibility(root string, paths []string) ([]Violation, error) {
	var violations []Violation
	for _, path := range paths {
		if forwardOnlyFixturePath(path) {
			continue
		}
		switch {
		case strings.HasSuffix(path, ".go"):
			current, err := forwardOnlyGoFileViolations(root, path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, current...)
		case isShellPath(path):
			data, err := readRegularFile(root, path)
			if err != nil {
				return nil, err
			}
			violations = append(violations, forwardOnlyShellViolations(path, data)...)
		}
	}
	return violations, nil
}

func forwardOnlyFixturePath(path string) bool {
	return strings.HasPrefix(path, "glm-worker/internal/harnesslint/")
}

func forwardOnlyGoFileViolations(root, path string) ([]Violation, error) {
	data, err := readRegularFile(root, path)
	if err != nil {
		return nil, err
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, data, 0)
	if err != nil {
		return nil, nil
	}
	if strings.HasSuffix(path, "_test.go") {
		return forwardOnlyCompatibilityTestViolations(set, file, path), nil
	}
	return forwardOnlyProductionGoViolations(set, file, path), nil
}

func forwardOnlyProductionGoViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		violations = append(violations, legacyMigrationDeclarationViolations(set, function, path)...)
		violations = append(violations, legacyMigrationCallViolations(set, function.Body, path)...)
		violations = append(violations, legacyAbsencePromotionViolations(set, function.Body, path)...)
		if schemaReaderName(function.Name.Name) {
			violations = append(violations, schemaRangeViolations(set, function.Body, path)...)
			violations = append(violations, schemaPromotionViolations(set, function.Body, path)...)
		}
	}
	return violations
}

func legacyMigrationDeclarationViolations(set *token.FileSet, function *ast.FuncDecl, path string) []Violation {
	if !legacyMigrationName(function.Name.Name) {
		return nil
	}
	return []Violation{forwardOnlyViolation(set, path, function.Name, "legacy migration/promotion helpers must not exist in production code")}
}

func legacyMigrationCallViolations(set *token.FileSet, body *ast.BlockStmt, path string) []Violation {
	var violations []Violation
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !legacyMigrationName(callableName(call.Fun)) {
			return true
		}
		violations = append(violations, forwardOnlyViolation(set, path, call, "production code must not call legacy migration/promotion helpers"))
		return true
	})
	return violations
}

func legacyMigrationName(name string) bool {
	lower := strings.ToLower(name)
	if !strings.Contains(lower, "legacy") {
		return false
	}
	return containsAny(lower, "migrat", "promot", "upgrade", "adopt")
}

func callableName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

func schemaReaderName(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "decode", "parse", "load", "unmarshal")
}

func schemaRangeViolations(set *token.FileSet, body *ast.BlockStmt, path string) []Violation {
	var violations []Violation
	ast.Inspect(body, func(node ast.Node) bool {
		comparison, ok := node.(*ast.BinaryExpr)
		if !ok || !orderedComparison(comparison.Op) {
			return true
		}
		if !containsSchemaReference(comparison.X) && !containsSchemaReference(comparison.Y) {
			return true
		}
		violations = append(violations, forwardOnlyViolation(set, path, comparison, "machine schema readers must use exact version/revision matching, not ordered compatibility ranges"))
		return true
	})
	return violations
}

func orderedComparison(operator token.Token) bool {
	return operator == token.LSS || operator == token.LEQ || operator == token.GTR || operator == token.GEQ
}

func schemaPromotionViolations(set *token.FileSet, body *ast.BlockStmt, path string) []Violation {
	var violations []Violation
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.IfStmt:
			violations = append(violations, schemaIfPromotionViolations(set, path, typed)...)
		case *ast.SwitchStmt:
			violations = append(violations, schemaSwitchPromotionViolations(set, path, typed)...)
		}
		return true
	})
	return violations
}

func schemaIfPromotionViolations(set *token.FileSet, path string, branch *ast.IfStmt) []Violation {
	kinds := schemaKinds(branch.Cond)
	if len(kinds) == 0 {
		return nil
	}
	return schemaAssignmentViolations(set, path, branch.Body, kinds)
}

func schemaSwitchPromotionViolations(set *token.FileSet, path string, branch *ast.SwitchStmt) []Violation {
	kinds := schemaKinds(branch.Tag)
	if len(kinds) == 0 || branch.Body == nil {
		return nil
	}
	var violations []Violation
	for _, statement := range branch.Body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		block := &ast.BlockStmt{List: clause.Body}
		violations = append(violations, schemaAssignmentViolations(set, path, block, kinds)...)
	}
	return violations
}

func schemaAssignmentViolations(set *token.FileSet, path string, block *ast.BlockStmt, kinds map[string]bool) []Violation {
	var violations []Violation
	ast.Inspect(block, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, target := range assignment.Lhs {
			kind := schemaReferenceKind(target)
			if kind == "" || !kinds[kind] || !assignmentPromotesCurrentSchema(assignment, index, kind) {
				continue
			}
			violations = append(violations, forwardOnlyViolation(set, path, target, "old machine schema values must not be rewritten or promoted to the current schema"))
		}
		return true
	})
	return violations
}

func assignmentPromotesCurrentSchema(assignment *ast.AssignStmt, index int, kind string) bool {
	if len(assignment.Rhs) == 0 {
		return false
	}
	value := assignment.Rhs[0]
	if len(assignment.Rhs) == len(assignment.Lhs) {
		value = assignment.Rhs[index]
	}
	return currentSchemaValue(value, kind)
}

func currentSchemaValue(expression ast.Expr, kind string) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return schemaConstantName(typed.Name, kind)
	case *ast.SelectorExpr:
		return schemaConstantName(typed.Sel.Name, kind)
	case *ast.BasicLit:
		return typed.Kind == token.INT && typed.Value != "0"
	default:
		return false
	}
}

func schemaConstantName(name, kind string) bool {
	lower := strings.ToLower(name)
	if kind == "version" {
		return lower != "version" && strings.Contains(lower, "version")
	}
	return lower != "schemarevision" && strings.Contains(lower, "revision")
}

func schemaKinds(node ast.Node) map[string]bool {
	result := make(map[string]bool)
	if node == nil {
		return result
	}
	ast.Inspect(node, func(current ast.Node) bool {
		expression, ok := current.(ast.Expr)
		if !ok {
			return true
		}
		if kind := schemaReferenceKind(expression); kind != "" {
			result[kind] = true
		}
		return true
	})
	return result
}

func containsSchemaReference(expression ast.Expr) bool {
	return len(schemaKinds(expression)) > 0
}

func schemaReferenceKind(expression ast.Expr) string {
	var name string
	switch typed := expression.(type) {
	case *ast.Ident:
		name = typed.Name
	case *ast.SelectorExpr:
		name = typed.Sel.Name
	default:
		return ""
	}
	switch strings.ToLower(name) {
	case "version":
		return "version"
	case "schemarevision":
		return "schema_revision"
	default:
		return ""
	}
}

func legacyAbsencePromotionViolations(set *token.FileSet, body *ast.BlockStmt, path string) []Violation {
	var violations []Violation
	ast.Inspect(body, func(node ast.Node) bool {
		branch, ok := node.(*ast.IfStmt)
		if !ok || !absenceCondition(branch.Cond) || !nodeReferencesLegacy(branch.Body) || !nodeMutatesCurrentState(branch.Body) {
			return true
		}
		violations = append(violations, forwardOnlyViolation(set, path, branch, "missing current state/path must not be reconstructed or populated from legacy provenance"))
		return true
	})
	return violations
}

func absenceCondition(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.UnaryExpr:
			if typed.Op == token.NOT && absenceName(expressionName(typed.X)) {
				found = true
			}
		case *ast.SelectorExpr:
			if strings.EqualFold(typed.Sel.Name, "ErrNotExist") {
				found = true
			}
		case *ast.BinaryExpr:
			if binaryIndicatesAbsence(typed) {
				found = true
			}
		}
		return !found
	})
	return found
}

func binaryIndicatesAbsence(expression *ast.BinaryExpr) bool {
	if expression.Op != token.EQL {
		return false
	}
	if identifierLiteral(expression.Y, "nil", "false") {
		return absenceName(expressionName(expression.X))
	}
	if identifierLiteral(expression.X, "nil", "false") {
		return absenceName(expressionName(expression.Y))
	}
	return false
}

func identifierLiteral(expression ast.Expr, values ...string) bool {
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return false
	}
	for _, value := range values {
		if identifier.Name == value {
			return true
		}
	}
	return false
}

func absenceName(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "exists", "present", "found", "available", "canonical", "currentstate", "ownershipstate")
}

func expressionName(expression ast.Expr) string {
	switch typed := expression.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		return typed.Sel.Name
	default:
		return ""
	}
}

func nodeReferencesLegacy(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		identifier, ok := current.(*ast.Ident)
		if ok && strings.Contains(strings.ToLower(identifier.Name), "legacy") {
			found = true
		}
		return !found
	})
	return found
}

func nodeMutatesCurrentState(node ast.Node) bool {
	mutates := false
	ast.Inspect(node, func(current ast.Node) bool {
		switch typed := current.(type) {
		case *ast.CallExpr:
			if mutationCallName(callableName(typed.Fun)) {
				mutates = true
			}
		case *ast.AssignStmt:
			for _, target := range typed.Lhs {
				if currentStateTarget(expressionName(target)) {
					mutates = true
					break
				}
			}
		}
		return !mutates
	})
	return mutates
}

func mutationCallName(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "save", "write", "commit", "install", "copy", "rename", "persist", "store", "record", "adopt", "promot", "migrat")
}

func currentStateTarget(name string) bool {
	lower := strings.ToLower(name)
	return containsAny(lower, "state", "ownership", "owner", "record", "canonical", "current")
}

func forwardOnlyCompatibilityTestViolations(set *token.FileSet, file *ast.File, path string) []Violation {
	var violations []Violation
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(function.Name.Name, "Test") || !compatibilityAcceptanceTestName(function.Name.Name) {
			continue
		}
		violations = append(violations, forwardOnlyViolation(set, path, function.Name, "tests must not guarantee acceptance, migration, or promotion of old/legacy input"))
	}
	return violations
}

func compatibilityAcceptanceTestName(name string) bool {
	lower := strings.ToLower(name)
	if containsAny(lower, "reject", "refus", "unsupported", "doesnot", "failclosed", "skip", "reset", "delete", "nonresum") {
		return false
	}
	historical := containsAny(lower, "legacy", "oldversion", "oldversions", "oldschema", "oldrevision", "backwardcompat")
	acceptance := containsAny(lower, "accept", "migrat", "promot", "upgrade", "compatib", "support", "adopt")
	return historical && acceptance
}

func forwardOnlyShellViolations(path string, data []byte) []Violation {
	text := string(data)
	matches := shellPathLookupAssignment.FindAllStringSubmatchIndex(text, -1)
	var violations []Violation
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}
		variable := text[match[2]:match[3]]
		if !shellCopiesVariable(text[match[1]:], variable) {
			continue
		}
		line := 1 + strings.Count(text[:match[0]], "\n")
		violations = append(violations, Violation{
			Rule: forwardOnlyCompatibilityRule, Path: path, Line: line, Column: 1,
			Message: "PATH-discovered executables must not be copied or promoted into canonical managed locations",
		})
	}
	return violations
}

func shellCopiesVariable(text, variable string) bool {
	pattern := regexp.MustCompile(`(?m)^[\t ]*(?:cp|mv|install|rsync)\b[^\n]*\$\{?` + regexp.QuoteMeta(variable) + `\}?\b`)
	return pattern.MatchString(text)
}

func forwardOnlyViolation(set *token.FileSet, path string, node ast.Node, message string) Violation {
	position := set.Position(node.Pos())
	return Violation{Rule: forwardOnlyCompatibilityRule, Path: path, Line: position.Line, Column: position.Column, Message: message}
}
