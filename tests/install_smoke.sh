#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
go_mod_cache=$(go env GOMODCACHE)
tmp=$(mktemp -d "${TMPDIR:-/tmp}/codex-install-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
repo="$tmp/repo"
home="$tmp/home"
mkdir -p "$repo" "$home/.codex" "$home/.claude" "$home/.local/bin" "$tmp/bin"
rsync -a --exclude .git --exclude .codex "$source_root/" "$repo/"
git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" -c user.name=install-smoke -c user.email=install-smoke@example.invalid commit -qm fixture
repo_revision=$(git -C "$repo" rev-parse HEAD)
printf '%s\n' 'local_key = "keep"' >"$home/.codex/config.toml"
printf '%s\n' '{"permissions":{"allow":["local"]},"env":{"LOCAL":"keep","REMOVE_ME":"local"}}' >"$home/.claude/settings.json"
cat >"$home/.local/bin/merge-json" <<'EOF_STALE_MERGE_JSON'
#!/bin/sh
printf '%s\n' 'stale destination merge-json must not run' >&2
exit 99
EOF_STALE_MERGE_JSON
chmod +x "$home/.local/bin/merge-json"
stale_merge_json_hash=$(shasum -a 256 "$home/.local/bin/merge-json")
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
		GOMODCACHE="$go_mod_cache" \
		PATH="$tmp/bin:$PATH" \
		CODEX_CONFIG_DIR="$home/.codex" \
		GLM_WORKER_BIN_DIR="$home/.local/bin" \
		GLM_WORKER_HOME="$home/.glm-worker" \
		CLAUDE_SETTINGS_FILE="$home/.claude/settings.json" \
		XDG_CONFIG_HOME="$home/.config" \
		"$repo/install.sh"
}

run_install
for binary in glm-worker glm-parent-action glm-codex-context commentlint harnesslint; do
	test -x "$home/.local/bin/$binary"
done
if [ "$(shasum -a 256 "$home/.local/bin/merge-json")" != "$stale_merge_json_hash" ]; then
	printf '%s\n' 'destination merge-json was overwritten by install' >&2
	exit 1
fi
test -f "$home/.codex/AGENTS.md"
test -f "$home/.codex/rules/glm-worker.rules"
cmp "$repo/codex/rules/glm-worker.rules" "$home/.codex/rules/glm-worker.rules"
grep -Fq '"glm-worker"' "$home/.codex/rules/glm-worker.rules"
grep -Fq '"glm-parent-action"' "$home/.codex/rules/glm-worker.rules"
grep -Fq '"glm-codex-context"' "$home/.codex/rules/glm-worker.rules"
if grep -Fq '"git"' "$home/.codex/rules/glm-worker.rules"; then
	printf '%s\n' 'managed rules allow a git subcommand' >&2
	exit 1
fi
if grep -Eq '(not_)?match = \[' "$home/.codex/rules/glm-worker.rules"; then
	printf '%s\n' 'managed rules still pin a per-subcommand allow-list' >&2
	exit 1
fi
test -f "$home/.codex/glm-worker/prompts/WORKER.md"
grep -q '^local_key = "keep"$' "$home/.codex/config.toml"
grep -q '^background_terminal_max_timeout = ' "$home/.codex/config.toml"
grep -q '"LOCAL": "keep"' "$home/.claude/settings.json"
grep -q '"REMOVE_ME": "local"' "$home/.claude/settings.json"
grep -q '"permissions"' "$home/.claude/settings.json"

mkdir -p "$home/.config/codex-config"
printf '%s\n' '{"env":{"LOCAL":"override","REMOVE_ME":null,"SMOKE_ADDED":"present"}}' >"$home/.config/codex-config/claude-settings.local.json"
run_install
grep -q '"LOCAL": "override"' "$home/.claude/settings.json"
grep -q '"SMOKE_ADDED": "present"' "$home/.claude/settings.json"
if grep -q '"REMOVE_ME"' "$home/.claude/settings.json"; then
	printf '%s\n' 'claude local override null deletion was not applied' >&2
	exit 1
fi
if [ "$(shasum -a 256 "$home/.local/bin/merge-json")" != "$stale_merge_json_hash" ]; then
	printf '%s\n' 'destination merge-json changed during reinstall' >&2
	exit 1
fi

cmp "$repo/codex/instructions/glm-repo-search.md" "$home/.codex/instructions/glm-repo-search.md"
grep -q 'GLM_WORKER_REPO_SEARCH' "$home/.codex/instructions/glm-repo-search.md"
HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --help >"$tmp/repo-search-help.json"
grep -q -- '--repo-search' "$tmp/repo-search-help.json"
HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" GLM_WORKER_REPO_SEARCH=0 "$home/.local/bin/glm-worker" --repo-search smoke >"$tmp/repo-search-disabled.json"
grep -q '"status":"disabled"' "$tmp/repo-search-disabled.json"
grep -q '"result":"disabled"' "$tmp/repo-search-disabled.json"

"$home/.local/bin/glm-codex-context" enable "$repo" >"$tmp/codex-context-enable.json"
grep -q '"status":"enabled"' "$tmp/codex-context-enable.json"
grep -q '"git_excluded":true' "$tmp/codex-context-enable.json"
grep -q '"requires_new_thread":true' "$tmp/codex-context-enable.json"
grep -Fxq 'include_apps_instructions = false' "$repo/.codex/config.toml"
grep -Fxq 'include_collaboration_mode_instructions = false' "$repo/.codex/config.toml"
grep -Fxq 'include_instructions = false' "$repo/.codex/config.toml"
grep -Fxq 'apps = false' "$repo/.codex/config.toml"
grep -Fxq 'plugins = false' "$repo/.codex/config.toml"
git -C "$repo" check-ignore -q -- .codex/config.toml
if git -C "$repo" status --porcelain --untracked-files=all | grep -Fq '.codex/config.toml'; then
	printf '%s\n' 'Codex context profile polluted target repository status' >&2
	exit 1
fi
"$home/.local/bin/glm-codex-context" enable "$repo" >"$tmp/codex-context-enable-again.json"
grep -q '"status":"enabled"' "$tmp/codex-context-enable-again.json"
"$home/.local/bin/glm-codex-context" disable "$repo" >"$tmp/codex-context-disable.json"
grep -q '"status":"disabled"' "$tmp/codex-context-disable.json"
test ! -e "$repo/.codex/config.toml"

(
	cd "$repo"
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" prepare decision
) >"$tmp/parent-action-prepare.json"
grep -q '"status":"prepared"' "$tmp/parent-action-prepare.json"
grep -q '"action":"decision"' "$tmp/parent-action-prepare.json"
repo_physical=$(CDPATH='' cd -- "$repo" && pwd -P)
grep -Fq "$repo_physical/.glm-worker-parent-actions/decision-" "$tmp/parent-action-prepare.json"
rm -rf "$repo/.glm-worker-parent-actions"
if (
	cd "$repo"
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-parent-action" made-up-action
) >"$tmp/parent-action-unknown.stdout" 2>"$tmp/parent-action-unknown.stderr"; then
	printf '%s\n' 'glm-parent-action accepted an unknown action' >&2
	exit 1
fi
test ! -s "$tmp/parent-action-unknown.stdout"
(
	cd "$repo"
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --reset >/dev/null
	HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --status
) >"$tmp/runtime-status.json"
grep -Fq "\"vcs_revision\":\"$repo_revision\"" "$tmp/runtime-status.json"
grep -Fq '"vcs_modified":false' "$tmp/runtime-status.json"
grep -Fq '"relationship":"same"' "$tmp/runtime-status.json"
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
