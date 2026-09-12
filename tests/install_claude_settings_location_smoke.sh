#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
go_mod_cache=$(go env GOMODCACHE)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/claude-settings-location-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
repo="$tmp/repo"
home="$tmp/home"
custom_dir="$tmp/custom-claude"
custom_settings="$custom_dir/settings.json"
mkdir -p "$repo" "$home/.codex" "$home/.local/bin" "$tmp/bin"
rsync -a --exclude .git --exclude .codex "$source_root/" "$repo/"
git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" -c user.name=install-smoke -c user.email=install-smoke@example.invalid commit -qm fixture

cat >"$tmp/bin/claude" <<'EOF_CLAUDE'
#!/bin/sh
if [ "${1:-}" = "--help" ]; then
	printf '%s\n' '--json-schema'
	exit 0
fi
exit 1
EOF_CLAUDE
chmod +x "$tmp/bin/claude"

golangci_lint_version=$(awk -F': ' '$1 == "golangci-lint" { print $2 }' "$repo/quality-tools.yml")
shellcheck_version=$(awk -F': ' '$1 == "shellcheck" { print $2 }' "$repo/quality-tools.yml")
shfmt_version=$(awk -F': ' '$1 == "shfmt" { print $2 }' "$repo/quality-tools.yml")
cat >"$tmp/bin/golangci-lint" <<EOF_TOOL
#!/bin/sh
printf '%s\\n' 'golangci-lint has version $golangci_lint_version'
EOF_TOOL
cat >"$tmp/bin/shellcheck" <<EOF_TOOL
#!/bin/sh
printf '%s\\n' 'version: $shellcheck_version'
EOF_TOOL
cat >"$tmp/bin/shfmt" <<EOF_TOOL
#!/bin/sh
printf '%s\\n' 'v$shfmt_version'
EOF_TOOL
chmod +x "$tmp/bin/golangci-lint" "$tmp/bin/shellcheck" "$tmp/bin/shfmt"

HOME="$home" \
	GOMODCACHE="$go_mod_cache" \
	PATH="$tmp/bin:$PATH" \
	CODEX_HOME="$home/.codex" \
	GLM_WORKER_BIN_DIR="$home/.local/bin" \
	GLM_WORKER_HOME="$home/.glm-worker" \
	CLAUDE_CONFIG_DIR='' \
	CLAUDE_SETTINGS_FILE="$custom_settings" \
	XDG_CONFIG_HOME="$home/.config" \
	"$repo/install.sh"

test -f "$custom_settings"
grep -q '"ANTHROPIC_BASE_URL"' "$custom_settings"
test ! -e "$home/.claude/settings.json"

conflict_dir="$tmp/conflicting-claude"
if HOME="$home" \
	GOMODCACHE="$go_mod_cache" \
	PATH="$tmp/bin:$PATH" \
	CODEX_HOME="$home/.codex" \
	GLM_WORKER_BIN_DIR="$home/.local/bin" \
	GLM_WORKER_HOME="$home/.glm-worker" \
	CLAUDE_CONFIG_DIR="$conflict_dir" \
	CLAUDE_SETTINGS_FILE="$custom_settings" \
	XDG_CONFIG_HOME="$home/.config" \
	"$repo/install.sh" >"$tmp/conflict.stdout" 2>"$tmp/conflict.stderr"; then
	printf '%s\n' 'installer accepted conflicting Claude settings locations' >&2
	exit 1
fi
grep -Fq 'different settings files' "$tmp/conflict.stderr"

printf '%s\n' 'claude settings location smoke: ok'
