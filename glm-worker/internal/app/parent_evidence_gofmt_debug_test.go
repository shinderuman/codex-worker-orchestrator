package app

import (
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParentEvidenceProjectorGofmtDebug(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	path := filepath.Join(filepath.Dir(currentFile), "..", "parentevidence", "projector.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(source)
	if err != nil {
		t.Fatal(err)
	}
	if string(source) == string(formatted) {
		return
	}
	originalLines := strings.Split(string(source), "\n")
	formattedLines := strings.Split(string(formatted), "\n")
	var delta strings.Builder
	limit := len(originalLines)
	if len(formattedLines) > limit {
		limit = len(formattedLines)
	}
	for i := 0; i < limit; i++ {
		var original, canonical string
		if i < len(originalLines) {
			original = originalLines[i]
		}
		if i < len(formattedLines) {
			canonical = formattedLines[i]
		}
		if original != canonical {
			fmt.Fprintf(&delta, "line %d\n- %q\n+ %q\n", i+1, original, canonical)
		}
	}
	t.Fatalf("projector.go gofmt delta:\n%s", delta.String())
}
