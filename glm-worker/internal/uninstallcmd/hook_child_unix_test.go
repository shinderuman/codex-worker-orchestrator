//go:build unix

package uninstallcmd

import (
	"bufio"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/installlock"
)

func TestUninstallKeepsLockWhileHookChildSurvivesParent(t *testing.T) {
	repo := createConcurrentRetireFixture(t)
	bin := filepath.Join(repo, "bin")
	guard := filepath.Join(bin, "glm-publication-guard")
	writeConcurrentRetireFile(t, guard, []byte("guard fixture"), 0o755)
	hookScript := filepath.Join(repo, "scripts", "manage-pull-hook.sh")
	runConcurrentRetireCommand(t, "sh", hookScript, "install", repo, guard)
	hook := filepath.Join(repo, ".git", "codex-worker-orchestrator", "hooks", "pre-push")
	assertConcurrentRetirePresent(t, hook)

	barrier := gateHookRetirement(t, hookScript)
	childOutput, parent := startUninstallHookParent(t, repo, bin, barrier.Name())
	awaitHookOutput(t, func() error {
		line, err := childOutput.ReadString('\n')
		if err == nil && line != "hook-ready\n" {
			return errors.New(line)
		}
		return err
	})
	if err := parent.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := parent.Wait(); err == nil {
		t.Fatal("強制終了した親uninstallが成功しました")
	}

	if err := installlock.With(func(*os.File) error { return nil }); !errors.Is(err, installlock.ErrBusy) {
		t.Errorf("hook子プロセスの動作中に別のinstallを拒否しませんでした: %v", err)
	}
	assertConcurrentRetirePresent(t, hook)
	if _, err := barrier.WriteString("retire\n"); err != nil {
		t.Fatal(err)
	}
	awaitHookOutput(t, func() error {
		_, err := io.Copy(io.Discard, childOutput)
		return err
	})
	assertConcurrentRetireAbsent(t, hook)
	if err := installlock.With(func(*os.File) error { return nil }); err != nil {
		t.Fatalf("hook子プロセス終了後にinstallを再開できません: %v", err)
	}
	runConcurrentRetireCommand(t, "sh", hookScript+".original", "install", repo, guard)
	assertConcurrentRetirePresent(t, hook)
}

func gateHookRetirement(t *testing.T, script string) *os.File {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hook-release")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = file.WriteString("retire\n")
		_ = file.Close()
	})
	if err := os.Rename(script, script+".original"); err != nil {
		t.Fatal(err)
	}
	writeConcurrentRetireFile(t, script, []byte(`#!/bin/sh
set -eu
printf 'hook-ready\n'
IFS= read -r answer < "$TEST_HOOK_RETIRE_BARRIER"
test "$answer" = retire
exec sh "$0.original" "$@"
`), 0o755)
	return file
}

func startUninstallHookParent(t *testing.T, repo, bin, barrier string) (*bufio.Reader, *exec.Cmd) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	cmd := exec.Command(os.Args[0], "-test.run=^TestUninstallHookParentProcess$")
	cmd.Env = append(os.Environ(),
		"TEST_UNINSTALL_HOOK_REPO="+repo, "TEST_UNINSTALL_HOOK_BIN="+bin,
		"TEST_HOOK_RETIRE_BARRIER="+barrier, "HOME="+t.TempDir(),
		"CLAUDE_CONFIG_DIR=", "CLAUDE_SETTINGS_FILE=",
	)
	cmd.Stdout = writer
	cmd.Stderr = writer
	err = cmd.Start()
	_ = writer.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	return bufio.NewReader(reader), cmd
}

func awaitHookOutput(t *testing.T, read func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- read() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("hook子プロセスの出力を待機中にタイムアウト")
	}
}

func TestUninstallHookParentProcess(t *testing.T) {
	repo := os.Getenv("TEST_UNINSTALL_HOOK_REPO")
	if repo == "" {
		return
	}
	bin := os.Getenv("TEST_UNINSTALL_HOOK_BIN")
	if err := run(repo, bin, filepath.Join(repo, "codex-config"), os.Stdout); err != nil {
		t.Fatal(err)
	}
}
