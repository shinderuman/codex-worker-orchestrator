package workflow

import "testing"

func TestRepositoryInstructionSurfaceIsCritical(t *testing.T) {
	tests := []struct {
		path     string
		wantHigh bool
	}{
		{path: "AGENTS.md", wantHigh: true},
		{path: "AGENTS.local.md", wantHigh: true},
		{path: "nested/AGENTS.md", wantHigh: true},
		{path: "nested/deeper/AGENTS.local.md", wantHigh: true},
		{path: "nested/AGENTS.txt", wantHigh: false},
		{path: "nested/my-AGENTS.md", wantHigh: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			high, _ := IsCriticalPath(test.path)
			if high != test.wantHigh {
				t.Fatalf("IsCriticalPath(%q) = %v, want %v", test.path, high, test.wantHigh)
			}
		})
	}
}
