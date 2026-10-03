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
	path, locator, err := splitCanonicalTarget(raw)
	if err != nil {
		return Target{}, err
	}
	if start, end, ok := ParseLineRange(locator); ok {
		return Target{Path: path, Locator: locator, Kind: LocatorLineRange, LineStart: start, LineEnd: end}, nil
	}
	if locator == WholeFileDiffLocator {
		return Target{Path: path, Locator: locator, Kind: LocatorWholeDiff}, nil
	}
	return parseGoTarget(raw, path, locator)
}

func splitCanonicalTarget(raw string) (string, string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", "", parseError("shape", raw)
	}
	separator := strings.Index(raw, ":")
	if separator <= 0 || separator == len(raw)-1 {
		return "", "", parseError("shape", raw)
	}
	path := raw[:separator]
	locator := raw[separator+1:]
	if strings.TrimSpace(path) != path || strings.TrimSpace(locator) != locator || !relativePath(path) || strings.ContainsAny(path, " ,()") {
		return "", "", parseError("path", raw)
	}
	return path, locator, nil
}

func parseGoTarget(raw, path, locator string) (Target, error) {
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
		if validGoLocatorIdentifier(parts[0]) {
			return "", parts[0], true
		}
	case 2:
		if validGoLocatorIdentifier(parts[0]) && validGoLocatorIdentifier(parts[1]) {
			return parts[0], parts[1], true
		}
	}
	return "", "", false
}

func validGoLocatorIdentifier(value string) bool {
	return value != "_" && token.IsIdentifier(value)
}

func parseError(code, raw string) error {
	return &ParseError{Code: code, Target: raw, Correction: canonicalCorrection}
}

func relativePath(path string) bool {
	if strings.Contains(path, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	return clean == path && clean != "" && clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") && !strings.HasPrefix(clean, "/")
}
