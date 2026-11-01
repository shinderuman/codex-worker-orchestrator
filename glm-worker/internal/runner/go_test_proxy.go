package runner

import (
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
      -C|--C) expect=dir ;;
      -C=*|--C=*) ;;
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
