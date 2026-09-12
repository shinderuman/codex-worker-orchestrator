#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
quality_tools_file="$repo_root/quality-tools.yml"
codex_dir="${CODEX_CONFIG_DIR:-${CODEX_HOME:-$HOME/.codex}}"
bin_dir="${GLM_WORKER_BIN_DIR:-$HOME/.local/bin}"
glm_worker_home="${GLM_WORKER_HOME:-$HOME/.glm-worker}"

quality_contract_value() {
	awk -F': ' -v key="$1" '$1 == key { print $2; exit }' "$quality_tools_file"
}

quality_tool_bin_dir() {
	default_dir=$1
	if [ -n "${QUALITY_TOOLS_BIN_DIR:-}" ]; then
		case "$QUALITY_TOOLS_BIN_DIR" in
		/*) printf '%s\n' "$QUALITY_TOOLS_BIN_DIR" ;;
		*) printf '%s/%s\n' "$repo_root" "$QUALITY_TOOLS_BIN_DIR" ;;
		esac
		return
	fi
	printf '%s/%s\n' "$HOME" "$default_dir"
}

quality_tool_path() {
	printf '%s/%s-%s-%s\n' "$QUALITY_TOOLS_RESOLVED_BIN_DIR" "$QUALITY_TOOL_NAMESPACE" "$1" "$2"
}

require() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "$1" >&2
		exit 1
	fi
}

quality_tool_version() {
	command_name=$1
	command_path=$2
	case "$command_name" in
	golangci-lint) "$command_path" version ;;
	shellcheck) "$command_path" --version ;;
	shfmt) "$command_path" --version ;;
	esac | awk 'match($0, /[0-9]+\.[0-9]+\.[0-9]+/) { print substr($0, RSTART, RLENGTH); exit }'
}

require_quality_tool() {
	command_name=$1
	required_version=$2
	command_path=$(quality_tool_path "$command_name" "$required_version")
	if { [ -e "$command_path" ] || [ -L "$command_path" ]; } && [ ! -x "$command_path" ]; then
		printf 'quality tool collision: %s exists and is not executable; refusing to overwrite\n' "$command_path" >&2
		exit 1
	fi
	if [ ! -x "$command_path" ]; then
		printf 'required quality tool not found at canonical path: %s\n' "$command_path" >&2
		printf '%s\n' 'install required versions with: ./install-quality-tools.sh' >&2
		exit 1
	fi
	installed_version=$(quality_tool_version "$command_name" "$command_path")
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
		go build -buildvcs=false -trimpath -o "$build_dir/repo-cli-install" ./cmd/repo-cli-install
		go build -buildvcs=false -trimpath -o "$build_dir/commentlint" ./cmd/commentlint
		go build -buildvcs=false -trimpath -o "$build_dir/harnesslint" ./cmd/harnesslint
		go build -buildvcs=false -trimpath -o "$build_dir/merge-json" ./cmd/merge-json
		go build -buildvcs=false -trimpath -o "$build_dir/plancheck" ./cmd/plancheck
	)
}

install_binaries() {
	build_dir=$1
	"$build_dir/repo-cli-install" -mode install -build-dir "$build_dir" -bin-dir "$bin_dir"
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
	sh "$repo_root/scripts/manage-pull-hook.sh" install "$repo_root"
}

require git
require go
require rsync
require cmp
require awk
require grep
QUALITY_TOOL_NAMESPACE=$(quality_contract_value namespace)
QUALITY_TOOLS_DEFAULT_BIN_DIR=$(quality_contract_value default-bin-dir)
QUALITY_TOOLS_RESOLVED_BIN_DIR=$(quality_tool_bin_dir "$QUALITY_TOOLS_DEFAULT_BIN_DIR")
GO_VERSION=$(quality_contract_value go)
LINT_GO_VERSION=$(quality_contract_value lint-go)
GOLANGCI_LINT_VERSION=$(quality_contract_value golangci-lint)
SHELLCHECK_VERSION=$(quality_contract_value shellcheck)
SHFMT_VERSION=$(quality_contract_value shfmt)
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
