#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
helper=${HOOK_HELPER_OVERRIDE:-"$source_root/scripts/manage-pull-hook.sh"}
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hook-ownership-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
real_git=$(command -v git)
real_mv=$(command -v mv)

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
		chmod 755 "$target/.githooks/$hook"
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
		test -f "$managed/$hook"
		test ! -L "$managed/$hook"
		test -s "$managed/$hook"
		test -x "$managed/$hook"
		git -C "$repo" show "HEAD:.githooks/$hook" | cmp -s - "$managed/$hook"
	done
}

assert_no_state() {
	repo=$1
	test ! -e "$(state_path "$repo")"
}

install_with_config_failure() {
	repo=$1
	fakebin=$2
	mkdir -p "$fakebin"
	cat >"$fakebin/git" <<'EOF_FAKE_GIT'
#!/bin/sh
if [ "${FAIL_HOOK_CONFIG:-0}" = 1 ] && [ "$#" -eq 6 ] && [ "$1" = -C ] && [ "$3" = config ] && [ "$4" = --local ] && [ "$5" = core.hooksPath ]; then
	exit 73
fi
exec "$REAL_GIT" "$@"
EOF_FAKE_GIT
	chmod 755 "$fakebin/git"
	if PATH="$fakebin:$PATH" REAL_GIT="$real_git" FAIL_HOOK_CONFIG=1 sh "$helper" install "$repo" >"$fakebin/install.stdout" 2>"$fakebin/install.stderr"; then
		printf '%s\n' 'injected git config failure unexpectedly succeeded' >&2
		exit 1
	fi
}

install_with_activation_failure() {
	repo=$1
	fakebin=$2
	managed=$(managed_hooks_path "$repo")
	mkdir -p "$fakebin"
	cat >"$fakebin/mv" <<'EOF_FAKE_MV'
#!/bin/sh
if [ "${FAIL_MANAGED_ACTIVATION:-0}" = 1 ] && [ "$#" -eq 2 ]; then
	case "$1:$2" in
	*.stage.*:"$MANAGED_HOOKS_PATH") exit 74 ;;
	esac
fi
exec "$REAL_MV" "$@"
EOF_FAKE_MV
	chmod 755 "$fakebin/mv"
	if PATH="$fakebin:$PATH" REAL_MV="$real_mv" MANAGED_HOOKS_PATH="$managed" FAIL_MANAGED_ACTIVATION=1 sh "$helper" install "$repo" >"$fakebin/install.stdout" 2>"$fakebin/install.stderr"; then
		printf '%s\n' 'injected managed snapshot activation failure unexpectedly succeeded' >&2
		exit 1
	fi
}

repo="$tmp/absent"
new_repo "$repo"
sh "$helper" install "$repo"
assert_managed_hooks "$repo"
managed=$(managed_hooks_path "$repo")
state=$(state_path "$repo")
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
printf '#!/bin/sh\nexit 99\n' >"$repo/.githooks/pre-push"
chmod 755 "$repo/.githooks/pre-push"
sh "$helper" install "$repo"
assert_managed_hooks "$repo"
sh "$helper" retire "$repo"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'retire left installer-owned core.hooksPath configured' >&2
	exit 1
fi
assert_no_state "$repo"
test ! -e "$managed"

repo="$tmp/atomic-refresh"
new_repo "$repo"
sh "$helper" install "$repo"
managed=$(managed_hooks_path "$repo")
before="$tmp/atomic-before"
cp -R "$managed" "$before"
git -C "$repo" rm -q .githooks/reference-transaction
git -C "$repo" commit -qm 'remove required hook'
if sh "$helper" install "$repo" >"$tmp/atomic-refresh.stdout" 2>"$tmp/atomic-refresh.stderr"; then
	printf '%s\n' 'refresh with missing committed hook unexpectedly succeeded' >&2
	exit 1
fi
grep -Fq 'committed source missing: .githooks/reference-transaction' "$tmp/atomic-refresh.stderr"
for hook in post-merge pre-commit reference-transaction pre-push; do
	cmp "$before/$hook" "$managed/$hook"
	test -x "$managed/$hook"
done

repo="$tmp/activation-rollback"
new_repo "$repo"
sh "$helper" install "$repo"
managed=$(managed_hooks_path "$repo")
before="$tmp/activation-before"
cp -R "$managed" "$before"
printf '#!/bin/sh\nexit 88\n' >"$repo/.githooks/pre-push"
chmod 755 "$repo/.githooks/pre-push"
git -C "$repo" add .githooks/pre-push
git -C "$repo" commit -qm 'change required hook'
install_with_activation_failure "$repo" "$tmp/fakemv-activation"
grep -Fq 'failed to activate validated managed snapshot' "$tmp/fakemv-activation/install.stderr"
for hook in post-merge pre-commit reference-transaction pre-push; do
	cmp "$before/$hook" "$managed/$hook"
	test -x "$managed/$hook"
done
sh "$helper" install "$repo"
assert_managed_hooks "$repo"

repo="$tmp/detached-first"
new_repo "$repo"
git -C "$repo" checkout -q --detach HEAD
sh "$helper" install "$repo" >"$tmp/detached-first.stdout" 2>"$tmp/detached-first.stderr"
assert_managed_hooks "$repo"
grep -Fq 'enabled installer-owned snapshot hooks' "$tmp/detached-first.stdout"

repo="$tmp/detached-refresh"
new_repo "$repo"
sh "$helper" install "$repo"
printf '#!/bin/sh\nexit 77\n' >"$repo/.githooks/pre-push"
chmod 755 "$repo/.githooks/pre-push"
git -C "$repo" add .githooks/pre-push
git -C "$repo" commit -qm 'candidate hook change'
git -C "$repo" checkout -q --detach HEAD
sh "$helper" install "$repo"
assert_managed_hooks "$repo"

repo="$tmp/interrupted"
new_repo "$repo"
managed=$(managed_hooks_path "$repo")
state=$(state_path "$repo")
install_with_config_failure "$repo" "$tmp/fakegit-absent"
test "$(cat "$state")" = "version=2 baseline=absent pending=$managed"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'failed fresh activation unexpectedly changed core.hooksPath' >&2
	exit 1
fi
sh "$helper" install "$repo" >"$tmp/interrupted.stdout" 2>"$tmp/interrupted.stderr"
assert_managed_hooks "$repo"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
grep -Fq 'recovered interrupted installer-owned snapshot hooks activation' "$tmp/interrupted.stdout"

repo="$tmp/adopted"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
managed=$(managed_hooks_path "$repo")
state=$(state_path "$repo")
sh "$helper" install "$repo" >"$tmp/adopted.stdout" 2>"$tmp/adopted.stderr"
assert_managed_hooks "$repo"
test "$(cat "$state")" = "version=2 baseline=.githooks value=$managed"
grep -Fq 'adopted preexisting tracked .githooks into installer-owned snapshot hooks' "$tmp/adopted.stdout"
sh "$helper" retire "$repo" >"$tmp/adopted-retire.stdout" 2>"$tmp/adopted-retire.stderr"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .githooks
assert_no_state "$repo"
test ! -e "$managed"
grep -Fq 'restored preexisting .githooks' "$tmp/adopted-retire.stdout"

repo="$tmp/adoption-interrupted"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
managed=$(managed_hooks_path "$repo")
state=$(state_path "$repo")
install_with_config_failure "$repo" "$tmp/fakegit-adoption"
test "$(cat "$state")" = "version=2 baseline=.githooks pending=$managed"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .githooks
sh "$helper" install "$repo" >"$tmp/adoption-interrupted.stdout" 2>"$tmp/adoption-interrupted.stderr"
assert_managed_hooks "$repo"
test "$(cat "$state")" = "version=2 baseline=.githooks value=$managed"
grep -Fq 'completed preexisting .githooks adoption' "$tmp/adoption-interrupted.stdout"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .githooks

repo="$tmp/legacy"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
state=$(state_path "$repo")
managed=$(managed_hooks_path "$repo")
mkdir -p "${state%/*}"
printf '%s\n' 'version=1 baseline=absent value=.githooks' >"$state"
sh "$helper" install "$repo" >"$tmp/legacy.stdout" 2>"$tmp/legacy.stderr"
assert_managed_hooks "$repo"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
grep -Fq 'migrated legacy installer-owned .githooks to snapshot hooks' "$tmp/legacy.stdout"
sh "$helper" retire "$repo"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'legacy installer baseline was not restored to absent' >&2
	exit 1
fi

repo="$tmp/legacy-migration-pending"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
state=$(state_path "$repo")
managed=$(managed_hooks_path "$repo")
mkdir -p "${state%/*}"
printf 'version=2 baseline=absent pending=%s source=.githooks\n' "$managed" >"$state"
sh "$helper" install "$repo" >"$tmp/legacy-migration-pending.stdout" 2>"$tmp/legacy-migration-pending.stderr"
assert_managed_hooks "$repo"
test "$(cat "$state")" = "version=2 baseline=absent value=$managed"
grep -Fq 'recovered legacy installer-owned hook migration' "$tmp/legacy-migration-pending.stdout"

repo="$tmp/preexisting-content-mismatch"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .githooks
printf '#!/bin/sh\nexit 42\n' >"$repo/.githooks/pre-push"
chmod 755 "$repo/.githooks/pre-push"
if sh "$helper" install "$repo" >"$tmp/content-mismatch.stdout" 2>"$tmp/content-mismatch.stderr"; then
	printf '%s\n' 'content-mismatched .githooks was claimed' >&2
	exit 1
fi
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .githooks
assert_no_state "$repo"
test ! -e "$(managed_hooks_path "$repo")"
grep -Fq 'cannot be safely adopted' "$tmp/content-mismatch.stderr"

repo="$tmp/preexisting-mode-mismatch"
new_repo "$repo"
chmod 644 "$repo/.githooks/pre-push"
git -C "$repo" add .githooks/pre-push
git -C "$repo" commit -qm 'make hook non-executable'
git -C "$repo" config --local core.hooksPath .githooks
if sh "$helper" install "$repo" >"$tmp/mode-mismatch.stdout" 2>"$tmp/mode-mismatch.stderr"; then
	printf '%s\n' 'non-executable tracked .githooks was claimed' >&2
	exit 1
fi
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .githooks
assert_no_state "$repo"

repo="$tmp/preexisting-other"
new_repo "$repo"
git -C "$repo" config --local core.hooksPath .external-hooks
if sh "$helper" install "$repo" >"$tmp/preexisting-other.stdout" 2>"$tmp/preexisting-other.stderr"; then
	printf '%s\n' 'external hook owner was accepted without managed publication guards' >&2
	exit 1
fi
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .external-hooks
assert_no_state "$repo"
grep -Fq 'preexisting core.hooksPath cannot be replaced safely' "$tmp/preexisting-other.stderr"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .external-hooks

repo="$tmp/external-change"
new_repo "$repo"
sh "$helper" install "$repo"
state=$(state_path "$repo")
managed=$(managed_hooks_path "$repo")
test -f "$state"
git -C "$repo" config --local core.hooksPath .changed-externally
if sh "$helper" install "$repo" >"$tmp/external-change.stdout" 2>"$tmp/external-change.stderr"; then
	printf '%s\n' 'externally changed core.hooksPath was accepted as managed' >&2
	exit 1
fi
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .changed-externally
grep -Fq 'ownership state conflicts with current core.hooksPath' "$tmp/external-change.stderr"
test -f "$state"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = .changed-externally
assert_no_state "$repo"
test ! -e "$managed"

repo="$tmp/unclaimed-managed-path"
new_repo "$repo"
managed=$(managed_hooks_path "$repo")
mkdir -p "$managed"
printf '%s\n' sentinel >"$managed/external"
if sh "$helper" install "$repo" >"$tmp/unclaimed.stdout" 2>"$tmp/unclaimed.stderr"; then
	printf '%s\n' 'unclaimed managed snapshot path was overwritten' >&2
	exit 1
fi
grep -Fq 'managed snapshot path exists without installer ownership state' "$tmp/unclaimed.stderr"
test "$(cat "$managed/external")" = sentinel
assert_no_state "$repo"

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

repo="$tmp/corrupt-state"
new_repo "$repo"
state=$(state_path "$repo")
mkdir -p "${state%/*}"
printf '%s\n' 'not-owned-by-this-installer' >"$state"
if sh "$helper" install "$repo" >"$tmp/corrupt-state.stdout" 2>"$tmp/corrupt-state.stderr"; then
	printf '%s\n' 'invalid hook ownership state was accepted' >&2
	exit 1
fi
grep -Fq 'git hook ownership state is invalid' "$tmp/corrupt-state.stderr"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'invalid ownership state changed core.hooksPath' >&2
	exit 1
fi
