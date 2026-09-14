package harnesslint

import (
	"go/ast"
	"go/token"
)

type forwardOnlyParsedTestFile struct {
	path string
	set  *token.FileSet
	file *ast.File
}

func scanForwardOnlyCompatibilityRule(root string, paths []string) ([]Violation, error) {
	violations, err := scanForwardOnlyCompatibility(root, paths)
	if err != nil {
		return nil, err
	}
	testSurfaceViolations, err := scanForwardOnlyTestCompatibilitySurfaces(root, paths)
	if err != nil {
		return nil, err
	}
	return append(violations, testSurfaceViolations...), nil
}

func forwardOnlyAliasCalledOutsideDeclarationFile(files []forwardOnlyParsedTestFile, declarationPath, name string) bool {
	for _, testFile := range files {
		if testFile.path == declarationPath {
			continue
		}
		called := false
		ast.Inspect(testFile.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := forwardOnlyUnparen(call.Fun).(*ast.Ident)
			if ok && forwardOnlyPackageAliasReference(testFile.file, identifier, name) {
				called = true
				return false
			}
			return true
		})
		if called {
			return true
		}
	}
	return false
}

func forwardOnlyAliasReassigned(files []forwardOnlyParsedTestFile, name string) bool {
	for _, testFile := range files {
		reassigned := false
		ast.Inspect(testFile.file, func(node ast.Node) bool {
			assignment, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assignment.Lhs {
				identifier, ok := forwardOnlyUnparen(target).(*ast.Ident)
				if ok && forwardOnlyPackageAliasReference(testFile.file, identifier, name) {
					reassigned = true
					return false
				}
			}
			return true
		})
		if reassigned {
			return true
		}
	}
	return false
}

func forwardOnlyPackageAliasReference(file *ast.File, identifier *ast.Ident, name string) bool {
	if identifier.Name != name {
		return false
	}
	if identifier.Obj == nil {
		return true
	}
	if file.Scope == nil {
		return false
	}
	return file.Scope.Objects[name] == identifier.Obj
}
