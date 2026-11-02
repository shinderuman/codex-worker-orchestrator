package failurepathadvisory

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentEvidenceGofmtDiagnostic(t *testing.T) {
	for _, name := range []string{"projector.go", "diff_admission_contract_test.go"} {
		path := filepath.Join("..", "parentevidence", name)
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		formatted, err := format.Source(original)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(original, formatted) {
			continue
		}
		before := strings.Split(string(original), "\n")
		after := strings.Split(string(formatted), "\n")
		for i := 0; i < len(before) && i < len(after); i++ {
			if before[i] != after[i] {
				t.Errorf("%s:%d before=%q after=%q", name, i+1, before[i], after[i])
			}
		}
	}
}
