#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/codex-install-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
repo="$tmp/repo"
home="$tmp/home"
mkdir -p "$repo" "$home/.codex" "$home/.claude" "$tmp/bin"
rsync -a --exclude .git "$source_root/" "$repo/"
printf '%s\n' 'local_key = "keep"' >"$home/.codex/config.toml"
printf '%s\n' '{"permissions":{"allow":["local"]},"env":{"LOCAL":"keep"}}' >"$home/.claude/settings.json"
cat >"$tmp/bin/claude" <<'EOF_CLAUDE'
#!/bin/sh
if [ "${1:-}" = "--help" ]; then
    printf '%s\n' '--json-schema'
    exit 0
fi
exit 1
EOF_CLAUDE
chmod +x "$tmp/bin/claude"
for tool in golangci-lint shellcheck shfmt; do
	cat >"$tmp/bin/$tool" <<'EOF_TOOL'
#!/bin/sh
exit 0
EOF_TOOL
	chmod +x "$tmp/bin/$tool"
done

run_install() {
	HOME="$home" \
		PATH="$tmp/bin:$PATH" \
		CODEX_CONFIG_DIR="$home/.codex" \
		GLM_WORKER_BIN_DIR="$home/.local/bin" \
		GLM_WORKER_HOME="$home/.glm-worker" \
		CLAUDE_SETTINGS_FILE="$home/.claude/settings.json" \
		XDG_CONFIG_HOME="$home/.config" \
		"$repo/install.sh"
}

run_install
for binary in glm-worker commentlint harnesslint merge-json; do
	test -x "$home/.local/bin/$binary"
done
test -f "$home/.codex/AGENTS.md"
test -f "$home/.codex/glm-worker/prompts/WORKER.md"
grep -q '^local_key = "keep"$' "$home/.codex/config.toml"
grep -q '^background_terminal_max_timeout = ' "$home/.codex/config.toml"
grep -q '"LOCAL": "keep"' "$home/.claude/settings.json"
grep -q '"permissions"' "$home/.claude/settings.json"
find "$home/.codex" "$home/.claude" "$home/.local/bin" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$tmp/first.sha"
run_install
find "$home/.codex" "$home/.claude" "$home/.local/bin" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$tmp/second.sha"
cmp "$tmp/first.sha" "$tmp/second.sha"

missing_bin="$tmp/missing-bin"
mkdir -p "$missing_bin"
ln -s "$(command -v dirname)" "$missing_bin/dirname"
for tool in git go rsync cmp awk grep install golangci-lint; do
	cat >"$missing_bin/$tool" <<'EOF_TOOL'
#!/bin/sh
exit 0
EOF_TOOL
	chmod +x "$missing_bin/$tool"
done
missing_stderr="$tmp/missing.stderr"
if PATH="$missing_bin" "$repo/install.sh" >"$tmp/missing.stdout" 2>"$missing_stderr"; then
	printf '%s\n' 'install missing dependency: expected failure' >&2
	exit 1
fi
test ! -s "$tmp/missing.stdout"
grep -Fxq 'required command not found: shellcheck' "$missing_stderr"
grep -Fxq 'install with Homebrew: brew install shellcheck' "$missing_stderr"
printf '%s\n' 'install smoke: pass'
