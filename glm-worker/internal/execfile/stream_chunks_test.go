package execfile

import (
	"bytes"
	"os"
	"testing"
)

func TestWriteStreamPreservesChunkSequence(t *testing.T) {
	want := bytes.Repeat([]byte("複数chunkの順序\n"), 16000)
	path, err := WriteStream(t.TempDir(), ".chunks-*", bytes.NewReader(want), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("stream内容: got=%d want=%d err=%v", len(got), len(want), err)
	}
}
