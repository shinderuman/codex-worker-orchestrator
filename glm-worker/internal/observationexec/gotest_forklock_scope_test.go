//go:build darwin || linux

package observationexec

import (
	"bytes"
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
	content     []byte
	copyDone    <-chan error
	release     func()
}

func newStalledCopyFixture(t *testing.T, boundaryRead bool) stalledCopyFixture {
	t.Helper()
	source := filepath.Join(t.TempDir(), "blocked-source")
	destination := filepath.Join(t.TempDir(), "copied")
	if err := syscall.Mkfifo(source, 0o600); err != nil {
		t.Fatal(err)
	}
	writerReady := make(chan error, 1)
	content := bytes.Repeat([]byte("source is still open\n"), 65536)
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
		_, err = writer.Write(content)
		writerReady <- err
		<-releaseWriter
		_ = writer.Close()
	}()
	copyDone := make(chan error, 1)
	copier := &moduleTreeCopier{}
	if boundaryRead {
		copier.maxBytes = int64(len(content))
	}
	go func() { copyDone <- copier.copyFile(destination, source, 0o600) }()
	select {
	case err := <-writerReady:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FIFO source did not open")
	}
	return stalledCopyFixture{destination: destination, content: content, copyDone: copyDone, release: release}
}

func TestModuleCopyBlockedSourceAllowsConcurrentSubprocess(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is unavailable")
	}
	for _, boundaryRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "source-read", true: "boundary-read"}[boundaryRead], func(t *testing.T) {
			testModuleCopyBlockedSourceAllowsConcurrentSubprocess(t, boundaryRead)
		})
	}
}

func testModuleCopyBlockedSourceAllowsConcurrentSubprocess(t *testing.T, boundaryRead bool) {
	t.Helper()
	fixture := newStalledCopyFixture(t, boundaryRead)
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
	if err != nil || !bytes.Equal(data, fixture.content) {
		t.Fatalf("copied input mismatch: bytes=%d err=%v", len(data), err)
	}
}

func TestModuleCopyBlockedSourceReleasesForkLock(t *testing.T) {
	for _, boundaryRead := range []bool{false, true} {
		t.Run(map[bool]string{false: "source-read", true: "boundary-read"}[boundaryRead], func(t *testing.T) {
			testModuleCopyBlockedSourceReleasesForkLock(t, boundaryRead)
		})
	}
}

func testModuleCopyBlockedSourceReleasesForkLock(t *testing.T, boundaryRead bool) {
	t.Helper()
	fixture := newStalledCopyFixture(t, boundaryRead)
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
