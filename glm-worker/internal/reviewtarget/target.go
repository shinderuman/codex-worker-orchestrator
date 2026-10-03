package reviewtarget

import (
	"fmt"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

type LocatorKind string

type Target struct {
	Path      string
	Locator   string
	Kind      LocatorKind
	LineStart int
	LineEnd   int
	Owner     string
	Symbol    string
}

type ParseError struct {
	Code       string
	Target     string
	Correction string
}

const (
	WholeFileDiffLocator = "@diff"
	MaxSourceProofLines  = 2000
	canonicalCorrection  = "use repository-relative path:N, path:N-M, path:@diff, or for Go source path:TopLevel / path:Type.Member"

	LocatorLineRange LocatorKind = "line_range"
	LocatorWholeDiff LocatorKind = "whole_diff"
	LocatorGoSymbol  LocatorKind = "go_symbol"
)

func (e *ParseError) Error() string {
	return fmt.Sprintf("review target [%s] %q is not canonical; correction=%s", e.Code, e.Target, e.Correction)
}

func Parse(raw string) (string, string, error) {
	target, err := ParseTarget(raw)
	if err != nil {
		return "", "", err
	}
	return target.Path, target.Locator, nil
}

func ParseTarget(raw string) (Target, error) {
	raw = strings.TrimSpace(raw)
	separator := strings.Index(raw, ":")
	if separator <= 0 || separator == len(raw)-1 {
		return Target{}, parseError("shape", raw)
	}
	path := strings.TrimSpace(raw[:separator])
	locator := strings.TrimSpace(raw[separator+1:])
	if path == "" || locator == "" || !relativePath(path) || strings.ContainsAny(path, " ,()") {
		return Target{}, parseError("path", raw)
	}
	if start, end, ok := ParseLineRange(locator); ok {
		return Target{Path: path, Locator: locator, Kind: LocatorLineRange, LineStart: start, LineEnd: end}, nil
	}
	if locator == WholeFileDiffLocator {
		return Target{Path: path, Locator: locator, Kind: LocatorWholeDiff}, nil
	}
	if !strings.HasSuffix(path, ".go") {
		return Target{}, parseError("locator-language", raw)
	}
	owner, symbol, ok := parseGoSymbolLocator(locator)
	if !ok {
		return Target{}, parseError("locator", raw)
	}
	return Target{Path: path, Locator: locator, Kind: LocatorGoSymbol, Owner: owner, Symbol: symbol}, nil
}

func ParseLineRange(locator string) (int, int, bool) {
	if locator == "" || strings.TrimSpace(locator) != locator {
		return 0, 0, false
	}
	parts := strings.Split(locator, "-")
	if len(parts) > 2 || parts[0] == "" {
		return 0, 0, false
	}
	start, err := strconv.Atoi(parts[0])
	if err != nil || start <= 0 || strconv.Itoa(start) != parts[0] {
		return 0, 0, false
	}
	if len(parts) == 1 {
		return start, start, true
	}
	end, err := strconv.Atoi(parts[1])
	if err != nil || end < start || strconv.Itoa(end) != parts[1] {
		return 0, 0, false
	}
	return start, end, true
}

func parseGoSymbolLocator(locator string) (string, string, bool) {
	parts := strings.Split(locator, ".")
	switch len(parts) {
	case 1:
		if token.IsIdentifier(parts[0]) {
			return "", parts[0], true
		}
	case 2:
		if token.IsIdentifier(parts[0]) && token.IsIdentifier(parts[1]) {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func parseError(code, raw string) error {
	return &ParseError{Code: code, Target: raw, Correction: canonicalCorrection}
}

func relativePath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}
