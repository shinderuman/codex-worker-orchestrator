//go:build unix

package execfile

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type streamReaderFunc func([]byte) (int, error)

func (f streamReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestWriteStreamReopensPrivateTemporaryFileWithRestrictiveUmask(t *testing.T) {
	dir := t.TempDir()
	previous := syscall.Umask(0o777)
	defer syscall.Umask(previous)
	path, err := WriteStream(dir, ".probe-*", strings.NewReader("#!/bin/sh\nprintf copied\n"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(path).Output(); err != nil || string(output) != "copied" {
		t.Fatalf("restrictive umaskでのexec: output=%q err=%v", output, err)
	}
}

func TestWriteStreamBlockedSourceReleasesForkLockAndAllowsExec(t *testing.T) {
	for _, prefix := range []int{0, 3 * 32 * 1024} {
		t.Run(map[bool]string{true: "first-read", false: "after-chunks"}[prefix == 0], func(t *testing.T) {
			testWriteStreamBlockedSource(t, bytes.Repeat([]byte("x"), prefix))
		})
	}
}

func testWriteStreamBlockedSource(t *testing.T, prefix []byte) {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	var path string
	var writeErr error
	dir := t.TempDir()
	reader := io.MultiReader(bytes.NewReader(prefix), streamReaderFunc(func([]byte) (int, error) {
		close(entered)
		<-release
		return 0, io.EOF
	}))
	go func() {
		path, writeErr = WriteStream(dir, ".probe-*", reader, 0o600)
		close(finished)
	}()
	t.Cleanup(func() {
		unblock()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("source EOF後もWriteStreamが終了しません")
		}
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("source readへ到達しません")
	}
	acquired := make(chan struct{})
	go func() {
		syscall.ForkLock.Lock()
		close(acquired)
		syscall.ForkLock.Unlock()
	}()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("source停止中にForkLockを取得できません")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "/bin/sh", "-c", "true").Run(); err != nil {
		t.Fatalf("source停止中のsubprocess起動: %v", err)
	}
	unblock()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("source EOF後に終了しません")
	}
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, prefix) {
		t.Fatalf("stream内容: bytes=%d err=%v", len(data), err)
	}
}

func TestWriteStreamRejectsReplacedFileBeforeWriteOrChmod(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		for _, phase := range []string{"write", "chmod"} {
			t.Run(kind+"-"+phase, func(t *testing.T) { testWriteStreamReplacedFile(t, kind, phase) })
		}
	}
}

func TestWriteStreamRejectsFIFOReplacementWithoutBlockingForkLock(t *testing.T) {
	dir := t.TempDir()
	ready := make(chan string, 1)
	reader := streamReaderFunc(func(p []byte) (int, error) {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Errorf("temporary file: entries=%d err=%v", len(entries), err)
			return 0, io.ErrUnexpectedEOF
		}
		path := filepath.Join(dir, entries[0].Name())
		if err := os.Rename(path, filepath.Join(dir, "original")); err != nil {
			return 0, err
		}
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			return 0, err
		}
		ready <- path
		return copy(p, "unexpected write"), io.EOF
	})
	done := make(chan error, 1)
	go func() {
		_, err := WriteStream(dir, ".probe-*", reader, 0o700)
		done <- err
	}()
	var fifo string
	select {
	case fifo = <-ready:
	case <-time.After(5 * time.Second):
		select {
		case err := <-done:
			t.Fatalf("FIFO置換前に終了: %v", err)
		default:
			t.Fatal("FIFO置換へ到達しません")
		}
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO置換を受理しました")
		}
	case <-time.After(5 * time.Second):
		peer, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			defer func() {
				if closeErr := peer.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("FIFO回収後も終了しません")
		}
		t.Fatal("FIFO置換でForkLockを保持したまま停止しました")
	}
	info, err := os.Lstat(fifo)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("未知FIFOを削除・変更: info=%v err=%v", info, err)
	}
}

func testWriteStreamReplacedFile(t *testing.T, kind, phase string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "foreign")
	want := []byte("foreign content")
	if err := os.WriteFile(target, want, 0o640); err != nil {
		t.Fatal(err)
	}
	var replaced string
	reads := 0
	reader := streamReaderFunc(func(p []byte) (int, error) {
		reads++
		if phase == "chmod" && reads == 1 {
			return copy(p, "initial chunk"), nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatalf("temporary file: entries=%d err=%v", len(entries), err)
		}
		replaced = filepath.Join(dir, entries[0].Name())
		if err := os.Rename(replaced, filepath.Join(dir, "original")); err != nil {
			t.Fatal(err)
		}
		if kind == "symlink" {
			err = os.Symlink(target, replaced)
		} else {
			err = os.WriteFile(replaced, want, 0o640)
		}
		if err != nil {
			t.Fatal(err)
		}
		if phase == "chmod" {
			return 0, io.EOF
		}
		return copy(p, "unexpected write"), io.EOF
	})
	if path, err := WriteStream(dir, ".probe-*", reader, 0o700); err == nil || path != "" {
		t.Fatalf("置換されたfileを受理: path=%q err=%v", path, err)
	}
	for _, path := range []string{target, replaced} {
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("foreign file変更: path=%s bytes=%d err=%v", path, len(data), err)
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("foreign mode変更: path=%s info=%v err=%v", path, info, err)
		}
	}
}
