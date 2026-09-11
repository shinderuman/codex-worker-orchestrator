#!/bin/sh
set -eu

source_root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
helper="$source_root/scripts/manage-pull-hook.sh"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/hook-ownership-smoke.XXXXXX")
trap 'rm -rf "$tmp"' EXIT HUP INT TERM

new_repo() {
	repo=$1
	mkdir -p "$repo/.githooks"
	git -C "$repo" init -q
	cat >"$repo/.githooks/post-merge" <<'EOF_HOOK'
#!/bin/sh
exit 0
EOF_HOOK
}

state_path() {
	repo=$1
	path=$(git -C "$repo" rev-parse --git-path codex-worker-orchestrator/hooks-path.state)
	case "$path" in
	/*) printf '%s\n' "$path" ;;
	*) printf '%s/%s\n' "$repo" "$path" ;;
	esac
}

repo="$tmp/absent"
new_repo "$repo"
sh "$helper" install "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.githooks'
owned_state=$(state_path "$repo")
test -f "$owned_state"
state_before=$(cat "$owned_state")
sh "$helper" install "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.githooks'
test "$(cat "$owned_state")" = "$state_before"
sh "$helper" retire "$repo"
if git -C "$repo" config --local --get-all core.hooksPath >/dev/null 2>&1; then
	printf '%s\n' 'retire left installer-owned core.hooksPath configured' >&2
	exit 1
fi
test ! -e "$owned_state"

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
test -f "$changed_state"
git -C "$repo" config --local core.hooksPath .changed-externally
sh "$helper" install "$repo" >"$tmp/external-change.stdout" 2>"$tmp/external-change.stderr"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.changed-externally'
grep -Fq 'ownership state exists but core.hooksPath changed externally' "$tmp/external-change.stderr"
test -f "$changed_state"
sh "$helper" retire "$repo"
test "$(git -C "$repo" config --local --get-all core.hooksPath)" = '.changed-externally'
test ! -e "$changed_state"

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
