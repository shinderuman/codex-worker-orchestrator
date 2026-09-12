package harnesslint

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestForwardOnlyGoViolationsRejectVersionPromotion(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "version promotion",
			source: `package fixture
const currentVersion = 3
type marker struct { Version int }
func decode(value marker) marker {
	if value.Version == 1 {
		value.Version = currentVersion
	}
	return value
}
`,
		},
		{
			name: "schema revision promotion with reversed equality",
			source: `package fixture
const currentSchemaRevision = 4
type record struct { SchemaRevision int }
func decode(value record) record {
	if 2 == value.SchemaRevision {
		value.SchemaRevision = currentSchemaRevision
	}
	return value
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := forwardOnlyFixtureViolations(t, tc.source)
			if len(violations) != 1 || violations[0].Rule != forwardOnlyCompatibilityRule {
				t.Fatalf("violations = %+v, want one %s violation", violations, forwardOnlyCompatibilityRule)
			}
		})
	}
}

func TestForwardOnlyGoViolationsAllowForwardOnlyAndUnrelatedFallbacks(t *testing.T) {
	cases := []struct {
		name   string
		source string
	}{
		{
			name: "strict current version decoder",
			source: `package fixture
import "fmt"
const currentVersion = 3
type marker struct { Version int }
func decode(value marker) error {
	if value.Version != currentVersion {
		return fmt.Errorf("unsupported version")
	}
	return nil
}
`,
		},
		{
			name: "writer-side current version initialization",
			source: `package fixture
const currentVersion = 3
type marker struct { Version int }
func appendRecord(value marker) marker {
	if value.Version == 0 {
		value.Version = currentVersion
	}
	return value
}
`,
		},
		{
			name: "old state reset rather than promotion",
			source: `package fixture
const currentVersion = 3
type marker struct { Version int }
func reset(value marker) marker {
	if value.Version == 1 {
		value.Version = 0
	}
	return value
}
`,
		},
		{
			name: "historical legacy label",
			source: `package fixture
const legacyCoverage = "legacy-evidence:lifecycle"
`,
		},
		{
			name: "algorithmic fallback",
			source: `package fixture
func choose(primary, fallback string) string {
	if primary == "" {
		return fallback
	}
	return primary
}
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if violations := forwardOnlyFixtureViolations(t, tc.source); len(violations) != 0 {
				t.Fatalf("violations = %+v, want none", violations)
			}
		})
	}
}

func forwardOnlyFixtureViolations(t *testing.T, source string) []Violation {
	t.Helper()
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	return forwardOnlyGoViolations(set, file, "fixture.go")
}
