#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
helper=${HOOK_HELPER_OVERRIDE:-"$source_root/scripts/manage-pull-hook.sh"}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hook-linked-worktree-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

guard_bin="$tmp/glm-parent-action"
printf '#!/bin/sh\nexit 0\n' >"$guard_bin"
chmod 755 "$guard_bin"

new_repo() {
	target=$1
	mkdir -p "$target/.githooks"
	git -C "$target" init -q -b main
	git -C "$target" config user.email test@example.com
	git -C "$target" config user.name Test
	for hook in post-merge pre-commit reference-transaction pre-push; do
		cat >"$target/.githooks/$hook" <<'EOF_HOOK'
#!/bin/sh
exit 0
EOF_HOOK
		chmod 755 "$target/.githooks/$hook"
	done
	git -C "$target" add .githooks
	git -C "$target" commit -qm hooks
}

common_dir() {
	repo=$1
	path=$(git -C "$repo" rev-parse --git-common-dir)
	case "$path" in
	/*) printf '%s\n' "$path" ;;
	*) printf '%s/%s\n' "$repo" "$path" ;;
	esac
}

state_path() {
	printf '%s/codex-worker-orchestrator/hooks-path.state\n' "$(common_dir "$1")"
}

managed_hooks_path() {
	printf '%s/codex-worker-orchestrator/hooks\n' "$(common_dir "$1")"
}

repo="$tmp/repo"
linked="$tmp/linked"
new_repo "$repo"

sh "$helper" install "$repo" "$guard_bin"
managed=$(managed_hooks_path "$repo")
state=$(state_path "$repo")
test -f "$state"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = "$managed"

git -C "$repo" worktree add -q --detach "$linked" HEAD
main_git_dir=$(git -C "$repo" rev-parse --git-dir)
linked_git_dir=$(git -C "$linked" rev-parse --git-dir)
test "$main_git_dir" != "$linked_git_dir"
test "$(state_path "$linked")" = "$state"
test "$(managed_hooks_path "$linked")" = "$managed"
test "$(git -C "$linked" config --local --get-all core.hooksPath)" = "$managed"

sh "$helper" install "$linked" "$guard_bin" >"$tmp/linked-install.stdout" 2>"$tmp/linked-install.stderr"
grep -Fq 'refreshed installer-owned snapshot hooks' "$tmp/linked-install.stdout"
test ! -s "$tmp/linked-install.stderr"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = "$managed"
test "$(git -C "$linked" config --local --get-all core.hooksPath)" = "$managed"
for hook in post-merge pre-commit reference-transaction pre-push; do
	test -f "$managed/$hook"
	test ! -L "$managed/$hook"
	test -s "$managed/$hook"
	test -x "$managed/$hook"
	git -C "$linked" show "HEAD:.githooks/$hook" | cmp -s - "$managed/$hook"
done

sh "$helper" retire "$linked" "$guard_bin" >"$tmp/linked-retire.stdout" 2>"$tmp/linked-retire.stderr"
grep -Fq 'retired installer-owned core.hooksPath' "$tmp/linked-retire.stdout"
test ! -s "$tmp/linked-retire.stderr"
test ! -e "$state"
test ! -e "$managed"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'linked-worktree retire left shared core.hooksPath configured' >&2
	exit 1
fi
if git -C "$linked" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'linked-worktree retire left linked core.hooksPath configured' >&2
	exit 1
fi
