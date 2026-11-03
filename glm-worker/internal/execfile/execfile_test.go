package execfile

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"
)

func TestWriteProducesExecutableReplacement(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "probe")
	if err := Write(path, []byte("#!/bin/sh\nprintf first\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := exec.Command(path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(first)) != "first" {
		t.Fatalf("first output = %q", first)
	}
	if err := Write(path, []byte("#!/bin/sh\nprintf second\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := exec.Command(path).Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(second)) != "second" {
		t.Fatalf("second output = %q", second)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("temp files remain: %v", entries)
	}
}

func TestWriteStreamProducesExecutable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first, err := WriteStream(dir, ".probe-*", strings.NewReader("#!/bin/sh\nexit 3\n"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	second, err := WriteStream(dir, ".probe-*", strings.NewReader("#!/bin/sh\nexit 3\n"), 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("temp names collide: %s", first)
	}
	for _, path := range []string{first, second} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("mode = %o", info.Mode().Perm())
		}
		runErr := exec.Command(path).Run()
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			t.Fatalf("probe did not execute: %v", runErr)
		}
	}
}

func TestWriteSupportsMaximumLengthFilename(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), strings.Repeat("x", 255))
	for _, value := range []string{"first", "second"} {
		if err := Write(path, []byte("#!/bin/sh\nprintf "+value+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command(path).Output(); err != nil || string(output) != value {
			t.Fatalf("executable = %q, %v", output, err)
		}
	}
}

func TestConcurrentWriteAndExecuteNeverReportsETXTBSY(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh is not available")
	}
	failures := make(chan error, 8)
	var executions atomic.Int64
	deadline := time.Now().Add(1200 * time.Millisecond)
	var workers sync.WaitGroup
	for i := 0; i < 4; i++ {
		path := filepath.Join(t.TempDir(), "probe")
		workers.Add(1)
		go func() {
			defer workers.Done()
			for time.Now().Before(deadline) {
				if err := Write(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
					failures <- err
					return
				}
				if err := exec.Command(path).Run(); err != nil {
					failures <- err
					return
				}
				executions.Add(1)
			}
		}()
	}
	for i := 0; i < 4; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for time.Now().Before(deadline) {
				if err := exec.Command("sh", "-c", "true").Run(); err != nil {
					failures <- err
					return
				}
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Fatalf("guarded executable write still hit the fork/exec race: %v", err)
	}
	if executions.Load() == 0 {
		t.Fatal("generated executable was never executed")
	}
}

func TestFailedWritesCleanTemporaryFilesAndReleaseForkLock(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := errors.New("reader failed")
	if _, err := WriteStream(dir, ".failed-*", iotest.ErrReader(want), 0o700); !errors.Is(err, want) {
		t.Fatalf("copy error = %v", err)
	}
	if _, err := WriteStream(filepath.Join(dir, "missing"), ".failed-*", strings.NewReader("data"), 0o700); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("create error = %v", err)
	}
	if err := Write(dir, []byte("replacement"), 0o700); err == nil {
		t.Fatal("replaced a directory with a file")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed writes left files: %v, %v", entries, err)
	}
	path := filepath.Join(dir, "probe")
	if err := Write(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(path).Run(); err != nil {
		t.Fatal(err)
	}
}
