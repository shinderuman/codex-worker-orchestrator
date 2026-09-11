#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
contract="$repo_root/quality-tools.yml"
contract_value() {
	awk -F': ' -v key="$1" '$1 == key { print $2; exit }' "$contract"
}

tool_namespace=$(contract_value namespace)
default_bin_dir=$(contract_value default-bin-dir)
go_version=$(contract_value go)
lint_go_version=$(contract_value lint-go)
golangci_version=$(contract_value golangci-lint)
shellcheck_version=$(contract_value shellcheck)
shfmt_version=$(contract_value shfmt)

if [ -n "${QUALITY_TOOLS_BIN_DIR:-}" ]; then
	case "$QUALITY_TOOLS_BIN_DIR" in
	/*) bin_dir=$QUALITY_TOOLS_BIN_DIR ;;
	*) bin_dir="$repo_root/$QUALITY_TOOLS_BIN_DIR" ;;
	esac
else
	bin_dir="$HOME/$default_bin_dir"
fi

for command_name in go curl tar awk install uname mktemp; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		printf 'required command not found: %s\n' "$command_name" >&2
		exit 1
	fi
done

case "$(uname -s):$(uname -m)" in
Darwin:arm64)
	golangci_platform=darwin-arm64
	shellcheck_platform=darwin.aarch64
	;;
Darwin:x86_64)
	golangci_platform=darwin-amd64
	shellcheck_platform=darwin.x86_64
	;;
Linux:aarch64 | Linux:arm64)
	golangci_platform=linux-arm64
	shellcheck_platform=linux.aarch64
	;;
Linux:x86_64)
	golangci_platform=linux-amd64
	shellcheck_platform=linux.x86_64
	;;
*)
	printf 'unsupported quality tool platform: %s:%s\n' "$(uname -s)" "$(uname -m)" >&2
	exit 1
	;;
esac

quality_tool_path() {
	printf '%s/%s-%s-%s\n' "$bin_dir" "$tool_namespace" "$1" "$2"
}

quality_tool_version() {
	tool_name=$1
	tool_path=$2
	case "$tool_name" in
	golangci-lint) "$tool_path" version ;;
	shellcheck) "$tool_path" --version ;;
	shfmt) "$tool_path" --version ;;
	esac | awk 'match($0, /[0-9]+\.[0-9]+\.[0-9]+/) { print substr($0, RSTART, RLENGTH); exit }'
}

target_needs_install() {
	tool_name=$1
	required_version=$2
	target=$3
	if [ ! -e "$target" ] && [ ! -L "$target" ]; then
		return 0
	fi
	if [ ! -x "$target" ]; then
		printf 'quality tool collision: %s exists and is not executable; refusing to overwrite\n' "$target" >&2
		exit 1
	fi
	observed=$(quality_tool_version "$tool_name" "$target")
	if [ "$observed" = "$required_version" ]; then
		printf '%s: unchanged (%s)\n' "$tool_name" "$target"
		return 1
	fi
	printf 'quality tool collision: %s exists with version %s, required %s; refusing to overwrite\n' "$target" "${observed:-unknown}" "$required_version" >&2
	exit 1
}

tmp=$(mktemp -d "${TMPDIR:-/tmp}/codex-quality-tools.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$bin_dir"

GOTOOLCHAIN="go$go_version" go version >/dev/null
GOTOOLCHAIN="go$lint_go_version" go version >/dev/null

shfmt_target=$(quality_tool_path shfmt "$shfmt_version")
if target_needs_install shfmt "$shfmt_version" "$shfmt_target"; then
	mkdir -p "$tmp/go-bin"
	GOTOOLCHAIN="go$go_version" GOBIN="$tmp/go-bin" go install "mvdan.cc/sh/v3/cmd/shfmt@v$shfmt_version"
	install -m 0755 "$tmp/go-bin/shfmt" "$shfmt_target"
	printf 'installed: %s\n' "$shfmt_target"
fi

golangci_target=$(quality_tool_path golangci-lint "$golangci_version")
if target_needs_install golangci-lint "$golangci_version" "$golangci_target"; then
	golangci_archive="golangci-lint-$golangci_version-$golangci_platform.tar.gz"
	curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v$golangci_version/$golangci_archive" -o "$tmp/golangci-lint.tar.gz"
	tar -xzf "$tmp/golangci-lint.tar.gz" -C "$tmp"
	install -m 0755 "$tmp/golangci-lint-$golangci_version-$golangci_platform/golangci-lint" "$golangci_target"
	printf 'installed: %s\n' "$golangci_target"
fi

shellcheck_target=$(quality_tool_path shellcheck "$shellcheck_version")
if target_needs_install shellcheck "$shellcheck_version" "$shellcheck_target"; then
	shellcheck_archive="shellcheck-v$shellcheck_version.$shellcheck_platform.tar.xz"
	curl -sSfL "https://github.com/koalaman/shellcheck/releases/download/v$shellcheck_version/$shellcheck_archive" -o "$tmp/shellcheck.tar.xz"
	tar -xJf "$tmp/shellcheck.tar.xz" -C "$tmp"
	install -m 0755 "$tmp/shellcheck-v$shellcheck_version/shellcheck" "$shellcheck_target"
	printf 'installed: %s\n' "$shellcheck_target"
fi

printf 'quality tools ready: %s\n' "$bin_dir"
