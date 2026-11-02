package observationexec

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

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

type controlledCopyReader struct {
	started chan struct{}
	resume  <-chan struct{}
	first   bool
}

func (r *controlledCopyReader) Read(p []byte) (int, error) {
	if r.first {
		return 0, io.EOF
	}
	r.first = true
	close(r.started)
	<-r.resume
	return copy(p, []byte("partial-copy-data")), nil
}

func TestModuleCopyCancellationRemovesPartialFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	destination := filepath.Join(dir, "target.bin")
	resume := make(chan struct{})
	reader := &controlledCopyReader{started: make(chan struct{}), resume: resume}
	done := make(chan error, 1)
	go func() {
		_, err := execfile.WriteStream(dir, ".module-copy-*", contextCopyReader{ctx: ctx, reader: reader}, 0o600)
		done <- err
	}()
	select {
	case <-reader.started:
	case <-time.After(5 * time.Second):
		t.Fatal("copy reader never began I/O")
	}
	cancel()
	close(resume)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("copy error = %v, want real context cancellation", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled atomic copy did not return")
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("cancelled copy published destination: %v", err)
	}
	temps, err := filepath.Glob(filepath.Join(dir, ".module-copy-*"))
	if err != nil || len(temps) != 0 {
		t.Fatalf("cancelled copy left partial temporary files: %v, %v", temps, err)
	}
}

func TestModuleCopyCancelledPublicationDoesNotExposeDestination(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	dir := t.TempDir()
	temp := filepath.Join(dir, ".module-copy-staged")
	destination := filepath.Join(dir, "target.bin")
	if err := os.WriteFile(temp, []byte("prepared"), 0o600); err != nil {
		t.Fatal(err)
	}
	cancel()
	copier := &moduleTreeCopier{ctx: ctx}
	if err := copier.publishCopiedFile(temp, destination); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled publication = %v", err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("cancelled publication exposed destination: %v", err)
	}
}

func TestObservationCancellationWaitErrorsDistinguishReaping(t *testing.T) {
	if err := observationCancellationWaitError(context.Canceled, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("clean cancellation = %v", err)
	}
	if err := observationCancellationWaitError(context.Canceled, &exec.ExitError{}); !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "not confirmed") {
		t.Fatalf("reaped signalled process = %v", err)
	}
	err := observationCancellationWaitError(context.Canceled, context.DeadlineExceeded)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "wait/reap not confirmed") {
		t.Fatalf("bounded wait timeout lost cancellation/reap evidence: %v", err)
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

