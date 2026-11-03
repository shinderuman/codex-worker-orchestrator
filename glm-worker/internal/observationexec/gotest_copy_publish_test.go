package observationexec

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestModuleCopyPreservesExistingDestinationAndCleansTemporaryFile(t *testing.T) {
	for _, kind := range []string{"regular", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			destination := filepath.Join(dir, "existing")
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("existing"), 0o640); err != nil {
				t.Fatal(err)
			}
			var err error
			if kind == "symlink" {
				err = os.Symlink("target", destination)
			} else {
				err = os.WriteFile(destination, []byte("existing"), 0o640)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(t.TempDir(), "source")
			if err := os.WriteFile(source, []byte("new content"), 0o700); err != nil {
				t.Fatal(err)
			}
			copier := &moduleTreeCopier{ctx: context.Background()}
			if err := copier.copyFile(destination, source, 0o700); !errors.Is(err, os.ErrExist) {
				t.Fatalf("既存destinationへのcopy: %v", err)
			}
			after, err := os.Lstat(destination)
			if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
				t.Fatalf("既存destination変更: info=%v err=%v", after, err)
			}
			data, err := os.ReadFile(destination)
			if err != nil || string(data) != "existing" {
				t.Fatalf("既存content変更: data=%q err=%v", data, err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 2 || copier.bytes != 0 {
				t.Fatalf("失敗時回収: entries=%d bytes=%d err=%v", len(entries), copier.bytes, err)
			}
		})
	}
}

func TestCopiedExecutablesRunDuringConcurrentCopiesAndSubprocesses(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	content := []byte("#!/bin/sh\nprintf copied\n" + strings.Repeat("# padding\n", 7000))
	if err := os.WriteFile(source, content, 0o700); err != nil {
		t.Fatal(err)
	}
	failures := make(chan error, 8)
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		dir := t.TempDir()
		workers.Add(1)
		go func() {
			defer workers.Done()
			copier := &moduleTreeCopier{ctx: context.Background()}
			for iteration := 0; iteration < 16; iteration++ {
				destination := filepath.Join(dir, fmt.Sprintf("probe-%d", iteration))
				if err := copier.copyFile(destination, source, 0o700); err != nil {
					failures <- err
					return
				}
				if output, err := exec.Command(destination).Output(); err != nil || string(output) != "copied" {
					failures <- fmt.Errorf("copy直後のexec: output=%q err=%w", output, err)
					return
				}
			}
		}()
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}
