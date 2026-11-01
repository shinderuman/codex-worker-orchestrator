//go:build unix

package runner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
)

func TestDockerTestExecutorRejectsSymlinkedAndSpecialRequestFiles(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)

	symlinkToken := "7000000000000000000"
	payload := filepath.Join(t.TempDir(), "payload.json")
	if err := os.WriteFile(payload, []byte(`{"schema_version":1,"token":"7000000000000000000","args":["./..."]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(payload, filepath.Join(guard.dockerTestStagingDir, dockertest.StagingRequestFileName(symlinkToken))); err != nil {
		t.Fatal(err)
	}
	response := waitStagingResponse(t, guard.dockerTestResponseDir, symlinkToken)
	if response.ExitCode != 1 || response.Failure == "" {
		t.Fatalf("symlink requestの拒否 = code %d failure %q", response.ExitCode, response.Failure)
	}

	fifoToken := "7000000000000000001"
	if err := syscall.Mkfifo(filepath.Join(guard.dockerTestStagingDir, dockertest.StagingRequestFileName(fifoToken)), 0o600); err != nil {
		t.Fatal(err)
	}
	response = waitStagingResponse(t, guard.dockerTestResponseDir, fifoToken)
	if response.ExitCode != 1 || response.Failure == "" {
		t.Fatalf("特殊file requestの拒否 = code %d failure %q", response.ExitCode, response.Failure)
	}

	if records := recorder.snapshot(); len(records) != 0 {
		t.Fatalf("symlink・特殊fileがowner実行へ到達しました: %#v", records)
	}
	log := dockerTestAttemptLogText(t, guard)
	if !strings.Contains(log, "docker-test:special-file:") {
		t.Fatalf("特殊file拒否が機械記録に残っていません: %q", log)
	}
}

func TestDockerTestExecutorRejectsStagingDirectoryReplacement(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)

	renamed := filepath.Join(filepath.Dir(guard.dockerTestStagingDir), "docker-test-moved")
	if err := os.Rename(guard.dockerTestStagingDir, renamed); err != nil {
		t.Fatal(err)
	}
	decoy := t.TempDir()
	token := "8000000000000000000"
	if err := os.WriteFile(filepath.Join(decoy, dockertest.StagingRequestFileName(token)), []byte(`{"schema_version":1,"token":"8000000000000000000","args":["./..."]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(decoy, guard.dockerTestStagingDir); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)

	if records := recorder.snapshot(); len(records) != 0 {
		t.Fatalf("置換staging経由のrequestがowner実行へ到達しました: %#v", records)
	}
	if _, err := os.Stat(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName(token))); err == nil {
		t.Fatal("置換stagingのrequestへresponseが書かれました")
	}
	log := dockerTestAttemptLogText(t, guard)
	if !strings.Contains(log, "docker-test:staging-replaced") {
		t.Fatalf("staging置換が機械記録に残っていません: %q", log)
	}
}

func TestDockerTestExecutorRejectsCrossDeviceInodeCollision(t *testing.T) {
	guard := newDockerTestGuard(t)
	pins, err := guard.pinDockerTestBoundary()
	if err != nil {
		t.Fatal(err)
	}
	executor := &dockerTestExecutor{
		guard:          guard,
		maxRequestSize: dockerTestMaxRequestBytes,
		pins:           pins,
	}
	token := "9500000000000000000"
	name := dockertest.StagingRequestFileName(token)
	writeStagingRequestFile(t, guard.dockerTestStagingDir, name, stagingRequestBody(t, token, "./..."))
	info, statErr := os.Lstat(filepath.Join(guard.dockerTestStagingDir, name))
	if statErr != nil {
		t.Fatal(statErr)
	}
	system, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatal("request fileのidentityを取得できません")
	}
	candidate := stagingRequestCandidate{name: name, token: token, kind: stagingCandidateRun, inode: system.Ino}

	crossDevice := pins
	crossDevice.stagingDir.device = pins.stagingDir.device + 1
	executor.pins = crossDevice
	_, readErr := executor.readRequest(candidate)
	if readErr == nil || !strings.Contains(readErr.Error(), "一致しません") {
		t.Fatalf("別filesystemの同一inodeが読込前に拒否されませんでした: %v", readErr)
	}

	executor.pins = pins
	if _, err := executor.readRequest(candidate); err != nil {
		t.Fatalf("同一filesystemの正当なrequestが拒否されました: %v", err)
	}
}

func TestDockerTestExecutorRejectsStagingReplacementByRegularDirectory(t *testing.T) {
	guard := newDockerTestGuard(t)
	recorder := &stagingRunRecorder{}
	run := func(_ context.Context, root string, args []string, _ io.Writer, _ io.Writer) (int, error) {
		recorder.capture(root, args)
		return 0, nil
	}
	startTestDockerTestExecutor(t, guard, run)

	renamed := filepath.Join(filepath.Dir(guard.dockerTestStagingDir), "docker-test-moved-again")
	if err := os.Rename(guard.dockerTestStagingDir, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(guard.dockerTestStagingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	token := "8100000000000000000"
	writeStagingRequestFile(t, guard.dockerTestStagingDir, dockertest.StagingRequestFileName(token),
		`{"schema_version":1,"token":"8100000000000000000","args":["./..."]}`)
	time.Sleep(50 * time.Millisecond)

	if records := recorder.snapshot(); len(records) != 0 {
		t.Fatalf("置換後の通常directory内requestがowner実行へ到達しました: %#v", records)
	}
	if _, err := os.Stat(filepath.Join(guard.dockerTestResponseDir, dockertest.StagingResponseFileName(token))); err == nil {
		t.Fatal("置換後のrequestへresponseが書かれました")
	}
	log := dockerTestAttemptLogText(t, guard)
	if !strings.Contains(log, "docker-test:staging-replaced") {
		t.Fatalf("通常directory置換が機械記録に残っていません: %q", log)
	}
}
