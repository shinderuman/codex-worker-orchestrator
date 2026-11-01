//go:build darwin || linux

package observationexec

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestModuleCopyBlockedSourceDoesNotHoldGlobalForkLock(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
	source := filepath.Join(t.TempDir(), "blocked-source")
	destination := filepath.Join(t.TempDir(), "copied")
	if err := syscall.Mkfifo(source, 0o600); err != nil {
		t.Fatal(err)
	}
	writerReady := make(chan error, 1)
	releaseWriter := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWriter) }) }
	defer release()
	go func() {
		writer, err := os.OpenFile(source, os.O_WRONLY, 0)
		if err != nil {
			writerReady <- err
			return
		}
		_, err = writer.Write([]byte("source is still open"))
		writerReady <- err
		<-releaseWriter
		_ = writer.Close()
	}()
	copyDone := make(chan error, 1)
	copier := &moduleTreeCopier{}
	go func() { copyDone <- copier.copyFile(destination, source, 0o600) }()
	select {
	case err := <-writerReady:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO source did not open")
	}
	readyDeadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(destination); err == nil {
			break
		}
		select {
		case err := <-copyDone:
			t.Fatalf("copy returned before source read stalled: %v", err)
		case <-readyDeadline:
			t.Fatal("copy destination was not created")
		case <-time.After(5 * time.Millisecond):
		}
	}
	lockDone := make(chan struct{})
	go func() {
		syscall.ForkLock.Lock()
		syscall.ForkLock.Unlock()
		close(lockDone)
	}()
	commandDone := make(chan error, 1)
	go func() { commandDone <- exec.Command("sh", "-c", "true").Run() }()
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatalf("concurrent subprocess failed during source read: %v", err)
		}
	case <-time.After(5 * time.Second):
		release()
		t.Fatal("source read held the global fork lock and blocked subprocess start")
	}
	select {
	case <-lockDone:
	case <-time.After(5 * time.Second):
		release()
		t.Fatal("source read retained ForkLock after destination creation")
	}
	select {
	case err := <-copyDone:
		t.Fatalf("copy should still wait for source EOF, got %v", err)
	default:
	}
	release()
	select {
	case err := <-copyDone:
		if err != nil {
			t.Fatalf("copy failed after release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copy did not finish after source EOF")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "source is still open" {
		t.Fatalf("copied source mismatch: %q err=%v", data, err)
	}
}
