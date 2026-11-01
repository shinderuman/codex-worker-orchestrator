//go:build dockerintegration

package runner

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDockerTestOwnerStagingRunsRealDockerDiagnostic(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("Docker test backend契約はDarwin ARM64 host向け")
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	guard, err := prepareGitAuthorityGuard(root)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.cleanup()
	if !guard.dockerPolicy {
		t.Fatal("Docker backend契約が有効ではありません")
	}
	executor, executorErr := guard.startDockerTestExecutor()
	if executorErr != nil {
		t.Fatal(executorErr)
	}
	defer executor.stop()

	wantArgv := []string{"-count=1", "-run", "^TestBackendSelectionKeepsNativeForAbsentContractEverywhere$", "./internal/goqualitycmd"}
	var output bytes.Buffer
	script := exec.Command(filepath.Join(root, "goquality"), append([]string{"test"}, wantArgv...)...)
	script.Dir = root
	script.Stdout = &output
	script.Stderr = &output
	script.Env = append(os.Environ(),
		"PATH="+guard.tempDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GLM_WORKER_DOCKER_TEST_STAGING="+guard.dockerTestStagingDir,
		"GLM_WORKER_DOCKER_TEST_RESPONSES="+guard.dockerTestResponseDir,
	)
	runErr := script.Run()
	text := output.String()
	if runErr != nil {
		t.Fatalf("owner staging経由のgoquality testが失敗しました: %v\n%s", runErr, text)
	}
	diagnostic := stagingOwnerDiagnosticLine(t, text)
	if diagnostic.Form != "diagnostic-go-test-linux" || diagnostic.Status != "pass" || diagnostic.Exit != 0 || diagnostic.Platform != "linux/arm64" {
		t.Fatalf("owner実行の機械証跡 = %#v", diagnostic)
	}
	if diagnostic.Head == "" || diagnostic.ImageID == "" || diagnostic.ArchiveSHA256 == "" {
		t.Fatalf("source binding証拠が欠けています: %#v", diagnostic)
	}
	if strings.Join(diagnostic.Argv, "\x00") != strings.Join(wantArgv, "\x00") {
		t.Fatalf("owner実行のargv = %#v want %#v", diagnostic.Argv, wantArgv)
	}
	if !strings.Contains(text, "goquality-diagnostic ") {
		t.Fatalf("container内証跡行の再現がありません: %s", text)
	}
	assertStagingOwnerBoundaries(t, guard)
	t.Logf("owner staging diagnostic: head=%s total_ms=%d test_ms=%d image=%s", diagnostic.Head, diagnostic.TotalMS, diagnostic.TestMS, diagnostic.ImageID)
}

type stagingOwnerDiagnostic struct {
	Form          string   `json:"form"`
	Platform      string   `json:"platform"`
	Status        string   `json:"status"`
	Head          string   `json:"head"`
	ImageID       string   `json:"image_id"`
	ArchiveSHA256 string   `json:"archive_sha256"`
	Argv          []string `json:"argv"`
	TotalMS       int64    `json:"total_ms"`
	TestMS        int64    `json:"test_ms"`
	Exit          int      `json:"exit"`
}

func stagingOwnerDiagnosticLine(t *testing.T, text string) stagingOwnerDiagnostic {
	t.Helper()
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "goquality-linux-diagnostic ") {
			continue
		}
		var diagnostic stagingOwnerDiagnostic
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "goquality-linux-diagnostic ")), &diagnostic); err != nil {
			t.Fatalf("goquality-linux-diagnostic行のparseに失敗しました: %v: %s", err, line)
		}
		return diagnostic
	}
	t.Fatalf("goquality-linux-diagnostic行が再現されていません: %s", text)
	return stagingOwnerDiagnostic{}
}

func assertStagingOwnerBoundaries(t *testing.T, guard *gitAuthorityGuard) {
	t.Helper()
	attempts, err := os.ReadFile(guard.attemptLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(attempts), "\n") {
		if strings.HasPrefix(line, "docker-test:") {
			t.Fatalf("staging経由でprotocol違反が記録されました: %s", line)
		}
	}
	responses, err := os.ReadDir(guard.dockerTestResponseDir)
	if err != nil || len(responses) == 0 {
		t.Fatalf("owner responseがありません: %v", err)
	}
	entries, err := os.ReadDir(guard.dockerTestStagingDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "response-") {
			t.Fatalf("worker書込可能領域へresponseが書かれました: %s", entry.Name())
		}
	}
}
