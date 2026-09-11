#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/quality-tools-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
repo="$tmp/repo"
home="$tmp/home"
shared="$home/.local/bin"
fake="$tmp/fake-bin"
mkdir -p "$repo" "$shared" "$fake"
cp "$source_root/install-quality-tools.sh" "$source_root/quality-tools.yml" "$repo/"

for tool in shfmt golangci-lint shellcheck; do
	cat >"$shared/$tool" <<EOF_USER_TOOL
#!/bin/sh
printf '%s\n' 'user-owned-$tool 9.9.9'
EOF_USER_TOOL
	chmod +x "$shared/$tool"
done
shfmt_hash=$(shasum -a 256 "$shared/shfmt")
golangci_hash=$(shasum -a 256 "$shared/golangci-lint")
shellcheck_hash=$(shasum -a 256 "$shared/shellcheck")

cat >"$fake/go" <<'EOF_GO'
#!/bin/sh
set -eu
if [ "${1:-}" = version ]; then
	printf 'go version %s linux/amd64\n' "${GOTOOLCHAIN:-go0.0.0}"
	exit 0
fi
if [ "${1:-}" = install ]; then
	version=${2##*@v}
	mkdir -p "$GOBIN"
	cat >"$GOBIN/shfmt" <<EOF_SHFMT
#!/bin/sh
printf '%s\\n' 'v$version'
EOF_SHFMT
	chmod +x "$GOBIN/shfmt"
	exit 0
fi
exit 1
EOF_GO

cat >"$fake/curl" <<'EOF_CURL'
#!/bin/sh
set -eu
output=
while [ "$#" -gt 0 ]; do
	case "$1" in
	-o)
		output=$2
		shift 2
		;;
	*) shift ;;
	esac
done
: >"$output"
EOF_CURL

cat >"$fake/uname" <<'EOF_UNAME'
#!/bin/sh
case "${1:-}" in
-s) printf '%s\n' Linux ;;
-m) printf '%s\n' x86_64 ;;
*) printf '%s\n' Linux ;;
esac
EOF_UNAME

cat >"$fake/tar" <<'EOF_TAR'
#!/bin/sh
set -eu
archive=
destination=
previous=
for arg in "$@"; do
	if [ "$previous" = -C ]; then
		destination=$arg
		previous=
		continue
	fi
	if [ "$arg" = -C ]; then
		previous=-C
		continue
	fi
	case "$arg" in
	*.tar.gz | *.tar.xz) archive=$arg ;;
	esac
done
case "$archive" in
*golangci-lint*)
	target="$destination/golangci-lint-2.7.0-linux-amd64/golangci-lint"
	mkdir -p "$(dirname "$target")"
	cat >"$target" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'golangci-lint has version 2.7.0 built with go1.25.4'
EOF_TOOL
	;;
*shellcheck*)
	target="$destination/shellcheck-v0.11.0/shellcheck"
	mkdir -p "$(dirname "$target")"
	cat >"$target" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'version: 0.11.0'
EOF_TOOL
	;;
*) exit 1 ;;
esac
chmod +x "$target"
EOF_TAR
chmod +x "$fake/go" "$fake/curl" "$fake/uname" "$fake/tar"

run_install() {
	HOME="$home" QUALITY_TOOLS_BIN_DIR="$shared" PATH="$fake:$PATH" "$repo/install-quality-tools.sh"
}

assert_user_tools_unchanged() {
	test "$(shasum -a 256 "$shared/shfmt")" = "$shfmt_hash"
	test "$(shasum -a 256 "$shared/golangci-lint")" = "$golangci_hash"
	test "$(shasum -a 256 "$shared/shellcheck")" = "$shellcheck_hash"
}

run_install
assert_user_tools_unchanged
old_shfmt="$shared/codex-worker-orchestrator-shfmt-3.13.1"
golangci="$shared/codex-worker-orchestrator-golangci-lint-2.7.0"
shellcheck="$shared/codex-worker-orchestrator-shellcheck-0.11.0"
test -x "$old_shfmt"
test -x "$golangci"
test -x "$shellcheck"
old_shfmt_hash=$(shasum -a 256 "$old_shfmt")

run_install
assert_user_tools_unchanged
test "$(shasum -a 256 "$old_shfmt")" = "$old_shfmt_hash"

awk '{ if ($1 == "shfmt:") print "shfmt: 3.13.2"; else print }' "$repo/quality-tools.yml" >"$repo/quality-tools.yml.next"
mv "$repo/quality-tools.yml.next" "$repo/quality-tools.yml"
run_install
assert_user_tools_unchanged
new_shfmt="$shared/codex-worker-orchestrator-shfmt-3.13.2"
test -x "$new_shfmt"
test "$(shasum -a 256 "$old_shfmt")" = "$old_shfmt_hash"

cat >"$new_shfmt" <<'EOF_COLLISION'
#!/bin/sh
printf '%s\n' 'v9.9.9'
EOF_COLLISION
chmod +x "$new_shfmt"
collision_hash=$(shasum -a 256 "$new_shfmt")
if run_install >"$tmp/collision.stdout" 2>"$tmp/collision.stderr"; then
	printf '%s\n' 'quality tool collision was silently overwritten' >&2
	exit 1
fi
grep -Fq 'quality tool collision:' "$tmp/collision.stderr"
test "$(shasum -a 256 "$new_shfmt")" = "$collision_hash"
assert_user_tools_unchanged
