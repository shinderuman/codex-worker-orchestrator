package reviewtarget

import (
	"errors"
	"testing"
)

func TestParseTargetCanonicalLocatorVocabulary(t *testing.T) {
	tests := []struct {
		target string
		kind   LocatorKind
		owner  string
		symbol string
	}{
		{target: "a.go:10", kind: LocatorLineRange},
		{target: "a.go:10-20", kind: LocatorLineRange},
		{target: "a.md:@diff", kind: LocatorWholeDiff},
		{target: "a.go:Run", kind: LocatorGoSymbol, symbol: "Run"},
		{target: "a.go:Runner.Run", kind: LocatorGoSymbol, owner: "Runner", symbol: "Run"},
	}
	for _, tc := range tests {
		parsed, err := ParseTarget(tc.target)
		if err != nil {
			t.Fatalf("ParseTarget(%q): %v", tc.target, err)
		}
		if parsed.Kind != tc.kind || parsed.Owner != tc.owner || parsed.Symbol != tc.symbol {
			t.Fatalf("ParseTarget(%q) = %#v", tc.target, parsed)
		}
	}
}

func TestParseTargetRejectsUnprovableLocatorShapes(t *testing.T) {
	for _, target := range []string{
		"a.go:AppendRecord-acquireRegistryLock",
		"a.go:10(exact target)",
		"a.go:10-20(notes)",
		"a.go:First,Second",
		"a.go:Runner.Run.More",
		"a.md:Heading",
	} {
		_, err := ParseTarget(target)
		if err == nil {
			t.Fatalf("ParseTarget(%q) unexpectedly succeeded", target)
		}
		var parseErr *ParseError
		if !errors.As(err, &parseErr) || parseErr.Code == "" || parseErr.Correction == "" {
			t.Fatalf("ParseTarget(%q) error is not machine-readable: %v", target, err)
		}
	}
}

func TestFindGoDeclarationUsesPreciseTopLevelAndMemberIdentity(t *testing.T) {
	content := []byte(`package review
var Target = 1
func Caller() {
	Target := 2
	_ = Target
}
type First struct { Shared int }
type Second struct { Shared int }
func (First) Run() {}
func (Second) Run() {}
`)
	if declaration, err := FindGoDeclaration(content, "Target"); err != nil || declaration.Kind != "var" {
		t.Fatalf("top-level Target = %#v err=%v", declaration, err)
	}
	if _, err := FindGoDeclaration(content, "Shared"); err == nil {
		t.Fatal("bare symbol matched an unrelated struct field")
	}
	if _, err := FindGoDeclaration(content, "Run"); err == nil {
		t.Fatal("bare symbol matched an unrelated method")
	}
	if declaration, err := FindGoDeclaration(content, "First.Shared"); err != nil || declaration.Kind != "field" {
		t.Fatalf("First.Shared = %#v err=%v", declaration, err)
	}
	if declaration, err := FindGoDeclaration(content, "Second.Run"); err != nil || declaration.Kind != "method" {
		t.Fatalf("Second.Run = %#v err=%v", declaration, err)
	}
}
