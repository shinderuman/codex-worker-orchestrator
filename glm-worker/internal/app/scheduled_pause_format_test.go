package app

import (
	"bytes"
	"fmt"
	"go/format"
	"os"
	"strings"
	"testing"
)

func TestScheduledPauseFormattingDiagnostic(t *testing.T) {
	raw, err := os.ReadFile("scheduled_pause_test.go")
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(raw)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(raw, formatted) {
		return
	}
	original := strings.Split(string(raw), "\n")
	expected := strings.Split(string(formatted), "\n")
	max := len(original)
	if len(expected) > max {
		max = len(expected)
	}
	var differences strings.Builder
	for i := 0; i < max; i++ {
		var old, newer string
		if i < len(original) {
			old = original[i]
		}
		if i < len(expected) {
			newer = expected[i]
		}
		if old != newer {
			fmt.Fprintf(&differences, "LINE %d: original %q expected %q\n", i+1, old, newer)
		}
	}
	t.Fatal(differences.String())
}
