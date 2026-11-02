package parentevidence

import (
	"bytes"
	"go/format"
	"os"
	"strings"
	"testing"
)

func TestDiffGofmtDiagnostic(t *testing.T) {
	for _, name := range []string{"projector.go", "diff_admission_contract_test.go"} {
		original, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		expected, err := format.Source(original)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(original, expected) {
			continue
		}
		before := strings.Split(string(original), "\n")
		after := strings.Split(string(expected), "\n")
		for i := 0; i < len(before) && i < len(after); i++ {
			if before[i] != after[i] {
				t.Errorf("%s:%d before=%q after=%q", name, i+1, before[i], after[i])
			}
		}
	}
}
