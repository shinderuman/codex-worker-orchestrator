package harnesslint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"strings"
)

type forwardOnlyPackageSurface struct {
	productionCallables    map[string]struct{}
	productionDeclarations map[string]struct{}
	testFiles              []forwardOnlyParsedTestFile
}

func scanForwardOnlyTestCompatibilitySurfaces(root string, paths []string) ([]Violation, error) {
	packages := make(map[string]*forwardOnlyPackageSurface)
	for _, filePath := range paths {
		if !strings.HasSuffix(filePath, ".go") || forwardOnlyFixturePath(filePath) {
			continue
		}
		data, err := readRegularFile(root, filePath)
		if err != nil {
			return nil, err
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, filePath, data, 0)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filePath, err)
		}
		key := path.Dir(filePath) + "\x00" + file.Name.Name
		pkg := packages[key]
		if pkg == nil {
			pkg = &forwardOnlyPackageSurface{
				productionCallables:    make(map[string]struct{}),
				productionDeclarations: make(map[string]struct{}),
			}
			packages[key] = pkg
		}
		if strings.HasSuffix(filePath, "_test.go") {
			pkg.testFiles = append(pkg.testFiles, forwardOnlyParsedTestFile{path: filePath, set: set, file: file})
			continue
		}
		collectForwardOnlyProductionSymbols(pkg, file)
	}

	var violations []Violation
	for _, pkg := range packages {
		for _, testFile := range pkg.testFiles {
			violations = append(violations, forwardOnlyTestAliasViolations(pkg, testFile)...)
		}
	}
	return violations, nil
}

func collectForwardOnlyProductionSymbols(pkg *forwardOnlyPackageSurface, file *ast.File) {
	for _, declaration := range file.Decls {
		collectForwardOnlyProductionDeclaration(pkg, declaration)
	}
}

func collectForwardOnlyProductionDeclaration(pkg *forwardOnlyPackageSurface, declaration ast.Decl) {
	switch typed := declaration.(type) {
	case *ast.FuncDecl:
		if typed.Recv == nil {
			pkg.productionDeclarations[typed.Name.Name] = struct{}{}
			pkg.productionCallables[typed.Name.Name] = struct{}{}
		}
	case *ast.GenDecl:
		collectForwardOnlyGeneralDeclarations(pkg, typed)
	}
}

func collectForwardOnlyGeneralDeclarations(pkg *forwardOnlyPackageSurface, declaration *ast.GenDecl) {
	for _, spec := range declaration.Specs {
		switch typed := spec.(type) {
		case *ast.TypeSpec:
			pkg.productionDeclarations[typed.Name.Name] = struct{}{}
		case *ast.ValueSpec:
			for _, name := range typed.Names {
				pkg.productionDeclarations[name.Name] = struct{}{}
			}
		}
	}
}

func forwardOnlyTestAliasViolations(pkg *forwardOnlyPackageSurface, testFile forwardOnlyParsedTestFile) []Violation {
	var violations []Violation
	for _, declaration := range testFile.file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.VAR {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			alias, target, ok := forwardOnlyDirectTestAlias(pkg, testFile.path, value)
			if !ok {
				continue
			}
			violations = append(violations, forwardOnlyViolation(testFile.set, testFile.path, alias,
				fmt.Sprintf("test-only package-level callable %s must not recreate an alternate production call surface by directly aliasing %s", alias.Name, target.Name)))
		}
	}
	return violations
}

func forwardOnlyDirectTestAlias(pkg *forwardOnlyPackageSurface, declarationPath string, value *ast.ValueSpec) (*ast.Ident, *ast.Ident, bool) {
	if value.Type != nil || len(value.Names) != 1 || len(value.Values) != 1 {
		return nil, nil, false
	}
	alias := value.Names[0]
	if !ast.IsExported(alias.Name) {
		return nil, nil, false
	}
	if _, exists := pkg.productionDeclarations[alias.Name]; exists {
		return nil, nil, false
	}
	target, ok := value.Values[0].(*ast.Ident)
	if !ok || target.Name == alias.Name {
		return nil, nil, false
	}
	if _, exists := pkg.productionCallables[target.Name]; !exists {
		return nil, nil, false
	}
	if !forwardOnlyAliasCalledOutsideDeclarationFile(pkg.testFiles, declarationPath, alias.Name) || forwardOnlyAliasReassigned(pkg.testFiles, alias.Name) {
		return nil, nil, false
	}
	return alias, target, true
}
