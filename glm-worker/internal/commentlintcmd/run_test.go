package commentlintcmd

import "testing"

func TestParseArgs(t *testing.T) {
	if fix, ok := parseArgs(nil); !ok || fix {
		t.Fatalf("empty = %v,%v", fix, ok)
	}
	if fix, ok := parseArgs([]string{"--fix"}); !ok || !fix {
		t.Fatalf("fix = %v,%v", fix, ok)
	}
	if _, ok := parseArgs([]string{"x"}); ok {
		t.Fatal("unexpected argument must fail")
	}
}
