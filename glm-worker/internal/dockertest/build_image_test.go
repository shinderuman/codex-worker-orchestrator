package dockertest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
)

func TestImageBuildKeepsClientWritesInsideExecutionTemporaryDirectory(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			root, temporary, bin := t.TempDir(), t.TempDir(), t.TempDir()
			for _, name := range []string{"quality-tools.yml", "install-quality-tools.sh"} {
				if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			script := `#!/bin/sh
set -eu
if [ "$1" = build ]; then
  mkdir -p "$BUILDX_CONFIG/activity"
  printf '%s' "$DOCKER_HOST" > "$BUILDX_CONFIG/activity/endpoint"
  exit "$BUILD_EXIT"
fi
if [ "$1" = image ]; then printf 'sha256:fixture\n'; exit 0; fi
exit 99
`
			if err := execfile.Write(filepath.Join(bin, "docker"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			ambient := filepath.Join(t.TempDir(), "host-buildx")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("BUILDX_CONFIG", ambient)
			t.Setenv("DOCKER_HOST", "unix:///selected/docker.sock")
			t.Setenv("BUILD_EXIT", fmt.Sprint(code))
			image, err := buildImage(context.Background(), root, temporary, "1.25.4", "1.26.4", io.Discard)
			if code == 0 && (err != nil || image != "sha256:fixture") {
				t.Fatalf("image=%q error=%v", image, err)
			}
			var exit *exec.ExitError
			if code != 0 && (!errors.As(err, &exit) || exit.ExitCode() != code || image != "") {
				t.Fatalf("build exit provenance: image=%q error=%v", image, err)
			}
			endpoint, err := os.ReadFile(filepath.Join(temporary, "buildx", "activity", "endpoint"))
			if err != nil || string(endpoint) != "unix:///selected/docker.sock" {
				t.Fatalf("client state or endpoint: %q %v", endpoint, err)
			}
			if _, err := os.Stat(ambient); !os.IsNotExist(err) {
				t.Fatalf("ambient buildx state was touched: %v", err)
			}
			x := execution{ctx: context.Background(), root: root, temporary: temporary}
			if err := x.cleanup(); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(temporary); !os.IsNotExist(err) {
				t.Fatalf("client state cleanup: %v", err)
			}
		})
	}
}
