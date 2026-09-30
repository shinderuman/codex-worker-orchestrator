package harnesslint

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
)

type duplicateTestBody struct {
	path string
	line int
	name string
	body string
}

func duplicateTestBodyViolations(root string, paths []string) ([]Violation, error) {
	byDirectory := make(map[string][]duplicateTestBody)
	for _, path := range goFiles(paths) {
		if !strings.HasSuffix(path, "_test.go") {
			continue
		}
		entries, err := duplicateTestBodiesInFile(root, path)
		if err != nil {
			return nil, err
		}
		directory := filepath.ToSlash(filepath.Dir(path))
		byDirectory[directory] = append(byDirectory[directory], entries...)
	}
	var violations []Violation
	for _, directory := range sortedDuplicateTestDirectories(byDirectory) {
		violations = append(violations, duplicateTestDirectoryViolations(byDirectory[directory])...)
	}
	return violations, nil
}

func duplicateTestBodiesInFile(root, path string) ([]duplicateTestBody, error) {
	data, err := readRegularFile(root, path)
	if err != nil {
		return nil, err
	}
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, path, data, 0)
	if err != nil {
		return nil, nil
	}
	var entries []duplicateTestBody
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil || !isGoTestEntrypoint(function.Name.Name) {
			continue
		}
		body, err := duplicateTestBodyText(set, function.Body)
		if err != nil {
			return nil, fmt.Errorf("format test body %s.%s: %w", path, function.Name.Name, err)
		}
		entries = append(entries, duplicateTestBody{
			path: path,
			line: set.Position(function.Pos()).Line,
			name: function.Name.Name,
			body: body,
		})
	}
	return entries, nil
}

func duplicateTestBodyText(set *token.FileSet, body *ast.BlockStmt) (string, error) {
	var buffer bytes.Buffer
	if err := format.Node(&buffer, set, body); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func sortedDuplicateTestDirectories(byDirectory map[string][]duplicateTestBody) []string {
	directories := make([]string, 0, len(byDirectory))
	for directory := range byDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	return directories
}

func duplicateTestDirectoryViolations(entries []duplicateTestBody) []Violation {
	sort.Slice(entries, func(left, right int) bool {
		if entries[left].path != entries[right].path {
			return entries[left].path < entries[right].path
		}
		return entries[left].line < entries[right].line
	})
	firstByBody := make(map[string]duplicateTestBody)
	var violations []Violation
	for _, entry := range entries {
		first, duplicate := firstByBody[entry.body]
		if !duplicate {
			firstByBody[entry.body] = entry
			continue
		}
		violations = append(violations, Violation{
			Rule:   "test-duplicate-body",
			Path:   entry.path,
			Line:   entry.line,
			Column: 1,
			Message: fmt.Sprintf(
				"test %s duplicates %s at %s:%d; extend or consolidate the existing test instead of adding a second identical entrypoint",
				entry.name,
				first.name,
				first.path,
				first.line,
			),
		})
	}
	return violations
}
