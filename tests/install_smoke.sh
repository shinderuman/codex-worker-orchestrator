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
cat >"$tmp/bin/golangci-lint" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'golangci-lint has version 2.7.0 built with go1.25.4'
EOF_TOOL
cat >"$tmp/bin/shellcheck" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'version: 0.11.0'
EOF_TOOL
cat >"$tmp/bin/shfmt" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'v3.13.1'
EOF_TOOL
chmod +x "$tmp/bin/golangci-lint" "$tmp/bin/shellcheck" "$tmp/bin/shfmt"

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
cmp "$repo/codex/instructions/glm-repo-search.md" "$home/.codex/instructions/glm-repo-search.md"
grep -q 'GLM_WORKER_REPO_SEARCH' "$home/.codex/instructions/glm-repo-search.md"
HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --help >"$tmp/repo-search-help.json"
grep -q -- '--repo-search' "$tmp/repo-search-help.json"
HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" GLM_WORKER_REPO_SEARCH=0 "$home/.local/bin/glm-worker" --repo-search smoke >"$tmp/repo-search-disabled.json"
grep -q '"status":"disabled"' "$tmp/repo-search-disabled.json"
grep -q '"result":"disabled"' "$tmp/repo-search-disabled.json"
find "$home/.codex" "$home/.claude" "$home/.local/bin" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$tmp/first.sha"
run_install
find "$home/.codex" "$home/.claude" "$home/.local/bin" -type f -exec shasum -a 256 {} \; | LC_ALL=C sort >"$tmp/second.sha"
cmp "$tmp/first.sha" "$tmp/second.sha"

missing_bin="$tmp/missing-bin"
mkdir -p "$missing_bin"
ln -s "$(command -v dirname)" "$missing_bin/dirname"
for tool in git rsync cmp awk grep install; do
	cat >"$missing_bin/$tool" <<'EOF_TOOL'
#!/bin/sh
exit 0
EOF_TOOL
	chmod +x "$missing_bin/$tool"
done
cat >"$missing_bin/go" <<'EOF_TOOL'
#!/bin/sh
case "${GOTOOLCHAIN:-}" in
go1.22.12) printf '%s\n' 'go version go1.22.12 darwin/arm64' ;;
*) printf '%s\n' 'go version go1.25.4 darwin/arm64' ;;
esac
EOF_TOOL
cat >"$missing_bin/golangci-lint" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'golangci-lint has version 2.7.0 built with go1.25.4'
EOF_TOOL
chmod +x "$missing_bin/go" "$missing_bin/golangci-lint"
missing_stderr="$tmp/missing.stderr"
if PATH="$missing_bin" "$repo/install.sh" >"$tmp/missing.stdout" 2>"$missing_stderr"; then
	printf '%s\n' 'install missing dependency: expected failure' >&2
	exit 1
fi
test ! -s "$tmp/missing.stdout"
missing_command_error='required command not found: shellcheck'
missing_brew_hint='install required versions with: ./install-quality-tools.sh'
grep -Fxq "$missing_command_error" "$missing_stderr"
grep -Fxq "$missing_brew_hint" "$missing_stderr"

mismatch_bin="$tmp/mismatch-bin"
cp -R "$missing_bin" "$mismatch_bin"
rm "$mismatch_bin/awk"
ln -s "$(command -v awk)" "$mismatch_bin/awk"
cat >"$mismatch_bin/shellcheck" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'version: 0.10.0'
EOF_TOOL
cat >"$mismatch_bin/shfmt" <<'EOF_TOOL'
#!/bin/sh
printf '%s\n' 'v3.13.1'
EOF_TOOL
chmod +x "$mismatch_bin/shellcheck" "$mismatch_bin/shfmt"
mismatch_stderr="$tmp/mismatch.stderr"
if PATH="$mismatch_bin" "$repo/install.sh" >"$tmp/mismatch.stdout" 2>"$mismatch_stderr"; then
	printf '%s\n' 'install version mismatch: expected failure' >&2
	exit 1
fi
test ! -s "$tmp/mismatch.stdout"
grep -Fq 'shellcheck=0.10.0' "$mismatch_stderr"
grep -Fq 'required=0.11.0' "$mismatch_stderr"
grep -Fq './install-quality-tools.sh' "$mismatch_stderr"

standard_bin="$tmp/standard-bin"
mkdir -p "$standard_bin"
ln -s "$(command -v dirname)" "$standard_bin/dirname"
standard_stderr="$tmp/standard.stderr"
if PATH="$standard_bin" "$repo/install.sh" >"$tmp/standard.stdout" 2>"$standard_stderr"; then
	printf '%s\n' 'install missing standard command: expected failure' >&2
	exit 1
fi
test ! -s "$tmp/standard.stdout"
grep -Fxq 'required command not found: git' "$standard_stderr"
if grep -Fq 'brew install' "$standard_stderr"; then
	printf '%s\n' 'install missing standard command: unexpected Homebrew hint' >&2
	exit 1
fi
printf '%s\n' 'install smoke: pass'
