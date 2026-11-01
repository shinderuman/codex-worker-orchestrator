package harnesslint

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParenthesizedLiteralOnlyTestEntrypoint(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		code string
		want bool
	}{
		{
			name: "parenthesized static log",
			code: "package fixture\nimport \"testing\"\nfunc TestInert(t *testing.T) { t.Log((\"static\")) }\n",
			want: true,
		},
		{
			name: "parenthesized dynamic observation",
			code: "package fixture\nimport \"testing\"\nfunc observed() string { return \"value\" }\nfunc TestObserved(t *testing.T) { t.Log((observed())) }\n",
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := "fixture_test.go"
			if err := os.WriteFile(filepath.Join(root, path), []byte(tc.code), 0o600); err != nil {
				t.Fatal(err)
			}
			violations, err := scanGoRules(root, []string{path})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, violation := range violations {
				if violation.Rule == "empty-test-entrypoint" {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("inert entrypoint violation = %v, want %v; violations: %#v", found, tc.want, violations)
			}
		})
	}
}
