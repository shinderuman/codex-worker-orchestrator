package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const goTestProxyTemplate = `#!/bin/sh
real_go=__REAL_GO__
go_guard_subcommand() {
  expect=
  for arg do
    if [ -n "$expect" ]; then
      expect=
      continue
    fi
    case "$arg" in
      -C) expect=dir ;;
      -*) ;;
      *) printf '%s\n' "$arg"; return ;;
    esac
  done
}
subcommand=$(go_guard_subcommand "$@")
if [ "$subcommand" = test ]; then
  printf '%s\n' '{"error":{"kind":"containment_denial","owner":"go-test-docker-policy","reason":"native_go_test_blocked","next_action":"use_repository_goquality_test_entry_docker_forced"}}' >&2
  exit 97
fi
exec "$real_go" "$@"
`

func goTestProxyScript(realGo string) string {
	return strings.NewReplacer(
		"__REAL_GO__", shellSingleQuote(realGo),
	).Replace(goTestProxyTemplate)
}

func resolveDockerDaemonSocket() string {
	if host := os.Getenv("DOCKER_HOST"); host != "" {
		return unixSocketEndpointPath(host)
	}
	docker, err := exec.LookPath("docker")
	if err != nil {
		return ""
	}
	data, err := exec.Command(docker, "context", "inspect", "--format", "{{.Endpoints.docker.Host}}").Output()
	if err != nil {
		return ""
	}
	return unixSocketEndpointPath(strings.TrimSpace(string(data)))
}

func unixSocketEndpointPath(endpoint string) string {
	if !strings.HasPrefix(endpoint, "unix://") {
		return ""
	}
	path := filepath.Clean(strings.TrimPrefix(endpoint, "unix://"))
	if !filepath.IsAbs(path) {
		return ""
	}
	return path
}
