#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
codex_dir="${CODEX_CONFIG_DIR:-${CODEX_HOME:-$HOME/.codex}}"
bin_dir="${GLM_WORKER_BIN_DIR:-$HOME/.local/bin}"

if ! command -v go >/dev/null 2>&1; then
    printf '%s\n' 'uninstall: Go toolchain is required to run the ownership-safe local uninstaller' >&2
    exit 1
fi

(
    cd "$repo_root/glm-worker"
    GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOWORK=off go run ./cmd/uninstall \
        -repo-root "$repo_root" -bin-dir "$bin_dir" -codex-dir "$codex_dir"
)
