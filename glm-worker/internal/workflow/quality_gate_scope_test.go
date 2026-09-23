package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRepositoryQualityGateSkipsInactiveHarness(t *testing.T) {
	cases := []struct {
		name   string
		marker string
	}{
		{name: "absent marker"},
		{name: "foreign marker", marker: "foreign-repository-harness\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "IMPLEMENTATION_PLAN.local.md"), []byte("malformed plan\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.marker != "" {
				if err := os.WriteFile(filepath.Join(root, ".glm-worker-repository-harness"), []byte(tc.marker), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			report, err := runRepositoryQualityGate(root)
			if err != nil {
				t.Fatal(err)
			}
			if report.Status != "pass" || len(report.Violations) != 0 {
				t.Fatalf("report = %+v", report)
			}
		})
	}
}
