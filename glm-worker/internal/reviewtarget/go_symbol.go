package reviewtarget

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

type GoDeclaration struct {
	Locator   string
	Kind      string
	LineStart int
	LineEnd   int
}

func FindGoDeclaration(content []byte, locator string) (GoDeclaration, error) {
	owner, symbol, ok := parseGoSymbolLocator(locator)
	if !ok {
		return GoDeclaration{}, fmt.Errorf("invalid Go symbol locator %q", locator)
	}
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, "review-target.go", content, parser.SkipObjectResolution)
	if err != nil {
		return GoDeclaration{}, fmt.Errorf("parse Go source for %q: %w", locator, err)
	}
	matches := findGoDeclarations(fileset, file, owner, symbol)
	if len(matches) == 0 {
		return GoDeclaration{}, fmt.Errorf("Go declaration %q is not present", locator)
	}
	if len(matches) != 1 {
		return GoDeclaration{}, fmt.Errorf("Go declaration %q is ambiguous", locator)
	}
	return matches[0], nil
}

func findGoDeclarations(fileset *token.FileSet, file *ast.File, owner, symbol string) []GoDeclaration {
	var matches []GoDeclaration
	for _, declaration := range file.Decls {
		if owner == "" {
			matches = append(matches, topLevelGoDeclaration(fileset, declaration, symbol)...)
			continue
		}
		matches = append(matches, goMemberDeclarations(fileset, declaration, owner, symbol)...)
	}
	return matches
}

func topLevelGoDeclaration(fileset *token.FileSet, declaration ast.Decl, symbol string) []GoDeclaration {
	switch current := declaration.(type) {
	case *ast.FuncDecl:
		if current.Recv == nil && current.Name.Name == symbol {
			return []GoDeclaration{goDeclaration(fileset, symbol, "func", current.Pos(), current.End())}
		}
	case *ast.GenDecl:
		var matches []GoDeclaration
		for _, spec := range current.Specs {
			switch typed := spec.(type) {
			case *ast.TypeSpec:
				if typed.Name.Name == symbol {
					matches = append(matches, goDeclaration(fileset, symbol, "type", typed.Pos(), typed.End()))
				}
			case *ast.ValueSpec:
				for _, name := range typed.Names {
					if name.Name == symbol {
						matches = append(matches, goDeclaration(fileset, symbol, current.Tok.String(), typed.Pos(), typed.End()))
					}
				}
			}
		}
	}
	return nil
}

func goMemberDeclarations(fileset *token.FileSet, declaration ast.Decl, owner, symbol string) []GoDeclaration {
	switch current := declaration.(type) {
	case *ast.FuncDecl:
		if current.Recv != nil && current.Name.Name == symbol && receiverTypeName(current.Recv) == owner {
			return []GoDeclaration{goDeclaration(fileset, owner+"."+symbol, "method", current.Pos(), current.End())}
		}
	case *ast.GenDecl:
		var matches []GoDeclaration
		for _, spec := range current.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != owner {
				continue
			}
			matches = append(matches, goTypeMemberDeclarations(fileset, typeSpec.Type, owner, symbol)...)
		}
		return matches
	}
	return nil
}

func goTypeMemberDeclarations(fileset *token.FileSet, typ ast.Expr, owner, symbol string) []GoDeclaration {
	var fields *ast.FieldList
	kind := "member"
	switch current := typ.(type) {
	case *ast.StructType:
		fields = current.Fields
		kind = "field"
	case *ast.InterfaceType:
		fields = current.Methods
		kind = "interface-method"
	default:
		return nil
	}
	var matches []GoDeclaration
	for _, field := range fields.List {
		for _, name := range field.Names {
			if name.Name == symbol {
				matches = append(matches, goDeclaration(fileset, owner+"."+symbol, kind, field.Pos(), field.End()))
			}
		}
	}
	return matches
}

func receiverTypeName(fields *ast.FieldList) string {
	if fields == nil || len(fields.List) != 1 {
		return ""
	}
	return receiverExprName(fields.List[0].Type)
}

func receiverExprName(expr ast.Expr) string {
	switch current := expr.(type) {
	case *ast.Ident:
		return current.Name
	case *ast.StarExpr:
		return receiverExprName(current.X)
	case *ast.ParenExpr:
		return receiverExprName(current.X)
	case *ast.IndexExpr:
		return receiverExprName(current.X)
	case *ast.IndexListExpr:
		return receiverExprName(current.X)
	default:
		return ""
	}
}

func goDeclaration(fileset *token.FileSet, locator, kind string, start, end token.Pos) GoDeclaration {
	return GoDeclaration{
		Locator:   locator,
		Kind:      kind,
		LineStart: fileset.Position(start).Line,
		LineEnd:   fileset.Position(end).Line,
	}
}
