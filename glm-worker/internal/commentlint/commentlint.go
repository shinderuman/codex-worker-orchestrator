package commentlint

import (
	"bytes"
	"errors"
	"fmt"
	"go/build/constraint"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Violation struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

type Report struct {
	Status     string      `json:"status"`
	Fixed      int         `json:"fixed"`
	Violations []Violation `json:"violations"`
}

type finding struct {
	Violation
	start int
	end   int
}

type pendingUpdate struct {
	path string
	data []byte
	mode os.FileMode
}

type shellLine struct {
	text   string
	number int
	offset int
}

func Check(root string) (Report, error) {
	return Run(root, false)
}

func Run(root string, fix bool) (Report, error) {
	paths, err := trackedAndUntracked(root)
	if err != nil {
		return Report{}, err
	}
	report := Report{Status: "pass", Violations: []Violation{}}
	classified := make(map[string]string, len(paths))
	for _, path := range paths {
		kind, eligible := classify(path)
		if !eligible {
			continue
		}
		if kind == "unclassified" {
			report.Violations = append(report.Violations, Violation{Path: path, Line: 1, Column: 1, Kind: kind, Message: "source candidate is not classified"})
			continue
		}
		classified[path] = kind
	}
	if len(report.Violations) > 0 {
		report.Status = "fail"
		return sortedReport(report), nil
	}
	updates := []pendingUpdate{}
	for _, path := range paths {
		kind, eligible := classified[path]
		if !eligible {
			continue
		}
		absolute := filepath.Join(root, filepath.FromSlash(path))
		data, err := os.ReadFile(absolute)
		if err != nil {
			return Report{}, fmt.Errorf("read %s: %w", path, err)
		}
		var findings []finding
		switch kind {
		case "go":
			findings = scanGo(path, data)
		case "shell":
			findings = scanShell(path, data)
		case "toml":
			findings = scanHash(path, data)
		case "gitignore":
			findings = scanGitignore(path, data)
		}
		if fix && len(findings) > 0 {
			updated := removeFindings(data, findings)
			updates = append(updates, pendingUpdate{path: absolute, data: updated, mode: fileMode(absolute)})
			report.Fixed += len(findings)
			continue
		}
		for _, item := range findings {
			report.Violations = append(report.Violations, item.Violation)
		}
	}
	for _, update := range updates {
		if err := os.WriteFile(update.path, update.data, update.mode); err != nil {
			return Report{}, fmt.Errorf("write %s: %w", update.path, err)
		}
	}
	if fix && report.Fixed > 0 {
		checked, err := Run(root, false)
		if err != nil {
			return Report{}, err
		}
		checked.Fixed = report.Fixed
		return checked, nil
	}
	if len(report.Violations) > 0 {
		report.Status = "fail"
	}
	return sortedReport(report), nil
}

func sortedReport(report Report) Report {
	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].Path != report.Violations[j].Path {
			return report.Violations[i].Path < report.Violations[j].Path
		}
		if report.Violations[i].Line != report.Violations[j].Line {
			return report.Violations[i].Line < report.Violations[j].Line
		}
		return report.Violations[i].Column < report.Violations[j].Column
	})
	return report
}

func trackedAndUntracked(root string) ([]string, error) {
	command := exec.Command("git", "-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	data, err := command.Output()
	if err != nil {
		return walkFiles(root)
	}
	parts := bytes.Split(data, []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			paths = append(paths, filepath.ToSlash(string(part)))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func walkFiles(root string) ([]string, error) {
	paths := []string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		paths = append(paths, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk repository files: %w", err)
	}
	sort.Strings(paths)
	return paths, nil
}

func classify(path string) (string, bool) {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".mod":
		return "go", true
	case ".sh":
		return "shell", true
	case ".toml", ".rules":
		return "toml", true
	case ".gitignore":
		return "gitignore", true
	case ".md", ".json", ".txt":
		return "", false
	}
	if base == "LICENSE" {
		return "", false
	}
	if ext == "" {
		if base == "commentlint" || path == ".githooks/post-merge" {
			return "shell", true
		}
	}
	return "unclassified", true
}

func scanGo(path string, data []byte) []finding {
	set := token.NewFileSet()
	file := set.AddFile(path, -1, len(data))
	var lexer scanner.Scanner
	lexer.Init(file, data, nil, scanner.ScanComments)
	var findings []finding
	for {
		position, tokenKind, literal := lexer.Scan()
		if tokenKind == token.EOF {
			break
		}
		if tokenKind != token.COMMENT {
			continue
		}
		pos := set.Position(position)
		if pos.Line == 1 && strings.HasPrefix(literal, "//go:build ") {
			if _, err := constraint.Parse(literal); err == nil {
				continue
			}
		}
		findings = append(findings, finding{Violation: Violation{Path: path, Line: pos.Line, Column: pos.Column, Kind: "comment", Message: "natural-language source comment is forbidden"}, start: pos.Offset, end: pos.Offset + len(literal)})
	}
	return findings
}

func scanShell(path string, data []byte) []finding {
	lines := make([]shellLine, 0, bytes.Count(data, []byte("\n"))+1)
	offset := 0
	for index, raw := range bytes.Split(data, []byte("\n")) {
		lines = append(lines, shellLine{text: strings.TrimSuffix(string(raw), "\r"), number: index + 1, offset: offset})
		offset += len(raw) + 1
	}
	return scanShellLines(path, lines)
}

func scanShellLines(path string, lines []shellLine) []finding {
	var findings []finding
	pending := []string{}
	bodyStart := 0
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		if len(pending) > 0 {
			if heredocTerminated(pending[0], line.text) {
				pending = pending[1:]
				bodyStart = index + 1
			}
			continue
		}
		commentAt := shellCommentOffset([]byte(line.text))
		code := line.text
		if commentAt >= 0 {
			allowed := line.number == 1 && (line.text == "#!/bin/sh" || line.text == "#!/usr/bin/env bash")
			if !allowed {
				findings = append(findings, finding{Violation: Violation{Path: path, Line: line.number, Column: commentAt + 1, Kind: "comment", Message: "natural-language source comment is forbidden"}, start: line.offset + commentAt, end: line.offset + len(line.text)})
			}
			code = line.text[:commentAt]
		}
		if delimiters := heredocDelimiters(code); len(delimiters) > 0 {
			pending = append(pending, delimiters...)
			bodyStart = index + 1
		}
	}
	if len(pending) > 0 && bodyStart < len(lines) {
		findings = append(findings, scanShellLines(path, lines[bodyStart:])...)
	}
	return findings
}

func heredocTerminated(word string, line string) bool {
	return line == word || strings.TrimPrefix(line, "\t") == word
}

func heredocDelimiters(code string) []string {
	var delimiters []string
	single := false
	double := false
	escaped := false
	for index := 0; index < len(code); index++ {
		value := code[index]
		if escaped {
			escaped = false
			continue
		}
		if value == '\\' && !single {
			escaped = true
			continue
		}
		if value == '\'' && !double {
			single = !single
			continue
		}
		if value == '"' && !single {
			double = !double
			continue
		}
		if single || double || value != '<' || index+1 >= len(code) || code[index+1] != '<' {
			continue
		}
		delimiter, consumed := heredocDelimiter(code, index+2)
		if delimiter != "" {
			delimiters = append(delimiters, delimiter)
		}
		index = consumed - 1
	}
	return delimiters
}

func heredocDelimiter(code string, start int) (string, int) {
	index := start
	if index < len(code) && code[index] == '-' {
		index++
	}
	for index < len(code) && (code[index] == ' ' || code[index] == '\t') {
		index++
	}
	quote := byte(0)
	if index < len(code) && (code[index] == '\'' || code[index] == '"') {
		quote = code[index]
		index++
	}
	begin := index
	for index < len(code) && heredocWordByte(code[index], index == begin) {
		index++
	}
	if index == begin {
		return "", start
	}
	end := index
	if quote != 0 && index < len(code) && code[index] == quote {
		index++
	}
	return code[begin:end], index
}

func heredocWordByte(value byte, first bool) bool {
	if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '_' {
		return true
	}
	return !first && value >= '0' && value <= '9'
}

func shellCommentOffset(line []byte) int {
	single := false
	double := false
	escaped := false
	for index, value := range line {
		if escaped {
			escaped = false
			continue
		}
		if value == '\\' && !single {
			escaped = true
			continue
		}
		if value == '\'' && !double {
			single = !single
			continue
		}
		if value == '"' && !single {
			double = !double
			continue
		}
		if value != '#' || single || double {
			continue
		}
		if index == 0 || strings.ContainsRune(" \t;|&() <>", rune(line[index-1])) {
			return index
		}
	}
	return -1
}

func scanHash(path string, data []byte) []finding {
	var findings []finding
	line := 1
	column := 1
	single := false
	double := false
	escaped := false
	for index, value := range data {
		if value == '\n' {
			line++
			column = 1
			escaped = false
			continue
		}
		if escaped {
			escaped = false
			column++
			continue
		}
		if value == '\\' && double {
			escaped = true
			column++
			continue
		}
		if value == '\'' && !double {
			single = !single
		} else if value == '"' && !single {
			double = !double
		} else if value == '#' && !single && !double {
			end := bytes.IndexByte(data[index:], '\n')
			if end < 0 {
				end = len(data) - index
			}
			findings = append(findings, finding{Violation: Violation{Path: path, Line: line, Column: column, Kind: "comment", Message: "natural-language source comment is forbidden"}, start: index, end: index + end})
		}
		column++
	}
	return findings
}

func scanGitignore(path string, data []byte) []finding {
	var findings []finding
	offset := 0
	for lineNumber, line := range bytes.Split(data, []byte("\n")) {
		if len(line) > 0 && line[0] == '#' {
			findings = append(findings, finding{Violation: Violation{Path: path, Line: lineNumber + 1, Column: 1, Kind: "comment", Message: "natural-language source comment is forbidden"}, start: offset, end: offset + len(line)})
		}
		offset += len(line) + 1
	}
	return findings
}

type sourceEdit struct {
	start int
	end   int
	blank bool
}

func removeFindings(data []byte, findings []finding) []byte {
	edits := []sourceEdit{}
	decidedLine := -1
	decidedRemoval := false
	for _, item := range findings {
		for cursor := item.start; cursor < item.end; {
			lineStart, contentEnd, nextLine := lineBounds(data, cursor)
			if lineStart != decidedLine {
				decidedLine = lineStart
				decidedRemoval = lineCommentsOnly(data, findings, lineStart, contentEnd) && !endsWithLineContinuation(data, lineStart)
			}
			if decidedRemoval {
				if last := len(edits) - 1; last < 0 || edits[last].blank || edits[last].start != lineStart {
					edits = append(edits, sourceEdit{start: lineStart, end: nextLine})
				}
			} else {
				edits = append(edits, sourceEdit{start: cursor, end: min(contentEnd, item.end), blank: true})
			}
			cursor = nextLine
		}
	}
	return applyEdits(data, edits)
}

func lineBounds(data []byte, cursor int) (int, int, int) {
	lineStart := 0
	if index := bytes.LastIndexByte(data[:cursor], '\n'); index >= 0 {
		lineStart = index + 1
	}
	lineFeed := bytes.IndexByte(data[lineStart:], '\n')
	if lineFeed < 0 {
		return lineStart, len(data), len(data)
	}
	lineFeed += lineStart
	contentEnd := lineFeed
	if contentEnd > lineStart && data[contentEnd-1] == '\r' {
		contentEnd--
	}
	return lineStart, contentEnd, lineFeed + 1
}

func lineCommentsOnly(data []byte, findings []finding, start, end int) bool {
	cursor := start
	for _, item := range findings {
		if item.end <= cursor {
			continue
		}
		if item.start >= end {
			break
		}
		if item.start > cursor && !onlyWhitespace(data[cursor:item.start]) {
			return false
		}
		cursor = max(cursor, item.end)
		if cursor >= end {
			return true
		}
	}
	return onlyWhitespace(data[cursor:end])
}

func onlyWhitespace(data []byte) bool {
	for _, value := range data {
		if value != ' ' && value != '\t' {
			return false
		}
	}
	return true
}

func endsWithLineContinuation(data []byte, lineStart int) bool {
	backslashes := 0
	for index := lineStart - 2; index >= 0 && data[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func applyEdits(data []byte, edits []sourceEdit) []byte {
	result := make([]byte, 0, len(data))
	position := 0
	for _, edit := range edits {
		result = append(result, data[position:edit.start]...)
		if edit.blank {
			result = append(result, bytes.Repeat([]byte{' '}, edit.end-edit.start)...)
		}
		position = edit.end
	}
	return append(result, data[position:]...)
}

func fileMode(path string) os.FileMode {
	info, err := os.Stat(path)
	if err != nil {
		return 0o644
	}
	return info.Mode().Perm()
}

func IsViolation(report Report) bool {
	return report.Status == "fail" && len(report.Violations) > 0
}

func ValidateRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("repository root is not a directory")
	}
	return nil
}
