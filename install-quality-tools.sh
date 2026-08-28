#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
contract="$repo_root/quality-tools.yml"
bin_dir="${QUALITY_TOOLS_BIN_DIR:-$HOME/.local/bin}"
go_version=$(awk -F': ' '$1 == "go" { print $2 }' "$contract")
lint_go_version=$(awk -F': ' '$1 == "lint-go" { print $2 }' "$contract")
golangci_version=$(awk -F': ' '$1 == "golangci-lint" { print $2 }' "$contract")
shellcheck_version=$(awk -F': ' '$1 == "shellcheck" { print $2 }' "$contract")
shfmt_version=$(awk -F': ' '$1 == "shfmt" { print $2 }' "$contract")

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

tmp=$(mktemp -d "${TMPDIR:-/tmp}/codex-quality-tools.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$bin_dir"

GOTOOLCHAIN="go$go_version" go version >/dev/null
GOTOOLCHAIN="go$lint_go_version" go version >/dev/null
GOTOOLCHAIN="go$go_version" GOBIN="$bin_dir" go install "mvdan.cc/sh/v3/cmd/shfmt@v$shfmt_version"

golangci_archive="golangci-lint-$golangci_version-$golangci_platform.tar.gz"
curl -sSfL "https://github.com/golangci/golangci-lint/releases/download/v$golangci_version/$golangci_archive" -o "$tmp/golangci-lint.tar.gz"
tar -xzf "$tmp/golangci-lint.tar.gz" -C "$tmp"
install -m 0755 "$tmp/golangci-lint-$golangci_version-$golangci_platform/golangci-lint" "$bin_dir/golangci-lint"

shellcheck_archive="shellcheck-v$shellcheck_version.$shellcheck_platform.tar.xz"
curl -sSfL "https://github.com/koalaman/shellcheck/releases/download/v$shellcheck_version/$shellcheck_archive" -o "$tmp/shellcheck.tar.xz"
tar -xJf "$tmp/shellcheck.tar.xz" -C "$tmp"
install -m 0755 "$tmp/shellcheck-v$shellcheck_version/shellcheck" "$bin_dir/shellcheck"

printf 'quality tools installed: %s\n' "$bin_dir"
printf 'ensure PATH includes: %s\n' "$bin_dir"
