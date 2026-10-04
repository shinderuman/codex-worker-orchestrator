//go:build !darwin && !linux

package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSetStdinFileRawUnsupportedPlatformBehavior(t *testing.T) {
	nullDevice, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("character deviceが開けません: %v", err)
	}
	defer func() { _ = nullDevice.Close() }()

	restore, applied, err := setStdinFileRaw(nullDevice)
	if err != nil {
		t.Fatalf("non-terminal character device was rejected: %v", err)
	}
	if applied {
		t.Fatal("non-terminal character deviceでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("non-terminal character deviceの復元がerrorを返しています: %v", err)
	}

	pipeReader, _, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	defer func() { _ = pipeReader.Close() }()
	restore, applied, err = setStdinFileRaw(pipeReader)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("pipe stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("pipe stdinの復元がerrorを返しています: %v", err)
	}

	payloadPath := filepath.Join(t.TempDir(), "payload-source")
	if err := os.WriteFile(payloadPath, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	payloadFile, openErr := os.Open(payloadPath)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer func() { _ = payloadFile.Close() }()
	restore, applied, err = setStdinFileRaw(payloadFile)
	if err != nil {
		t.Fatal(err)
	}
	if applied {
		t.Fatal("regular file stdinでraw適用扱いになっています")
	}
	if err := restore(); err != nil {
		t.Fatalf("regular file stdinの復元がerrorを返しています: %v", err)
	}
}
