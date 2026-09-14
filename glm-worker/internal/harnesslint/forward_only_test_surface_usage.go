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

func forwardOnlyAliasDirectlyCalled(files []forwardOnlyParsedTestFile, name string) bool {
	for _, testFile := range files {
		called := false
		ast.Inspect(testFile.file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			identifier, ok := call.Fun.(*ast.Ident)
			if ok && identifier.Name == name {
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
				identifier, ok := target.(*ast.Ident)
				if ok && identifier.Name == name {
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
