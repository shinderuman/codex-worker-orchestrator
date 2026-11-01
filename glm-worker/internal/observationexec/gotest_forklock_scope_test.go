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

type stalledCopyFixture struct {
	destination string
	copyDone    <-chan error
	release     func()
}

func newStalledCopyFixture(t *testing.T) stalledCopyFixture {
	t.Helper()
	source := filepath.Join(t.TempDir(), "blocked-source")
	destination := filepath.Join(t.TempDir(), "copied")
	if err := syscall.Mkfifo(source, 0o600); err != nil {
		t.Fatal(err)
	}
	writerReady := make(chan error, 1)
	releaseWriter := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseWriter) }) }
	t.Cleanup(release)
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
	deadline := time.After(5 * time.Second)
	for {
		if _, err := os.Stat(destination); err == nil {
			break
		}
		select {
		case err := <-copyDone:
			t.Fatalf("copy ended before destination creation: %v", err)
		case <-deadline:
			t.Fatal("copy destination was not created")
		case <-time.After(5 * time.Millisecond):
		}
	}
	return stalledCopyFixture{destination: destination, copyDone: copyDone, release: release}
}

func TestModuleCopyBlockedSourceAllowsConcurrentSubprocess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
	fixture := newStalledCopyFixture(t)
	commandDone := make(chan error, 1)
	go func() { commandDone <- exec.Command("sh", "-c", "true").Run() }()
	select {
	case err := <-commandDone:
		if err != nil {
			t.Fatalf("concurrent subprocess failed during source read: %v", err)
		}
	case <-time.After(5 * time.Second):
		fixture.release()
		t.Fatal("source read blocked subprocess start")
	}
	select {
	case err := <-fixture.copyDone:
		t.Fatalf("copy should still wait for EOF: %v", err)
	default:
	}
	fixture.release()
	select {
	case err := <-fixture.copyDone:
		if err != nil {
			t.Fatalf("copy failed after source EOF: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copy did not finish after source EOF")
	}
	data, err := os.ReadFile(fixture.destination)
	if err != nil || string(data) != "source is still open" {
		t.Fatalf("copied input mismatch: %q err=%v", data, err)
	}
}

func TestModuleCopyBlockedSourceReleasesForkLock(t *testing.T) {
	fixture := newStalledCopyFixture(t)
	lockAcquired := make(chan struct{}, 1)
	go func() {
		syscall.ForkLock.Lock()
		lockAcquired <- struct{}{}
		syscall.ForkLock.Unlock()
	}()
	select {
	case <-lockAcquired:
	case <-time.After(5 * time.Second):
		fixture.release()
		t.Fatal("blocked source retained the global ForkLock")
	}
	fixture.release()
	select {
	case err := <-fixture.copyDone:
		if err != nil {
			t.Fatalf("copy failed after source EOF: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("copy did not finish")
	}
}
