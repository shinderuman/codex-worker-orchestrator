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

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
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

type heredocSpec struct {
	word      string
	stripTabs bool
}

type sourceEdit struct {
	start int
	end   int
	blank bool
}

type shellLexState struct {
	single  bool
	double  bool
	escaped bool
}

const (
	statusFail  = "fail"
	sourceShell = "shell"
)

func Check(root string) (Report, error) {
	if !harnesslint.AppliesTo(root) {
		return Run(root, false)
	}
	quality, err := harnesslint.Check(root)
	if err != nil {
		return Report{}, err
	}
	return qualityReport(quality), nil
}

func qualityReport(source harnesslint.Report) Report {
	report := Report{
		Status:     source.Status,
		Fixed:      source.Fixed,
		Violations: make([]Violation, 0, len(source.Violations)),
	}
	for _, item := range source.Violations {
		report.Violations = append(report.Violations, Violation{
			Path:    item.Path,
			Line:    item.Line,
			Column:  item.Column,
			Kind:    item.Rule,
			Message: item.Message,
		})
	}
	return report
}

func Run(root string, fix bool) (Report, error) {
	paths, err := trackedAndUntracked(root)
	if err != nil {
		return Report{}, err
	}
	report, classified := classifyPaths(paths)
	if len(report.Violations) > 0 {
		report.Status = statusFail
		return sortedReport(report), nil
	}
	updates, err := scanClassifiedPaths(root, paths, classified, fix, &report)
	if err != nil {
		return Report{}, err
	}
	if err := applyPendingUpdates(root, updates); err != nil {
		return Report{}, err
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
		report.Status = statusFail
	}
	return sortedReport(report), nil
}

func classifyPaths(paths []string) (Report, map[string]string) {
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
	return report, classified
}

func scanClassifiedPaths(root string, paths []string, classified map[string]string, fix bool, report *Report) ([]pendingUpdate, error) {
	var updates []pendingUpdate
	for _, path := range paths {
		kind, eligible := classified[path]
		if !eligible {
			continue
		}
		data, mode, err := readRegular(root, path)
		if err != nil {
			return nil, err
		}
		findings := scan(kind, path, data)
		if fix && len(findings) > 0 {
			updates = append(updates, pendingUpdate{path: path, data: removeFindings(data, findings), mode: mode})
			report.Fixed += len(findings)
			continue
		}
		for _, item := range findings {
			report.Violations = append(report.Violations, item.Violation)
		}
	}
	return updates, nil
}

func applyPendingUpdates(root string, updates []pendingUpdate) error {
	for _, update := range updates {
		if err := replaceRegular(root, update); err != nil {
			return err
		}
	}
	return nil
}

func scan(kind, path string, data []byte) []finding {
	switch kind {
	case "go":
		return scanGo(path, data)
	case sourceShell:
		return scanShell(path, data)
	case "hash":
		return scanHash(path, data)
	case "gitignore":
		return scanGitignore(path, data)
	default:
		return nil
	}
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
		if len(part) == 0 {
			continue
		}
		path := filepath.ToSlash(string(part))
		_, statErr := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil {
			return nil, fmt.Errorf("lstat %s: %w", path, statErr)
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths, nil
}

func walkFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return filepath.SkipDir
			}
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
	extension := strings.ToLower(filepath.Ext(path))
	switch extension {
	case ".go", ".mod":
		return "go", true
	case ".sh":
		return sourceShell, true
	case ".toml", ".rules", ".yml", ".yaml":
		return "hash", true
	case ".gitignore":
		return "gitignore", true
	case ".md", ".json", ".txt", ".sum":
		return "", false
	}
	if base == "LICENSE" {
		return "", false
	}
	if extension == "" {
		if base == "commentlint" || base == "harnesslint" || path == ".githooks/post-merge" {
			return sourceShell, true
		}
	}
	return "unclassified", true
}

func readRegular(root, path string) ([]byte, os.FileMode, error) {
	absolute := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return nil, 0, fmt.Errorf("lstat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("refusing non-regular source %s", path)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, 0, fmt.Errorf("read %s: %w", path, err)
	}
	return data, info.Mode().Perm(), nil
}

func replaceRegular(root string, update pendingUpdate) error {
	absolute := filepath.Join(root, filepath.FromSlash(update.path))
	info, err := os.Lstat(absolute)
	if err != nil {
		return fmt.Errorf("lstat %s: %w", update.path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to replace non-regular source %s", update.path)
	}
	file, err := os.CreateTemp(filepath.Dir(absolute), ".commentlint-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer func() { _ = os.Remove(temp) }()
	if err := file.Chmod(update.mode); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(update.data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, absolute); err != nil {
		return fmt.Errorf("replace %s: %w", update.path, err)
	}
	return nil
}

func scanGo(path string, data []byte) []finding {
	set := token.NewFileSet()
	file := set.AddFile(path, -1, len(data))
	var lexer scanner.Scanner
	lexer.Init(file, data, nil, scanner.ScanComments)
	var findings []finding
	for {
		position, kind, literal := lexer.Scan()
		if kind == token.EOF {
			break
		}
		if kind != token.COMMENT {
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
	var pending []heredocSpec
	bodyStart := 0
	for index, line := range lines {
		if len(pending) > 0 {
			if heredocTerminated(pending[0], line.text) {
				pending = pending[1:]
				bodyStart = index + 1
			}
			continue
		}
		lineFindings, delimiters := scanShellCodeLine(path, line)
		findings = append(findings, lineFindings...)
		pending = append(pending, delimiters...)
		if len(pending) > 0 {
			bodyStart = index + 1
		}
	}
	if len(pending) > 0 && bodyStart < len(lines) {
		findings = append(findings, scanShellLines(path, lines[bodyStart:])...)
	}
	return findings
}

func scanShellCodeLine(path string, line shellLine) ([]finding, []heredocSpec) {
	commentAt := shellCommentOffset([]byte(line.text))
	code := line.text
	var findings []finding
	if commentAt >= 0 {
		if !allowedShellComment(line) {
			findings = append(findings, finding{
				Violation: Violation{Path: path, Line: line.number, Column: commentAt + 1, Kind: "comment", Message: "natural-language source comment is forbidden"},
				start:     line.offset + commentAt, end: line.offset + len(line.text),
			})
		}
		code = line.text[:commentAt]
	}
	return findings, heredocDelimiters(code)
}

func allowedShellComment(line shellLine) bool {
	return line.number == 1 && (line.text == "#!/bin/sh" || line.text == "#!/usr/bin/env bash")
}

func heredocTerminated(spec heredocSpec, line string) bool {
	if line == spec.word {
		return true
	}
	return spec.stripTabs && strings.TrimLeft(line, "\t") == spec.word
}

func heredocDelimiters(code string) []heredocSpec {
	var delimiters []heredocSpec
	state := shellLexState{}
	for index := 0; index < len(code); index++ {
		if state.consume(code[index]) || state.quoted() || !heredocStart(code, index) {
			continue
		}
		if tripleRedirect(code, index) {
			index += 2
			continue
		}
		delimiter, consumed := heredocDelimiter(code, index+2)
		if delimiter.word != "" {
			delimiters = append(delimiters, delimiter)
		}
		index = consumed - 1
	}
	return delimiters
}

func (s *shellLexState) consume(value byte) bool {
	if s.escaped {
		s.escaped = false
		return true
	}
	if value == '\\' && !s.single {
		s.escaped = true
		return true
	}
	if value == '\'' && !s.double {
		s.single = !s.single
		return true
	}
	if value == '"' && !s.single {
		s.double = !s.double
		return true
	}
	return false
}

func (s shellLexState) quoted() bool {
	return s.single || s.double
}

func heredocStart(code string, index int) bool {
	return code[index] == '<' && index+1 < len(code) && code[index+1] == '<'
}

func tripleRedirect(code string, index int) bool {
	return index+2 < len(code) && code[index+2] == '<'
}

func heredocDelimiter(code string, start int) (heredocSpec, int) {
	index, stripTabs := heredocPrefix(code, start)
	quote, index := heredocQuote(code, index)
	begin := index
	for index < len(code) && heredocWordByte(code[index], index == begin) {
		index++
	}
	if index == begin {
		return heredocSpec{}, start
	}
	end := index
	if quote != 0 && index < len(code) && code[index] == quote {
		index++
	}
	return heredocSpec{word: code[begin:end], stripTabs: stripTabs}, index
}

func heredocPrefix(code string, start int) (int, bool) {
	index := start
	stripTabs := false
	if index < len(code) && code[index] == '-' {
		stripTabs = true
		index++
	}
	for index < len(code) && (code[index] == ' ' || code[index] == '\t') {
		index++
	}
	return index, stripTabs
}

func heredocQuote(code string, index int) (byte, int) {
	if index >= len(code) || (code[index] != '\'' && code[index] != '"') {
		return 0, index
	}
	return code[index], index + 1
}

func heredocWordByte(value byte, first bool) bool {
	if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value == '_' {
		return true
	}
	return !first && value >= '0' && value <= '9'
}

func shellCommentOffset(line []byte) int {
	state := shellLexState{}
	for index, value := range line {
		if state.consume(value) || state.quoted() || value != '#' {
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
	state := shellLexState{}
	for index, value := range data {
		if value == '\n' {
			line++
			column = 1
			state.escaped = false
			continue
		}
		if scanHashState(&state, value) {
			column++
			continue
		}
		if value == '#' && !state.quoted() {
			end := bytes.IndexByte(data[index:], '\n')
			if end < 0 {
				end = len(data) - index
			}
			findings = append(findings, finding{
				Violation: Violation{Path: path, Line: line, Column: column, Kind: "comment", Message: "natural-language source comment is forbidden"},
				start:     index, end: index + end,
			})
		}
		column++
	}
	return findings
}

func scanHashState(state *shellLexState, value byte) bool {
	if state.escaped {
		state.escaped = false
		return true
	}
	if value == '\\' && state.double {
		state.escaped = true
		return true
	}
	if value == '\'' && !state.double {
		state.single = !state.single
		return true
	}
	if value == '"' && !state.single {
		state.double = !state.double
		return true
	}
	return false
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

func removeFindings(data []byte, findings []finding) []byte {
	var edits []sourceEdit
	for _, item := range findings {
		edits = append(edits, findingEdits(data, findings, item)...)
	}
	return applyEdits(data, edits)
}

func findingEdits(data []byte, findings []finding, item finding) []sourceEdit {
	var edits []sourceEdit
	decidedLine := -1
	decidedRemoval := false
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
	return edits
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

func IsViolation(report Report) bool {
	return report.Status == statusFail && len(report.Violations) > 0
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
