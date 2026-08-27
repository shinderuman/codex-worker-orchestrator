package mergejsoncmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	fragment := filepath.Join(dir, "managed.json")
	if err := os.WriteFile(fragment, []byte(`{"env":{"A":"1"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := Run([]string{"-target", target, "-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "updated\n" {
		t.Fatalf("output=%q", output.String())
	}
	output.Reset()
	if err := Run([]string{"-target", target, "-fragment", fragment}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "unchanged\n" {
		t.Fatalf("output=%q", output.String())
	}
}

func TestRunRejectsMissingPaths(t *testing.T) {
	if err := Run(nil, &bytes.Buffer{}); err == nil {
		t.Fatal("expected usage error")
	}
}
