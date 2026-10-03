#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
go_mod_cache=$(go env GOMODCACHE)
quality_tools_default=$(awk -F': ' '$1 == "default-bin-dir" { print $2; exit }' "$source_root/quality-tools.yml")
case "${QUALITY_TOOLS_BIN_DIR:-}" in
"") quality_tools_bin_dir="$HOME/$quality_tools_default" ;;
/*) quality_tools_bin_dir="$QUALITY_TOOLS_BIN_DIR" ;;
*) quality_tools_bin_dir="$source_root/$QUALITY_TOOLS_BIN_DIR" ;;
esac

tmp=$(mktemp -d "${TMPDIR:-/tmp}/codex-detached-runtime-identity.XXXXXX")
repo="$tmp/repo"
detached="$tmp/detached"
home="$tmp/home"
trap 'git -C "$repo" worktree remove --force "$detached" >/dev/null 2>&1 || true; rm -rf "$tmp"' EXIT HUP INT TERM
mkdir -p "$repo" "$home/.codex" "$home/.claude" "$home/.local/bin" "$tmp/bin"
rsync -a --exclude .git --exclude .codex "$source_root/" "$repo/"

git -C "$repo" init -q -b main
git -C "$repo" add -A
git -C "$repo" -c user.name=detached-runtime-identity -c user.email=detached-runtime-identity@example.invalid commit -qm candidate
candidate_oid=$(git -C "$repo" rev-parse HEAD)
printf '\nouter head marker\n' >>"$repo/README.md"
git -C "$repo" add README.md
git -C "$repo" -c user.name=detached-runtime-identity -c user.email=detached-runtime-identity@example.invalid commit -qm 'outer head'
outer_oid=$(git -C "$repo" rev-parse HEAD)
if [ "$candidate_oid" = "$outer_oid" ]; then
	printf '%s\n' 'fixture failed to separate candidate and outer HEAD' >&2
	exit 1
fi

git -C "$repo" worktree add -q --detach "$detached" "$candidate_oid"

cat >"$tmp/bin/claude" <<'EOF_CLAUDE'
#!/bin/sh
if [ "${1:-}" = "--help" ]; then
	printf '%s\n' '--json-schema'
	exit 0
fi
exit 1
EOF_CLAUDE
chmod +x "$tmp/bin/claude"

run_detached_install() {
	HOME="$home" \
		GOMODCACHE="$go_mod_cache" \
		QUALITY_TOOLS_BIN_DIR="$quality_tools_bin_dir" \
		CODEX_HOME="$home/.codex" \
		GLM_WORKER_BIN_DIR="$home/.local/bin" \
		GLM_WORKER_HOME="$home/.glm-worker" \
		GLM_WORKER_CLAUDE_BIN="$tmp/bin/claude" \
		CLAUDE_SETTINGS_FILE="$home/.claude/settings.json" \
		XDG_CONFIG_HOME="$home/.config" \
		"$detached/install.sh" >/dev/null
}

runtime_status() {
	(
		cd "$detached"
		HOME="$home" GLM_WORKER_HOME="$home/.glm-worker" "$home/.local/bin/glm-worker" --status
	)
}

run_detached_install
runtime_status >"$tmp/clean-status.json"
grep -Fq "\"vcs_revision\":\"$candidate_oid\"" "$tmp/clean-status.json"
grep -Fq '"vcs_modified":false' "$tmp/clean-status.json"
grep -Fq "\"repository_head\":\"$candidate_oid\"" "$tmp/clean-status.json"
grep -Fq '"relationship":"same"' "$tmp/clean-status.json"
if grep -Fq "\"vcs_revision\":\"$outer_oid\"" "$tmp/clean-status.json"; then
	printf '%s\n' 'detached install inherited outer worktree runtime identity' >&2
	exit 1
fi
if [ "$(git -C "$repo" rev-parse HEAD)" != "$outer_oid" ]; then
	printf '%s\n' 'detached install changed outer worktree HEAD' >&2
	exit 1
fi

printf '\ndirty candidate source\n' >>"$detached/README.md"
printf '%s\n' 'untracked candidate source' >"$detached/.runtime-identity-untracked"
run_detached_install
runtime_status >"$tmp/dirty-status.json"
grep -Fq "\"vcs_revision\":\"$candidate_oid\"" "$tmp/dirty-status.json"
grep -Fq '"vcs_modified":true' "$tmp/dirty-status.json"
grep -Fq "\"repository_head\":\"$candidate_oid\"" "$tmp/dirty-status.json"
grep -Fq '"relationship":"unknown"' "$tmp/dirty-status.json"
if grep -Fq '"relationship":"same"' "$tmp/dirty-status.json"; then
	printf '%s\n' 'dirty detached source was accepted as exact candidate identity' >&2
	exit 1
fi
