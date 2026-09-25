package reviewtarget

import "testing"

func TestParseReviewTarget(t *testing.T) {
	cases := []struct {
		name        string
		target      string
		wantPath    string
		wantLocator string
		wantErr     bool
	}{
		{name: "symbol", target: "glm-worker/internal/packet/validate.go:validateTargets", wantPath: "glm-worker/internal/packet/validate.go", wantLocator: "validateTargets"},
		{name: "line", target: "a.go:10", wantPath: "a.go", wantLocator: "10"},
		{name: "range", target: "a.go:10-20", wantPath: "a.go", wantLocator: "10-20"},
		{name: "padded", target: " a.go:10 ", wantPath: "a.go", wantLocator: "10"},
		{name: "live malformed description", target: "IMPLEMENTATION_TASKS/system-one-dogfood-evidence-shadow-eval.md (Contract追記・終了時Go/No-Go要件)", wantErr: true},
		{name: "missing locator", target: "a.go:", wantErr: true},
		{name: "missing path", target: ":symbol", wantErr: true},
		{name: "absolute path", target: "/tmp/a.go:10", wantErr: true},
		{name: "parent path", target: "../a.go:10", wantErr: true},
		{name: "path spaces", target: "dir with space/a.go:10", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, locator, err := Parse(tc.target)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) unexpectedly succeeded: path=%q locator=%q", tc.target, path, locator)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.target, err)
			}
			if path != tc.wantPath || locator != tc.wantLocator {
				t.Fatalf("Parse(%q) = (%q, %q), want (%q, %q)", tc.target, path, locator, tc.wantPath, tc.wantLocator)
			}
		})
	}
}
