package goqualitycmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/dockertest"
)

func writeBackendContract(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "quality-tools.yml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

func TestBackendSelectionResolvesThroughSharedBackendAuthority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body string
	}{
		{name: "no-space-value", body: "go: 1.25.4\ngo-test-macos-backend:docker-linux-arm64\n"},
		{name: "spaced-value", body: "go: 1.25.4\ngo-test-macos-backend: docker-linux-arm64\n"},
		{name: "padded-value", body: "go: 1.25.4\ngo-test-macos-backend:   docker-linux-arm64  \n"},
	}
	onTargetHost := runtime.GOOS == "darwin" && runtime.GOARCH == "arm64"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			code := run([]string{"--backend", writeBackendContract(t, tc.body)}, &stdout, &stderr)
			want := "native\n"
			if onTargetHost {
				want = "docker-linux-arm64\n"
			}
			if code != 0 || stdout.String() != want {
				t.Fatalf("backend = code %d out %q err %q want %q", code, stdout.String(), stderr.String(), want)
			}
		})
	}
}

func TestBackendSelectionKeepsNativeForAbsentContractEverywhere(t *testing.T) {
	t.Parallel()
	var stdout, stderr strings.Builder
	code := run([]string{"--backend", writeBackendContract(t, "namespace: probe\n")}, &stdout, &stderr)
	if code != 0 || stdout.String() != "native\n" {
		t.Fatalf("absent contract backend = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
}

func TestBackendSelectionFailsClosedWithDetailOnUnsupportedValueOnTargetHost(t *testing.T) {
	t.Parallel()
	var stdout, stderr strings.Builder
	code := run([]string{"--backend", writeBackendContract(t, "go: 1.25.4\ngo-test-macos-backend: docker-linux-amd64\n")}, &stdout, &stderr)
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" {
		if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "go-test-macos-backend") {
			t.Fatalf("unsupported backend on target host = code %d out %q err %q", code, stdout.String(), stderr.String())
		}
		return
	}
	if code != 0 || stdout.String() != "native\n" {
		t.Fatalf("非対象hostで新たな失敗 = code %d out %q err %q", code, stdout.String(), stderr.String())
	}
}

func TestBackendModeReturnsUsageForWrongArity(t *testing.T) {
	t.Parallel()
	cases := [][]string{{"--backend"}, {"--backend", "/tmp/repo", "extra"}}
	for _, args := range cases {
		var stdout, stderr strings.Builder
		code := run(args, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
			t.Fatalf("args %#v = code %d out %q err %q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestStagingModeReturnsUsageForWrongArity(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"--staging"},
		{"--staging", "/tmp/staging"},
		{"--staging", "/tmp/staging", "--responses"},
		{"--staging", "/tmp/staging", "--wrong", "/tmp/responses", "./..."},
	}
	for _, args := range cases {
		var stdout, stderr strings.Builder
		code := run(args, &stdout, &stderr)
		if code != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "usage:") {
			t.Fatalf("args %#v = code %d out %q err %q", args, code, stdout.String(), stderr.String())
		}
	}
}

func TestStagingModeDefaultsOmittedTestArgsToAllPackages(t *testing.T) {
	temp := t.TempDir()
	staging := filepath.Join(temp, "worker", "docker-test")
	responses := filepath.Join(temp, "docker-test-responses")
	for _, dir := range []string{staging, responses} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	type result struct {
		code   int
		stdout string
		stderr string
	}
	done := make(chan result, 1)
	go func() {
		var stdout, stderr strings.Builder
		code := run([]string{"--staging", staging, "--responses", responses}, &stdout, &stderr)
		done <- result{code: code, stdout: stdout.String(), stderr: stderr.String()}
	}()
	deadline := time.Now().Add(5 * time.Second)
	var request dockertest.StagingRequest
	for {
		entries, err := os.ReadDir(staging)
		if err != nil {
			t.Fatal(err)
		}
		served := false
		for _, entry := range entries {
			if !dockertest.IsStagingRequestFileName(entry.Name()) {
				continue
			}
			data, err := os.ReadFile(filepath.Join(staging, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			request, err = dockertest.ParseStagingRequest(data)
			if err != nil {
				t.Fatalf("staging requestのparse失敗: %v", err)
			}
			response := dockertest.StagingResponse{
				SchemaVersion: dockertest.StagingSchemaVersion,
				Token:         request.Token,
				ExitCode:      7,
				Stdout:        "container-all-packages\n",
			}
			body, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(responses, dockertest.StagingResponseFileName(request.Token)), body, 0o600); err != nil {
				t.Fatal(err)
			}
			served = true
			break
		}
		if served {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("staging requestが現れません")
		}
		time.Sleep(5 * time.Millisecond)
	}
	select {
	case outcome := <-done:
		if outcome.code != 7 || !strings.Contains(outcome.stdout, "container-all-packages") {
			t.Fatalf("引数なしstaging実行 = code %d out %q err %q", outcome.code, outcome.stdout, outcome.stderr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("staging実行が完了しません")
	}
	if len(request.Args) != 1 || request.Args[0] != "./..." {
		t.Fatalf("引数なしstaging requestのargs = %#v want [\"./...\"]", request.Args)
	}
}
