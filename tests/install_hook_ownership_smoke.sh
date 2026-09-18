#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
helper=${HOOK_HELPER_OVERRIDE:-"$source_root/scripts/manage-pull-hook.sh"}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hook-ownership-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

new_repo() {
	target=$1
	mkdir -p "$target/.githooks"
	git -C "$target" init -q
	git -C "$target" config user.email test@example.com
	git -C "$target" config user.name Test
	for hook in post-merge pre-commit reference-transaction pre-push; do
		cat >"$target/.githooks/$hook" <<'EOF_HOOK'
#!/bin/sh
exit 0
EOF_HOOK
	done
	git -C "$target" add .githooks
	git -C "$target" commit -qm hooks
}

state_path() {
	repo=$1
	path=$(git -C "$repo" rev-parse --git-path codex-worker-orchestrator/hooks-path.state)
	case "$path" in
	/*) printf '%s\n' "$path" ;;
	*) printf '%s/%s\n' "$repo" "$path" ;;
	esac
}

managed_hooks_path() {
	repo=$1
	path=$(git -C "$repo" rev-parse --git-common-dir)
	case "$path" in
	/*) ;;
	*) path="$repo/$path" ;;
	esac
	printf '%s/codex-worker-orchestrator/hooks\n' "$path"
}

assert_managed_hooks() {
	repo=$1
	managed=$(managed_hooks_path "$repo")
	test "$(git -C "$repo" config --local --get-all core.hooksPath)" = "$managed"
	for hook in post-merge pre-commit reference-transaction pre-push; do
		test -x "$managed/$hook"
	done
}

repo="$tmp/absent"
new_repo "$repo"
sh "$helper" install "$repo"
assert_managed_hooks "$repo"
managed=$(managed_hooks_path "$repo")
printf '#!/bin/sh\nexit 99\n' >"$repo/.githooks/pre-push"
sh "$managed/pre-push"
state_before=$(cat "$(state_path "$repo")")
sh "$helper" install "$repo"
assert_managed_hooks "$repo"
sh "$managed/pre-push"
test "$(cat "$(state_path "$repo")")" = "$state_before"
sh "$helper" retire "$repo"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'retire left installer-owned core.hooksPath configured' >&2
	exit 1
fi
test ! -e "$(state_path "$repo")"
test ! -e "$managed"

repo="$tmp/interrupted"
new_repo "$repo"
pending_state=$(state_path "$repo")
managed=$(managed_hooks_path "$repo")
mkdir -p "${pending_state%/*}"
printf 'version=2 baseline=absent pending=%s\n' "$managed" >"$pending_state"
sh "$helper" install "$repo" >"$tmp/interrupted.stdout" 2>"$tmp/interrupted.stderr"
assert_managed_hooks "$repo"
test "$(cat "$pending_state")" = "version=2 baseline=absent value=$managed"
grep -Fq 'recovered interrupted installer-owned snapshot hooks activation' "$tmp/interrupted.stdout"

repo="$tmp/legacy"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
legacy_state=$(state_path "$repo")
mkdir -p "${legacy_state%/*}"
printf '%s\n' 'version=1 baseline=absent value=.githooks' >"$legacy_state"
sh "$helper" install "$repo" >"$tmp/legacy.stdout" 2>"$tmp/legacy.stderr"
assert_managed_hooks "$repo"
grep -Fq 'migrated installer-owned .githooks to snapshot hooks' "$tmp/legacy.stdout"
case "$(cat "$legacy_state")" in
'version=2 baseline=absent value='*) ;;
*) printf '%s\n' 'legacy ownership state was not migrated' >&2; exit 1 ;;
esac

repo="$tmp/inherited-git-dir"
other_repo="$tmp/inherited-other"
new_repo "$repo"
new_repo "$other_repo"
other_git_dir=$(git -C "$other_repo" rev-parse --absolute-git-dir)
GIT_DIR="$other_git_dir" GIT_WORK_TREE="$other_repo" sh "$helper" install "$repo"
assert_managed_hooks "$repo"
if git -C "$other_repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'inherited Git repository variables redirected hook installation' >&2
	exit 1
fi

repo="$tmp/preexisting-other"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .external-hooks
sh "$helper" install "$repo" >"$tmp/preexisting-other.stdout" 2>"$tmp/preexisting-other.stderr"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.external-hooks'
grep -Fq 'preexisting core.hooksPath retained' "$tmp/preexisting-other.stderr"
test ! -e "$(state_path "$repo")"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.external-hooks'

repo="$tmp/preexisting-same"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
sh "$helper" install "$repo" >"$tmp/preexisting-same.stdout" 2>"$tmp/preexisting-same.stderr"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.githooks'
grep -Fq 'preexisting .githooks retained without claiming ownership' "$tmp/preexisting-same.stdout"
test ! -e "$(state_path "$repo")"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.githooks'

repo="$tmp/external-change"
new_repo "$repo"
sh "$helper" install "$repo"
changed_state=$(state_path "$repo")
managed=$(managed_hooks_path "$repo")
test -f "$changed_state"
git -C "$repo" config --local core.hooksPath .changed-externally
sh "$helper" install "$repo" >"$tmp/external-change.stdout" 2>"$tmp/external-change.stderr"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.changed-externally'
grep -Fq 'ownership state exists but core.hooksPath changed externally' "$tmp/external-change.stderr"
test -f "$changed_state"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.changed-externally'
test ! -e "$changed_state"
test ! -e "$managed"

repo="$tmp/corrupt-state"
new_repo "$repo"
corrupt_state=$(state_path "$repo")
mkdir -p "${corrupt_state%/*}"
printf '%s\n' 'not-owned-by-this-installer' >"$corrupt_state"
if sh "$helper" install "$repo" >"$tmp/corrupt-state.stdout" 2>"$tmp/corrupt-state.stderr"; then
	printf '%s\n' 'invalid hook ownership state was accepted' >&2
	exit 1
fi
grep -Fq 'git hook ownership state is invalid' "$tmp/corrupt-state.stderr"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'invalid ownership state changed core.hooksPath' >&2
	exit 1
fi
