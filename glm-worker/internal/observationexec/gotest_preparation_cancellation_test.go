package observationexec

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type cancelAfterReadsContext struct {
	context.Context
	remaining int
}

func TestCancelledObservationDoesNotEnterConfinement(t *testing.T) {
	moduleDir := t.TempDir()
	artifactDir := t.TempDir()
	original := observationConfinementAdmission
	t.Cleanup(func() { observationConfinementAdmission = original })
	called := false
	observationConfinementAdmission = func() error {
		called = true
		return errors.New("must not run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	outcome := RunIsolatedGoTest(ctx, GoTestInput{ModuleDir: moduleDir, ArtifactDir: artifactDir, ExecutionID: "cancel-before-0001", DeadlineMS: 60000})
	if outcome.Status != StatusFail || outcome.ExitSource != exitSourceCancelled || called {
		t.Fatalf("cancelled execution entered confinement: outcome=%+v called=%v", outcome, called)
	}
	if entries, err := os.ReadDir(artifactDir); err != nil || len(entries) != 0 {
		t.Fatalf("cancelled execution created artifacts: entries=%v err=%v", entries, err)
	}
}

func TestObservationPreflightProcessDoesNotLaunchAfterCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell preflight fixture")
	}
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runObservationPreflightProcess(ctx, []string{"/bin/sh", "-c", "printf started > \"$1\"", "sh", marker})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight cancellation error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("cancelled preflight launched subprocess: %v", err)
	}
}

func TestObservationPreflightProcessDeadlineReapsProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell preflight fixture")
	}
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runObservationPreflightProcess(ctx, []string{"/bin/sh", "-c", "printf started > \"$1\"; exec sleep 30", "sh", marker})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("preflight deadline error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 12*time.Second {
		t.Fatalf("preflight timeout did not bound subprocess lifetime: %s", elapsed)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("preflight did not start before deadline: %v", err)
	}
}

func TestCancelledModuleCopyDoesNotCommitInput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := t.TempDir()
	destination := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(filepath.Join(source, "payload.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyModuleTree(ctx, destination, source); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled module copy = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("cancelled module copy created a destination: %v", err)
	}
}

func (c *cancelAfterReadsContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestModuleCopyCancellationRemovesPartialFile(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.bin")
	destination := filepath.Join(t.TempDir(), "target.bin")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(16 << 20); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterReadsContext{Context: context.Background(), remaining: 3}
	copier := &moduleTreeCopier{ctx: ctx}
	err = copier.copyFile(destination, source, 0o600)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("copy error = %v want context cancellation", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial destination was not removed: %v", err)
	}
	if copier.bytes != 0 {
		t.Fatalf("cancelled copy committed %d bytes", copier.bytes)
	}
}

func TestObservationPreflightNormalAndFailedCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell preflight fixture")
	}
	output, err := runObservationPreflightProcess(context.Background(), []string{"/bin/sh", "-c", "printf ready"})
	if err != nil || string(output) != "ready" {
		t.Fatalf("successful preflight output=%q err=%v", output, err)
	}
	output, err = runObservationPreflightProcess(context.Background(), []string{"/bin/sh", "-c", "printf denied >&2; exit 23"})
	if err == nil || !strings.Contains(string(output), "denied") {
		t.Fatalf("failed preflight output=%q err=%v", output, err)
	}
}

func TestObservationPreflightCancellationStopsDescendant(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell process-group fixture")
	}
	dir := t.TempDir()
	childStarted := filepath.Join(dir, "child-started")
	childSurvived := filepath.Join(dir, "child-survived")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		limit := time.After(4 * time.Second)
		for {
			if _, err := os.Stat(childStarted); err == nil {
				cancel()
				return
			}
			select {
			case <-limit:
				cancel()
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}()
	script := "(printf launched > \"$1\"; sleep 2; printf survived > \"$2\") & wait"
	start := time.Now()
	_, err := runObservationPreflightProcess(ctx, []string{"/bin/sh", "-c", script, "sh", childStarted, childSurvived})
	<-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("preflight child cancellation = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 12*time.Second {
		t.Fatalf("preflight descendant cancellation took %s", elapsed)
	}
	if _, err := os.Stat(childStarted); err != nil {
		t.Fatalf("preflight child never started: %v", err)
	}
	time.Sleep(2200 * time.Millisecond)
	if _, err := os.Stat(childSurvived); !os.IsNotExist(err) {
		t.Fatalf("cancelled preflight descendant survived: %v", err)
	}
}
