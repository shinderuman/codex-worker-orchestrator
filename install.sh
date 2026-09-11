#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
quality_tools_file="$repo_root/quality-tools.yml"
codex_dir="${CODEX_CONFIG_DIR:-${CODEX_HOME:-$HOME/.codex}}"
bin_dir="${GLM_WORKER_BIN_DIR:-$HOME/.local/bin}"
glm_worker_home="${GLM_WORKER_HOME:-$HOME/.glm-worker}"

require() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "$1" >&2
		exit 1
	fi
}

require_quality_command() {
	command_name=$1
	if ! command -v "$command_name" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "$command_name" >&2
		printf '%s\n' 'install required versions with: ./install-quality-tools.sh' >&2
		exit 1
	fi
}

quality_tool_version() {
	case "$1" in
	golangci-lint) golangci-lint version ;;
	shellcheck) shellcheck --version ;;
	shfmt) shfmt --version ;;
	esac | awk 'match($0, /[0-9]+\.[0-9]+\.[0-9]+/) { print substr($0, RSTART, RLENGTH); exit }'
}

require_quality_tool() {
	command_name=$1
	required_version=$2
	require_quality_command "$command_name"
	installed_version=$(quality_tool_version "$command_name")
	if [ "$installed_version" != "$required_version" ]; then
		printf 'quality tool version mismatch: %s=%s, required=%s\n' "$command_name" "${installed_version:-unknown}" "$required_version" >&2
		printf '%s\n' 'install required versions with: ./install-quality-tools.sh' >&2
		exit 1
	fi
}

verify_go_toolchain() {
	tool_name=$1
	required_version=$2
	installed_version=$(GOTOOLCHAIN="go$required_version" go version | awk 'match($0, /go[0-9]+\.[0-9]+\.[0-9]+/) { print substr($0, RSTART + 2, RLENGTH - 2); exit }')
	if [ "$installed_version" != "$required_version" ]; then
		printf 'quality tool version mismatch: %s=%s, required=%s\n' "$tool_name" "${installed_version:-unknown}" "$required_version" >&2
		printf '%s\n' 'install required versions with: ./install-quality-tools.sh' >&2
		exit 1
	fi
}

install_codex_configuration() {
	build_dir=$1
	"$build_dir/codex-install" --repo-root "$repo_root" --codex-dir "$codex_dir"
}

build_binaries() {
	build_dir=$1
	(
		cd "$repo_root/glm-worker"
		go build -trimpath -o "$build_dir/glm-worker" ./cmd/glm-worker
		go build -buildvcs=false -trimpath -o "$build_dir/glm-parent-action" ./cmd/glm-parent-action
		go build -buildvcs=false -trimpath -o "$build_dir/glm-codex-context" ./cmd/glm-codex-context
		go build -buildvcs=false -trimpath -o "$build_dir/codex-install" ./cmd/codex-install
		go build -buildvcs=false -trimpath -o "$build_dir/commentlint" ./cmd/commentlint
		go build -buildvcs=false -trimpath -o "$build_dir/harnesslint" ./cmd/harnesslint
		go build -buildvcs=false -trimpath -o "$build_dir/merge-json" ./cmd/merge-json
		go build -buildvcs=false -trimpath -o "$build_dir/plancheck" ./cmd/plancheck
	)
}

install_binaries() {
	build_dir=$1
	mkdir -p "$bin_dir"
	for name in glm-worker glm-parent-action glm-codex-context commentlint harnesslint; do
		if [ -f "$bin_dir/$name" ] && cmp -s "$build_dir/$name" "$bin_dir/$name"; then
			printf '%s: unchanged\n' "$name"
		else
			install -m 0755 "$build_dir/$name" "$bin_dir/$name"
			printf 'installed: %s\n' "$bin_dir/$name"
		fi
	done
}

verify_claude_cli() {
	claude_bin="${GLM_WORKER_CLAUDE_BIN:-claude}"
	if ! command -v "$claude_bin" >/dev/null 2>&1; then
		printf 'claude cli: %s not on PATH; skipping contract check (glm-worker requires it at runtime)\n' "$claude_bin"
		return
	fi
	if ! "$claude_bin" --help 2>/dev/null | grep -q -- '--json-schema'; then
		printf 'claude cli: %s does not support --json-schema; upgrade Claude Code before install\n' "$claude_bin" >&2
		return 1
	fi
	printf 'claude cli: %s supports --json-schema\n' "$claude_bin"
}

merge_claude_settings() {
	build_dir=$1
	result=$("$build_dir/merge-json" -fragment "$repo_root/claude/settings-managed.json")
	printf 'claude settings: %s\n' "$result"
}

install_pull_hook() {
	if ! git -C "$repo_root" rev-parse --git-dir >/dev/null 2>&1; then
		printf '%s\n' 'git hook: skipped'
		return
	fi
	chmod +x "$repo_root/.githooks/post-merge"
	current=$(git -C "$repo_root" config --local --get core.hooksPath || true)
	if [ "$current" = '.githooks' ]; then
		printf '%s\n' 'git hook: unchanged'
		return
	fi
	git -C "$repo_root" config --local core.hooksPath .githooks
	printf '%s\n' 'git hook: enabled'
}

require git
require go
require rsync
require cmp
require awk
require grep
require install
GO_VERSION=$(awk -F': ' '$1 == "go" { print $2 }' "$quality_tools_file")
LINT_GO_VERSION=$(awk -F': ' '$1 == "lint-go" { print $2 }' "$quality_tools_file")
GOLANGCI_LINT_VERSION=$(awk -F': ' '$1 == "golangci-lint" { print $2 }' "$quality_tools_file")
SHELLCHECK_VERSION=$(awk -F': ' '$1 == "shellcheck" { print $2 }' "$quality_tools_file")
SHFMT_VERSION=$(awk -F': ' '$1 == "shfmt" { print $2 }' "$quality_tools_file")
export GOTOOLCHAIN="go$GO_VERSION"
verify_go_toolchain go "$GO_VERSION"
verify_go_toolchain lint-go "$LINT_GO_VERSION"
require_quality_tool golangci-lint "$GOLANGCI_LINT_VERSION"
require_quality_tool shellcheck "$SHELLCHECK_VERSION"
require_quality_tool shfmt "$SHFMT_VERSION"

build_dir=$(mktemp -d "${TMPDIR:-/tmp}/codex-worker-orchestrator-build.XXXXXX")
trap 'rm -rf "$build_dir"' EXIT HUP INT TERM
printf '%s\n' 'preflight: building runtime binaries'
build_binaries "$build_dir"
"$build_dir/plancheck" "$repo_root"
verify_claude_cli
install_binaries "$build_dir"
install_codex_configuration "$build_dir"
merge_claude_settings "$build_dir"
install_pull_hook
mkdir -p "$glm_worker_home/sessions"
rm -rf "$build_dir"
trap - EXIT HUP INT TERM
printf '%s\n' 'install complete'
printf '%s\n' 'Codexルールの再読込を保証するには、新しいCodexタスクを開始してください。'
