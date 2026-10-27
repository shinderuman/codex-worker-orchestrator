package goqualitycmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
